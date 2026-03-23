package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/logutil"
	"github.com/nevinsm/sol/internal/processutil"
	"github.com/nevinsm/sol/internal/store"
)

// DefaultPort is the standard OTLP HTTP port.
const DefaultPort = 4318

// Config holds ledger configuration.
type Config struct {
	Port    int
	SOLHome string
}

// DefaultConfig returns defaults for the given SOL_HOME.
func DefaultConfig(solHome string) Config {
	return Config{
		Port:    DefaultPort,
		SOLHome: solHome,
	}
}

// sessionKey identifies an active agent session.
type sessionKey struct {
	World      string
	AgentName  string
	WritID string
}

// Ledger receives OTLP HTTP log events and writes token usage to world databases.
type Ledger struct {
	config   Config
	logger   *log.Logger
	eventLog *events.Logger // optional event logger

	mu       sync.Mutex
	sessions map[sessionKey]string    // sessionKey -> agent_history ID
	stores   map[string]*store.WorldStore  // world name -> store (cached)
	worlds   map[string]bool          // worlds written to (for heartbeat)

	// Atomic counters for heartbeat/ingest events.
	requestCount   atomic.Int64
	tokensIngested atomic.Int64
}

// New creates a new Ledger instance.
// The eventLog parameter is optional — if nil, no events are emitted.
func New(cfg Config, eventLog ...*events.Logger) *Ledger {
	var el *events.Logger
	if len(eventLog) > 0 {
		el = eventLog[0]
	}
	return &Ledger{
		config:   cfg,
		logger:   log.New(os.Stderr, "[ledger] ", log.LstdFlags),
		eventLog: el,
		sessions: make(map[sessionKey]string),
		stores:   make(map[string]*store.WorldStore),
		worlds:   make(map[string]bool),
	}
}

// PIDPath returns the path to the ledger PID file.
func PIDPath() string {
	return filepath.Join(config.RuntimeDir(), "ledger.pid")
}

// WritePID writes the current process PID to the ledger PID file.
func WritePID() error {
	return processutil.WritePID(PIDPath(), os.Getpid())
}

// RemovePID removes the ledger PID file on clean shutdown.
func RemovePID() {
	_ = processutil.ClearPID(PIDPath())
}

// ReadPID reads the ledger PID from its PID file. Returns 0 if not found.
func ReadPID() int {
	pid, _ := processutil.ReadPID(PIDPath())
	return pid
}

// Run starts the OTLP HTTP server and blocks until the context is cancelled.
func (l *Ledger) Run(ctx context.Context) error {
	// Write PID file.
	if err := WritePID(); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}
	defer RemovePID()
	defer RemoveHeartbeat()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/logs", l.handleLogs)

	addr := fmt.Sprintf("127.0.0.1:%d", l.config.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Start listener.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		l.emitError("listen_failed", err)
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	// Emit start event.
	l.emitEvent(events.EventLedgerStart, map[string]any{
		"port": l.config.Port,
		"addr": addr,
	})

	// Write initial heartbeat.
	l.writeHeartbeat("running")

	// Start heartbeat goroutine (every 30s).
	go l.heartbeatLoop(ctx)

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	l.logger.Printf("listening on %s", addr)
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		l.emitError("server_error", err)
		return fmt.Errorf("server error: %w", err)
	}

	// Emit stop event.
	l.emitEvent(events.EventLedgerStop, map[string]any{
		"requests_total":   l.requestCount.Load(),
		"tokens_processed": l.tokensIngested.Load(),
	})

	// Clean up heartbeat.
	RemoveHeartbeat()

	// Close cached stores.
	l.mu.Lock()
	for _, s := range l.stores {
		s.Close()
	}
	l.mu.Unlock()

	return nil
}

