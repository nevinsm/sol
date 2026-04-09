package sessionsave

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeSender is a synchronous in-memory Sender used by all tests in this
// file. No tmux involvement.
type fakeSender struct {
	mu sync.Mutex

	// inject behavior
	injectErr     error
	injectCalls   int
	lastSession   string
	lastPromptTxt string
	lastSubmit    bool

	// capture behavior: a queue of (sample, err) pairs returned in order.
	// When the queue is exhausted, the last entry repeats — that lets a
	// test set "now stable forever" with a single trailing entry.
	captureQueue []captureResult
	captureCalls int
}

type captureResult struct {
	out string
	err error
}

func (f *fakeSender) Inject(name, text string, submit bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.injectCalls++
	f.lastSession = name
	f.lastPromptTxt = text
	f.lastSubmit = submit
	return f.injectErr
}

func (f *fakeSender) Capture(name string, lines int) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.captureCalls++
	if len(f.captureQueue) == 0 {
		return "", nil
	}
	if f.captureCalls-1 < len(f.captureQueue) {
		r := f.captureQueue[f.captureCalls-1]
		return r.out, r.err
	}
	r := f.captureQueue[len(f.captureQueue)-1]
	return r.out, r.err
}

// fastOpts returns Options tuned for unit tests: tight intervals so tests
// finish in tens of milliseconds, generous-enough Timeout for the slowest
// "never stabilize" case to actually hit the timeout deterministically.
func fastOpts() Options {
	return Options{
		PollInterval:    5 * time.Millisecond,
		StabilityWindow: 20 * time.Millisecond,
		Timeout:         200 * time.Millisecond,
	}
}

func TestPrompt_StabilizesQuickly(t *testing.T) {
	f := &fakeSender{
		// First two captures change, then output settles to a constant
		// "idle" sample which is repeated forever.
		captureQueue: []captureResult{
			{out: "working line 1"},
			{out: "working line 2"},
			{out: "idle"},
			{out: "idle"},
		},
	}

	start := time.Now()
	err := Prompt(f, "sol-test-Echo", EnvoyStopPrompt, fastOpts())
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Prompt() error = %v, want nil", err)
	}
	if f.injectCalls != 1 {
		t.Errorf("inject calls = %d, want 1", f.injectCalls)
	}
	if f.lastSession != "sol-test-Echo" {
		t.Errorf("session name = %q, want %q", f.lastSession, "sol-test-Echo")
	}
	if f.lastPromptTxt != EnvoyStopPrompt {
		t.Errorf("prompt text = %q, want EnvoyStopPrompt", f.lastPromptTxt)
	}
	if !f.lastSubmit {
		t.Error("submit = false, want true (prompt must be submitted)")
	}
	if f.captureCalls < 3 {
		t.Errorf("capture calls = %d, want >= 3 (need multiple polls to detect stability)", f.captureCalls)
	}
	// Should have returned well before the timeout once the sample settled.
	if elapsed >= fastOpts().Timeout {
		t.Errorf("elapsed = %v, want < timeout (%v) — should have returned on stability, not timeout", elapsed, fastOpts().Timeout)
	}
}

func TestPrompt_TimesOutWhenNeverStable(t *testing.T) {
	// Capture returns a different value every call (counter encoded into
	// the string), so the stability window can never be satisfied.
	calls := 0
	f := &neverStableSender{counter: &calls}

	opts := fastOpts()
	start := time.Now()
	err := Prompt(f, "sol-test-Echo", EnvoyStopPrompt, opts)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Prompt() error = %v, want nil (timeout is best-effort success)", err)
	}
	if elapsed < opts.Timeout {
		t.Errorf("elapsed = %v, want >= timeout (%v)", elapsed, opts.Timeout)
	}
	if calls < 2 {
		t.Errorf("capture calls = %d, want at least 2", calls)
	}
}

