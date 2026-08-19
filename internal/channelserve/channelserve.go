// Package channelserve implements the bridge logic behind `sol channel
// serve`: a stdio JSON-RPC MCP server that declares Claude Code's
// experimental "claude/channel" capability and pushes pending
// internal/nudge queue messages into the live session as
// notifications/claude/channel events.
//
// Design, per the three-spike investigation (writs sol-159ee545a38d78ac,
// sol-d792deb1a2e99eec, sol-0e4943e8366c7b1c):
//
//   - Thin and stateless: [Serve] keeps no cursor or durability of its own.
//     internal/nudge's queue (Enqueue/Drain, atomic write + hardlink) is the
//     single source of truth. On every restart, whatever is still pending in
//     the queue gets redelivered — at-least-once, matching the queue's own
//     durability contract.
//   - Consume-then-push: each poll tick calls nudge.Drain, which atomically
//     claims and removes messages from the durable queue, then pushes each
//     one as a channel notification. If the push fails (client disconnected,
//     stdout closed), the message is re-enqueued rather than lost — the
//     window between Drain's claim and a successful push is the only place
//     a crash could still lose a message, an accepted trade-off already
//     documented by the spikes ("be fine with occasional duplicate
//     informational nudges rather than exactly-once semantics").
//   - Liveness signal, not delivery guarantee: every tick also calls
//     nudge.MarkChannelAlive, which is what lets nudge.Deliver's routing
//     logic (internal/nudge.Deliver) skip the pane doorbell while a bridge
//     is actively watching the queue.
//
// Testable without a live claude/homeserver process: In/Out are plain
// io.Reader/io.Writer, so tests drive Serve with piped JSON-RPC transcripts
// exactly as a real claude process would produce/consume them.
package channelserve

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/nevinsm/sol/internal/nudge"
)

// DefaultPoll is the queue poll / heartbeat interval used when Options.Poll
// is zero. Short enough that nudge.Deliver's channelHeartbeatFreshness
// window (10s) comfortably tolerates a couple of missed ticks.
const DefaultPoll = 2 * time.Second

// Options configures a Serve run.
type Options struct {
	// Session is the sol session name (config.SessionName(world, agent))
	// whose nudge queue this bridge watches.
	Session string

	// In/Out are the stdio JSON-RPC transport. In production these are
	// os.Stdin/os.Stdout (Claude Code speaks MCP over stdio to a plugin's
	// server); tests substitute piped transcripts.
	In  io.Reader
	Out io.Writer

	// Log receives human-readable diagnostic lines (connection lifecycle,
	// push failures). Defaults to io.Discard. Never written to Out — mixing
	// log lines into the JSON-RPC stream would corrupt the protocol.
	Log io.Writer

	// Poll is the queue-poll and heartbeat interval. Defaults to DefaultPoll.
	Poll time.Duration

	// Reply is called for every "reply" tool invocation from the model,
	// receiving the reply text. Defaults to replyViaMail, which shells out
	// to `sol mail send` addressed to the autarch. Overridable for tests.
	Reply func(text string) error
}

func (o *Options) setDefaults() {
	if o.Log == nil {
		o.Log = io.Discard
	}
	if o.Poll <= 0 {
		o.Poll = DefaultPoll
	}
	if o.Reply == nil {
		o.Reply = replyViaMail
	}
}

// replyViaMail is the default Options.Reply implementation: it forwards the
// model's reply text to the autarch via `sol mail send`, reusing the
// existing CLI rather than importing store internals directly — the same
// "shell out to sol's own CLI" pattern hook commands already use (e.g.
// `sol nudge drain` as a UserPromptSubmit hook). Resolves the sol binary via
// os.Executable() rather than a bare "sol" on PATH, matching the same
// resolution pattern used elsewhere sol re-invokes itself (e.g.
// internal/daemon's lifecycle helpers) — this process was itself launched at
// an absolute path by Claude Code's plugin loader, so PATH may not include
// sol's directory.
func replyViaMail(text string) error {
	solBin, err := os.Executable()
	if err != nil {
		solBin = "sol" // fall back to PATH lookup if self-resolution fails
	}
	cmd := exec.Command(solBin, "mail", "send", "--to", "autarch", "--subject", "channel reply", "--body", text) //nolint:gosec
	return cmd.Run()
}

// rpcRequest is an inbound JSON-RPC request or notification from Claude Code.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func (r rpcRequest) isNotification() bool {
	return len(r.ID) == 0 || string(r.ID) == "null"
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

// writer is a mutex-guarded line writer shared by the stdin-reader goroutine
// (which sends JSON-RPC responses) and the poll loop (which sends channel
// notifications) — both write to the same Out stream.
type writer struct {
	mu  sync.Mutex
	out io.Writer
}

func (w *writer) write(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("channelserve: failed to marshal message: %w", err)
	}
	data = append(data, '\n')

	w.mu.Lock()
	defer w.mu.Unlock()
	_, err = w.out.Write(data)
	return err
}