// heartbeatLoop writes heartbeat and emits periodic ingest summary every 30 seconds.
func (l *Ledger) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.writeHeartbeat("running")
			// Emit periodic ingest summary so the ledger appears in the feed.
			if l.eventLog != nil {
				l.mu.Lock()
				worldCount := len(l.worlds)
				l.mu.Unlock()
				l.eventLog.Emit(events.EventLedgerIngest, "ledger", "ledger", "feed",
					map[string]any{
						"requests_total":   l.requestCount.Load(),
						"tokens_processed": l.tokensIngested.Load(),
						"worlds_written":   worldCount,
					})
			}
			// Best-effort log rotation.
			logutil.TruncateIfNeeded(filepath.Join(config.RuntimeDir(), "ledger.log"), logutil.DefaultMaxLogSize)
		}
	}
}

// writeHeartbeat writes a heartbeat file with current counters.
func (l *Ledger) writeHeartbeat(status string) {
	l.mu.Lock()
	worldCount := len(l.worlds)
	l.mu.Unlock()

	hb := Heartbeat{
		Timestamp:       time.Now().UTC(),
		Status:          status,
		RequestsTotal:   l.requestCount.Load(),
		TokensProcessed: l.tokensIngested.Load(),
		WorldsWritten:   worldCount,
	}
	if err := WriteHeartbeat(hb); err != nil {
		l.logger.Printf("failed to write heartbeat: %v", err)
	}
}

// emitEvent emits an event if an event logger is configured.
func (l *Ledger) emitEvent(eventType string, payload any) {
	if l.eventLog != nil {
		l.eventLog.Emit(eventType, "ledger", "ledger", "both", payload)
	}
}

// emitError emits a ledger_error event.
func (l *Ledger) emitError(reason string, err error) {
	l.emitEvent(events.EventLedgerError, map[string]any{
		"reason": reason,
		"error":  err.Error(),
	})
}

// handleLogs processes OTLP HTTP log export requests.
func (l *Ledger) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024)) // 10MB limit
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	var req ExportLogsServiceRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid OTLP JSON", http.StatusBadRequest)
		return
	}

	var totalRecords, failedRecords int
	for _, rl := range req.ResourceLogs {
		total, failed := l.processResourceLogs(rl)
		totalRecords += total
		failedRecords += failed
	}

	w.Header().Set("Content-Type", "application/json")
	if failedRecords > 0 && failedRecords == totalRecords {
		// All records failed — signal to OTLP client to retry.
		w.WriteHeader(http.StatusInternalServerError)
		resp, _ := json.Marshal(map[string]any{
			"error":          "all records failed",
			"total_records":  totalRecords,
			"failed_records": failedRecords,
		})
		_, _ = w.Write(resp)
	} else if failedRecords > 0 {
		// Partial failure — HTTP 200 with partialSuccess body per OTLP spec.
		w.WriteHeader(http.StatusOK)
		resp, _ := json.Marshal(map[string]any{
			"partialSuccess": map[string]any{
				"rejectedLogRecords": failedRecords,
				"errorMessage":       fmt.Sprintf("%d of %d log records rejected", failedRecords, totalRecords),
			},
		})
		_, _ = w.Write(resp)
	} else {
		// All records succeeded (or no processable records).
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}
}

// processResourceLogs handles a single ResourceLogs entry.
// Returns total processable records and the number that failed.
func (l *Ledger) processResourceLogs(rl ResourceLogs) (total, failed int) {
	resAttrs := attributeMap(rl.Resource.Attributes)

	agentName := resAttrs["agent.name"]
	world := resAttrs["world"]
	writID := resAttrs["writ_id"]

	if agentName == "" || world == "" {
		return 0, 0 // skip events without required resource attributes
	}

	for _, sl := range rl.ScopeLogs {
		for _, rec := range sl.LogRecords {
			total++
			if err := l.processLogRecord(world, agentName, writID, rec); err != nil {
				failed++
			}
		}
	}

	return total, failed
}