// neverStableSender returns a different capture sample every call so the
// stability window cannot be satisfied. Implemented separately from
// fakeSender so the queue logic does not need to grow unbounded.
type neverStableSender struct {
	mu      sync.Mutex
	counter *int
}

func (n *neverStableSender) Inject(name, text string, submit bool) error { return nil }
func (n *neverStableSender) Capture(name string, lines int) (string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	*n.counter++
	return time.Now().Format("15:04:05.000000000"), nil
}

func TestPrompt_InjectError(t *testing.T) {
	wantErr := errors.New("tmux: session not found")
	f := &fakeSender{injectErr: wantErr}

	err := Prompt(f, "sol-test-Echo", EnvoyStopPrompt, fastOpts())

	if !errors.Is(err, wantErr) {
		t.Fatalf("Prompt() error = %v, want %v", err, wantErr)
	}
	if f.captureCalls != 0 {
		t.Errorf("capture calls = %d, want 0 (must not poll after inject failure)", f.captureCalls)
	}
}

func TestPrompt_CaptureErrorsTolerated(t *testing.T) {
	// Capture errors are tolerated: polling continues, and the function
	// still terminates (here, via timeout). The point of this test is to
	// confirm capture errors do not return early.
	f := &fakeSender{
		captureQueue: []captureResult{
			{err: errors.New("tmux: capture failed")},
			{err: errors.New("tmux: capture failed")},
			{err: errors.New("tmux: capture failed")},
		},
	}

	opts := fastOpts()
	err := Prompt(f, "sol-test-Echo", EnvoyStopPrompt, opts)
	if err != nil {
		t.Fatalf("Prompt() error = %v, want nil (capture errors are best-effort)", err)
	}
	if f.captureCalls < 2 {
		t.Errorf("capture calls = %d, want >= 2 (must keep polling through errors)", f.captureCalls)
	}
}

func TestPrompt_CaptureRecoversAfterError(t *testing.T) {
	// First capture errors, then output goes stable. Prompt must not have
	// terminated on the error and must detect the eventual stability.
	f := &fakeSender{
		captureQueue: []captureResult{
			{err: errors.New("transient")},
			{out: "idle"},
			{out: "idle"},
			{out: "idle"},
		},
	}

	start := time.Now()
	err := Prompt(f, "sol-test-Echo", EnvoyStopPrompt, fastOpts())
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Prompt() error = %v, want nil", err)
	}
	if elapsed >= fastOpts().Timeout {
		t.Errorf("elapsed = %v, want < timeout — should recover and detect stability", elapsed)
	}
}

func TestApplyDefaults(t *testing.T) {
	tests := []struct {
		name string
		in   Options
		want Options
	}{
		{
			name: "all zero -> all defaults",
			in:   Options{},
			want: Options{
				PollInterval:    500 * time.Millisecond,
				StabilityWindow: 3 * time.Second,
				Timeout:         30 * time.Second,
			},
		},
		{
			name: "partial override preserved",
			in: Options{
				PollInterval: 100 * time.Millisecond,
			},
			want: Options{
				PollInterval:    100 * time.Millisecond,
				StabilityWindow: 3 * time.Second,
				Timeout:         30 * time.Second,
			},
		},
		{
			name: "all set -> nothing replaced",
			in: Options{
				PollInterval:    1 * time.Second,
				StabilityWindow: 5 * time.Second,
				Timeout:         60 * time.Second,
			},
			want: Options{
				PollInterval:    1 * time.Second,
				StabilityWindow: 5 * time.Second,
				Timeout:         60 * time.Second,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.in
			got.applyDefaults()
			if got != tt.want {
				t.Errorf("applyDefaults() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// Compile-time guard: the package's exported Sender is satisfied by anything
// implementing the canonical method set used by callers (envoy.StopManager,
// session.SessionManager). This is just a documentation test that the narrow
// interface stays narrow.
var _ Sender = (*fakeSender)(nil)
var _ Sender = (*neverStableSender)(nil)
