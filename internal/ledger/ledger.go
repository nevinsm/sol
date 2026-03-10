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
	"time"

	"github.com/nevinsm/sol/internal/config"
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
	config Config
	logger *log.Logger

	mu       sync.Mutex
	sessions map[sessionKey]string // sessionKey -> agent_history ID
	stores   map[string]*store.Store // world name -> store (cached)
}

// New creates a new Ledger instance.
func New(cfg Config) *Ledger {
	return &Ledger{
		config:   cfg,
		logger:   log.New(os.Stderr, "[ledger] ", log.LstdFlags),
		sessions: make(map[sessionKey]string),
		stores:   make(map[string]*store.Store),
	}
}

// PIDPath returns the path to the ledger PID file.
func PIDPath() string {
	return filepath.Join(config.RuntimeDir(), "ledger.pid")
}

// WritePID writes the current process PID to the ledger PID file.
func WritePID() error {
	dir := config.RuntimeDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create runtime directory: %w", err)
	}
	return os.WriteFile(PIDPath(), []byte(strconv.Itoa(os.Getpid())), 0o644)
}

// RemovePID removes the ledger PID file on clean shutdown.
func RemovePID() {
	_ = os.Remove(PIDPath())
}

// ReadPID reads the ledger PID from its PID file. Returns 0 if not found.
func ReadPID() int {
	data, err := os.ReadFile(PIDPath())
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return 0
	}
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
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	// Start heartbeat goroutine (writes every 30 seconds).
	go l.heartbeatLoop(ctx)

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	l.logger.Printf("listening on %s", addr)
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}

	// Close cached stores.
	l.mu.Lock()
	for _, s := range l.stores {
		s.Close()
	}
	l.mu.Unlock()

	return nil
}

// heartbeatLoop writes a heartbeat file every 30 seconds until ctx is cancelled.
func (l *Ledger) heartbeatLoop(ctx context.Context) {
	// Write an initial heartbeat immediately.
	if err := WriteHeartbeat(&Heartbeat{
		Timestamp: time.Now().UTC(),
		Status:    "running",
	}); err != nil {
		l.logger.Printf("failed to write heartbeat: %v", err)
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := WriteHeartbeat(&Heartbeat{
				Timestamp: time.Now().UTC(),
				Status:    "running",
			}); err != nil {
				l.logger.Printf("failed to write heartbeat: %v", err)
			}
		}
	}
}

// handleLogs processes OTLP HTTP log export requests.
func (l *Ledger) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024)) // 10MB limit
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req ExportLogsServiceRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid OTLP JSON", http.StatusBadRequest)
		return
	}

	for _, rl := range req.ResourceLogs {
		l.processResourceLogs(rl)
	}

	// OTLP expects an empty JSON response on success.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("{}"))
}

// processResourceLogs handles a single ResourceLogs entry.
func (l *Ledger) processResourceLogs(rl ResourceLogs) {
	resAttrs := attributeMap(rl.Resource.Attributes)

	agentName := resAttrs["agent.name"]
	world := resAttrs["world"]
	writID := resAttrs["writ_id"]

	if agentName == "" || world == "" {
		return // skip events without required resource attributes
	}

	for _, sl := range rl.ScopeLogs {
		for _, rec := range sl.LogRecords {
			l.processLogRecord(world, agentName, writID, rec)
		}
	}
}

// processLogRecord processes a single log record, extracting token usage.
func (l *Ledger) processLogRecord(world, agentName, writID string, rec LogRecord) {
	attrs := attributeMap(rec.Attributes)

	// Filter for claude_code.api_request events.
	// The event name may be in the body or in an attribute.
	eventName := rec.Body.StringValue
	if eventName == "" {
		eventName = attrs["event.name"]
	}
	if eventName != "claude_code.api_request" {
		return
	}

	model := attrs["gen_ai.response.model"]
	if model == "" {
		model = attrs["model"]
	}
	if model == "" {
		return // no model, skip
	}

	input := parseIntAttr(attrs, "gen_ai.usage.input_tokens")
	output := parseIntAttr(attrs, "gen_ai.usage.output_tokens")
	cacheRead := parseIntAttr(attrs, "gen_ai.usage.cache_read_input_tokens")
	cacheCreation := parseIntAttr(attrs, "gen_ai.usage.cache_creation_input_tokens")

	historyID, err := l.ensureHistory(world, agentName, writID)
	if err != nil {
		l.logger.Printf("failed to ensure history for %s/%s: %v", world, agentName, err)
		return
	}

	ws, err := l.worldStore(world)
	if err != nil {
		l.logger.Printf("failed to open world store %q: %v", world, err)
		return
	}

	if _, err := ws.WriteTokenUsage(historyID, model, input, output, cacheRead, cacheCreation); err != nil {
		l.logger.Printf("failed to write token usage: %v", err)
	}
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

	id, err := ws.WriteHistory(agentName, writID, "session", "", time.Now(), nil)
	if err != nil {
		return "", fmt.Errorf("failed to create agent history: %w", err)
	}

	l.mu.Lock()
	l.sessions[key] = id
	l.mu.Unlock()

	l.logger.Printf("created history %s for %s/%s (writ: %s)", id, world, agentName, writID)
	return id, nil
}

// worldStore returns a cached store for the given world.
func (l *Ledger) worldStore(world string) (*store.Store, error) {
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