// Serve runs the channel bridge until ctx is cancelled or opts.In reaches
// EOF (the parent claude process exited, closing the pipe — the ordinary
// shutdown path in production). Blocking; callers run it for the lifetime
// of the MCP child process.
func Serve(ctx context.Context, opts Options) error {
	opts.setDefaults()
	w := &writer{out: opts.Out}

	// Mark alive immediately so nudge.Deliver can trust this bridge before
	// the first poll tick elapses, then again on every tick.
	if err := nudge.MarkChannelAlive(opts.Session); err != nil {
		fmt.Fprintf(opts.Log, "channelserve: failed to record initial heartbeat: %v\n", err)
	}

	readDone := make(chan error, 1)
	go func() {
		readDone <- readLoop(opts, w)
	}()

	ticker := time.NewTicker(opts.Poll)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-readDone:
			// stdin closed: the parent claude process exited. Ordinary
			// shutdown, not an error condition — mirrors the prototype's
			// "stdin closed (EOF) - claude process likely exited" log line.
			return err
		case <-ticker.C:
			if err := nudge.MarkChannelAlive(opts.Session); err != nil {
				fmt.Fprintf(opts.Log, "channelserve: failed to record heartbeat: %v\n", err)
			}
			pollAndDeliver(opts, w)
		}
	}
}

// pollAndDeliver drains any pending nudge messages for opts.Session and
// pushes each as a channel notification. Drain atomically claims and
// removes messages from the durable queue before this function ever sees
// them; a message whose push fails is re-enqueued so it is not lost — see
// the package doc's "consume-then-push" note.
func pollAndDeliver(opts Options, w *writer) {
	messages, err := nudge.Drain(opts.Session)
	if err != nil {
		fmt.Fprintf(opts.Log, "channelserve: drain failed: %v\n", err)
		return
	}
	for _, msg := range messages {
		notif := rpcNotification{
			JSONRPC: "2.0",
			Method:  "notifications/claude/channel",
			Params: map[string]any{
				"content": nudge.FormatNotification(msg),
				"meta": map[string]string{
					"sender": msg.Sender,
					"type":   msg.Type,
				},
			},
		}
		if err := w.write(notif); err != nil {
			fmt.Fprintf(opts.Log, "channelserve: push failed for message from %s, re-enqueuing: %v\n", msg.Sender, err)
			if reErr := nudge.Enqueue(opts.Session, msg); reErr != nil {
				fmt.Fprintf(opts.Log, "channelserve: re-enqueue after push failure also failed (message may be lost): %v\n", reErr)
			}
			continue
		}
	}
}

// readLoop reads newline-delimited JSON-RPC requests from opts.In and
// responds via w. Returns nil on ordinary EOF (parent exited).
func readLoop(opts Options, w *writer) error {
	scanner := bufio.NewScanner(opts.In)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			fmt.Fprintf(opts.Log, "channelserve: unparseable line (%d bytes): %v\n", len(line), err)
			continue
		}
		handleRequest(opts, w, req)
	}
	return scanner.Err()
}

func handleRequest(opts Options, w *writer, req rpcRequest) {
	switch req.Method {
	case "initialize":
		result := map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities": map[string]any{
				"tools": map[string]any{},
				// Presence of this key registers the channel notification
				// listener on Claude Code's side.
				"experimental": map[string]any{"claude/channel": map[string]any{}},
			},
			"serverInfo": map[string]any{
				"name":    "sol-channel",
				"version": "0.1.0",
			},
			"instructions": "Events from sol arrive as <channel source=\"sol-channel\" ...>. " +
				"They are informational nudges from sol's own nudge/mail queue (writ dependencies, " +
				"escalation replies, autarch messages). Use the reply tool if you want to send a " +
				"message back to the autarch — your transcript output never reaches sol on its own.",
		}
		if !req.isNotification() {
			_ = w.write(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result})
		}

	case "notifications/initialized":
		// No-op: handshake complete, nothing to do.

	case "tools/list":
		result := map[string]any{
			"tools": []map[string]any{
				{
					"name":        "reply",
					"description": "Send a message back to sol (routed to the autarch's mail inbox).",
					"inputSchema": map[string]any{
						"type":       "object",
						"properties": map[string]any{"text": map[string]any{"type": "string"}},
						"required":   []string{"text"},
					},
				},
			},
		}
		if !req.isNotification() {
			_ = w.write(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result})
		}

	case "tools/call":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		_ = json.Unmarshal(req.Params, &params)
		if params.Name != "reply" {
			if !req.isNotification() {
				_ = w.write(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
					"content": []map[string]any{{"type": "text", "text": "unknown tool"}},
					"isError": true,
				}})
			}
			return
		}
		text, _ := params.Arguments["text"].(string)
		replyErr := opts.Reply(text)
		result := map[string]any{"content": []map[string]any{{"type": "text", "text": "sent"}}}
		if replyErr != nil {
			fmt.Fprintf(opts.Log, "channelserve: reply failed: %v\n", replyErr)
			result = map[string]any{
				"content": []map[string]any{{"type": "text", "text": "failed to send reply"}},
				"isError": true,
			}
		}
		if !req.isNotification() {
			_ = w.write(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result})
		}

	case "ping":
		if !req.isNotification() {
			_ = w.write(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}})
		}

	case "notifications/claude/channel/permission":
		// Unused by sol's bridge (no permission-gated actions), acknowledged silently.

	default:
		if !req.isNotification() {
			_ = w.write(rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: "method not found"}})
		}
	}
}
