package jsoncontract

import (
	"encoding/json"
	"testing"
)

// ---------------------------------------------------------------------------
// AssertJSONShape tests
// ---------------------------------------------------------------------------

func TestAssertJSONShape_ValidStruct(t *testing.T) {
	type sample struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}

	raw := []byte(`{"name":"test","status":"ok"}`)
	var s sample
	AssertJSONShape(t, raw, &s)

	if s.Name != "test" {
		t.Errorf("expected Name=test, got %s", s.Name)
	}
	if s.Status != "ok" {
		t.Errorf("expected Status=ok, got %s", s.Status)
	}
}

func TestAssertJSONShape_ExtraFieldFails(t *testing.T) {
	type sample struct {
		Name string `json:"name"`
	}

	raw := []byte(`{"name":"test","unexpected":"field"}`)

	// Use a sub-test so the expected Fatal doesn't kill the parent test.
	ft := &fakeTB{}
	AssertJSONShape(ft, raw, &sample{})

	if !ft.failed {
		t.Error("expected AssertJSONShape to fail on extra fields, but it passed")
	}
}

func TestAssertJSONShape_MalformedJSON(t *testing.T) {
	type sample struct {
		Name string `json:"name"`
	}

	ft := &fakeTB{}
	AssertJSONShape(ft, []byte(`not json`), &sample{})

	if !ft.failed {
		t.Error("expected AssertJSONShape to fail on malformed JSON")
	}
}

// ---------------------------------------------------------------------------
// RequireFields tests
// ---------------------------------------------------------------------------

func TestRequireFields_Present(t *testing.T) {
	raw := []byte(`{"name":"sol","version":"1.0","nested":{"key":"val"}}`)

	RequireFields(t, raw, "name", "version", "nested.key")
}

func TestRequireFields_MissingPath(t *testing.T) {
	raw := []byte(`{"name":"sol"}`)

	ft := &fakeTB{}
	RequireFields(ft, raw, "missing")

	if !ft.errored {
		t.Error("expected RequireFields to error on missing path")
	}
}

func TestRequireFields_NullValue(t *testing.T) {
	raw := []byte(`{"name":null}`)

	ft := &fakeTB{}
	RequireFields(ft, raw, "name")

	if !ft.errored {
		t.Error("expected RequireFields to error on null value")
	}
}

func TestRequireFields_ArrayIndex(t *testing.T) {
	raw := []byte(`{"items":[{"id":"a"},{"id":"b"}]}`)
	RequireFields(t, raw, "items.0.id", "items.1.id")
}

func TestRequireFields_ArrayIndexOutOfBounds(t *testing.T) {
	raw := []byte(`{"items":[{"id":"a"}]}`)

	ft := &fakeTB{}
	RequireFields(ft, raw, "items.5.id")

	if !ft.errored {
		t.Error("expected RequireFields to error on out-of-bounds array index")
	}
}

// ---------------------------------------------------------------------------
// resolvePath tests
// ---------------------------------------------------------------------------

func TestResolvePath_Nested(t *testing.T) {
	var data any
	_ = json.Unmarshal([]byte(`{"a":{"b":{"c":"deep"}}}`), &data)

	val, ok := resolvePath(data, "a.b.c")
	if !ok || val != "deep" {
		t.Errorf("expected 'deep', got %v (ok=%v)", val, ok)
	}
}

func TestResolvePath_TopLevel(t *testing.T) {
	var data any
	_ = json.Unmarshal([]byte(`{"key":"value"}`), &data)

	val, ok := resolvePath(data, "key")
	if !ok || val != "value" {
		t.Errorf("expected 'value', got %v (ok=%v)", val, ok)
	}
}

func TestResolvePath_Missing(t *testing.T) {
	var data any
	_ = json.Unmarshal([]byte(`{"a":"b"}`), &data)

	_, ok := resolvePath(data, "missing")
	if ok {
		t.Error("expected not found for missing path")
	}
}

// ---------------------------------------------------------------------------
// Integration tests (require sol binary build)
// ---------------------------------------------------------------------------

func TestRunCommand_Version(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// SetupEnv is needed to set SOL_HOME even for --version.
	SetupEnv(t)

	out := RunCommand(t, "--version")
	if len(out) == 0 {
		t.Error("expected non-empty output from sol --version")
	}
}

func TestRunCommandJSON_Doctor(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	SetupEnv(t)

	// Doctor --json returns {"checks": [...]}.
	var result struct {
		Checks []map[string]any `json:"checks"`
	}
	RunCommandJSON(t, &result, "doctor")

	if len(result.Checks) == 0 {
		t.Error("expected non-empty doctor checks")
	}

	// Verify structural fields exist via RequireFields on the full output.
	raw := RunCommand(t, "doctor", "--json")
	RequireFields(t, raw, "checks.0.name", "checks.0.passed")
}

func TestRunCommandJSON_DoctorShape(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	SetupEnv(t)

	raw := RunCommand(t, "doctor", "--json")

	// Parse the wrapper to extract individual checks.
	var wrapper struct {
		Checks []json.RawMessage `json:"checks"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		t.Fatalf("unmarshal wrapper: %v", err)
	}
	if len(wrapper.Checks) == 0 {
		t.Fatal("expected at least one doctor check")
	}

	// Verify first check matches expected shape strictly.
	type doctorCheck struct {
		Name    string `json:"name"`
		Passed  bool   `json:"passed"`
		Message string `json:"message"`
		Fix     string `json:"fix"`
	}
	var check doctorCheck
	AssertJSONShape(t, wrapper.Checks[0], &check)

	if check.Name == "" {
		t.Error("expected non-empty name in doctor check")
	}
}

// ---------------------------------------------------------------------------
// fakeTB — minimal testing.TB substitute for testing failure paths
// ---------------------------------------------------------------------------

type fakeTB struct {
	testing.TB
	failed  bool
	errored bool
	logs    []string
}

func (f *fakeTB) Helper() {}

func (f *fakeTB) Fatalf(format string, args ...any) {
	f.failed = true
}

func (f *fakeTB) Errorf(format string, args ...any) {
	f.errored = true
}

func (f *fakeTB) Logf(format string, args ...any) {
	// no-op
}