// processLogRecord processes a single log record, extracting token usage.
// Returns nil on success (including filtered-out records), or an error if
// the record could not be persisted.
func (l *Ledger) processLogRecord(world, agentName, writID string, rec LogRecord) error {
	attrs := attributeMap(rec.Attributes)

	// Filter for claude_code.api_request events.
	// The event name may be in the body or in an attribute.
	// Claude Code sets the body to "claude_code.api_request" and the
	// event.name attribute to "api_request" (without the prefix).
	// Accept both forms so we handle either path.
	eventName := rec.Body.StringValue
	if eventName == "" {
		eventName = attrs["event.name"]
	}
	if eventName != "claude_code.api_request" && eventName != "api_request" {
		return nil // not a relevant event, skip without error
	}

	// Claude Code emits short attribute names ("model", "input_tokens", …).
	// Fall back to OTel gen_ai.* semantic convention names for forward compatibility.
	model := attrs["model"]
	if model == "" {
		model = attrs["gen_ai.response.model"]
	}
	if model == "" {
		return nil // no model, skip without error
	}

	input := parseIntAttr(attrs, "input_tokens")
	if input == 0 {
		input = parseIntAttr(attrs, "gen_ai.usage.input_tokens")
	}
	output := parseIntAttr(attrs, "output_tokens")
	if output == 0 {
		output = parseIntAttr(attrs, "gen_ai.usage.output_tokens")
	}
	cacheRead := parseIntAttr(attrs, "cache_read_tokens")
	if cacheRead == 0 {
		cacheRead = parseIntAttr(attrs, "gen_ai.usage.cache_read_input_tokens")
	}
	cacheCreation := parseIntAttr(attrs, "cache_creation_tokens")
	if cacheCreation == 0 {
		cacheCreation = parseIntAttr(attrs, "gen_ai.usage.cache_creation_input_tokens")
	}

	historyID, err := l.ensureHistory(world, agentName, writID)
	if err != nil {
		l.logger.Printf("failed to ensure history for %s/%s: %v", world, agentName, err)
		l.emitError("ensure_history", err)
		return fmt.Errorf("ensure history: %w", err)
	}

	ws, err := l.worldStore(world)
	if err != nil {
		l.logger.Printf("failed to open world store %q: %v", world, err)
		l.emitError("open_world_store", err)
		return fmt.Errorf("open world store: %w", err)
	}

	if _, err := ws.WriteTokenUsage(historyID, model, input, output, cacheRead, cacheCreation); err != nil {
		l.logger.Printf("failed to write token usage: %v", err)
		l.emitError("write_token_usage", err)
		return fmt.Errorf("write token usage: %w", err)
	}

	// Track counters for heartbeat.
	l.requestCount.Add(1)
	l.tokensIngested.Add(input + output + cacheRead + cacheCreation)

	// Track worlds written to.
	l.mu.Lock()
	l.worlds[world] = true
	l.mu.Unlock()

	return nil
}

// ensureHistory returns the agent_history ID for the session, creating one if needed.
func (l *Ledger) ensureHistory(world, agentName, writID string) (string, error) {
	key := sessionKey{World: world, AgentName: agentName, WritID: writID}

	l.mu.Lock()
	if id, ok := l.sessions[key]; ok {
		l.mu.Unlock()
		return id, nil
	}
	l.mu.Unlock()

	ws, err := l.worldStore(world)
	if err != nil {
		return "", err
	}

	// Re-acquire lock and re-check before writing: another goroutine may have
	// raced past the first cache miss and already created the record. By holding
	// the lock across WriteHistory we guarantee at most one DB write per key.
	l.mu.Lock()
	defer l.mu.Unlock()

	if existing, ok := l.sessions[key]; ok {
		return existing, nil
	}

	id, err := ws.WriteHistory(agentName, writID, "session", "", time.Now(), nil)
	if err != nil {
		return "", fmt.Errorf("failed to create agent history: %w", err)
	}
	l.sessions[key] = id

	l.logger.Printf("created history %s for %s/%s (writ: %s)", id, world, agentName, writID)
	return id, nil
}

// worldStore returns a cached store for the given world.
func (l *Ledger) worldStore(world string) (*store.WorldStore, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if s, ok := l.stores[world]; ok {
		return s, nil
	}

	s, err := store.OpenWorld(world)
	if err != nil {
		return nil, fmt.Errorf("failed to open world database %q: %w", world, err)
	}

	l.stores[world] = s
	return s, nil
}

// parseIntAttr parses an integer attribute value, returning 0 on failure.
func parseIntAttr(attrs map[string]string, key string) int64 {
	v, ok := attrs[key]
	if !ok {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0
	}
	return n
}
