package attrutil

import (
	"math"
	"strconv"
	"testing"
)

// ---- ParseInt ----

func TestParseInt(t *testing.T) {
	tests := []struct {
		name  string
		attrs map[string]string
		key   string
		want  int64
	}{
		{
			name:  "valid positive",
			attrs: map[string]string{"k": "42"},
			key:   "k",
			want:  42,
		},
		{
			name:  "valid negative",
			attrs: map[string]string{"k": "-7"},
			key:   "k",
			want:  -7,
		},
		{
			name:  "zero",
			attrs: map[string]string{"k": "0"},
			key:   "k",
			want:  0,
		},
		{
			name:  "max int64",
			attrs: map[string]string{"k": strconv.FormatInt(math.MaxInt64, 10)},
			key:   "k",
			want:  math.MaxInt64,
		},
		{
			name:  "min int64",
			attrs: map[string]string{"k": strconv.FormatInt(math.MinInt64, 10)},
			key:   "k",
			want:  math.MinInt64,
		},
		{
			name:  "key absent",
			attrs: map[string]string{"other": "1"},
			key:   "k",
			want:  0,
		},
		{
			name:  "empty map",
			attrs: map[string]string{},
			key:   "k",
			want:  0,
		},
		{
			// Reading from a nil map returns the zero value with ok=false in Go,
			// so ParseInt is safe on nil maps and behaves as if the key is absent.
			// Both call sites (claude/codex adapters) rely on this implicitly by
			// passing maps that may have been freshly allocated or copied.
			name:  "nil map safe",
			attrs: nil,
			key:   "k",
			want:  0,
		},
		{
			name:  "unparseable not-a-number",
			attrs: map[string]string{"k": "not-a-number"},
			key:   "k",
			want:  0,
		},
		{
			name:  "unparseable empty string",
			attrs: map[string]string{"k": ""},
			key:   "k",
			want:  0,
		},
		{
			name:  "unparseable float",
			attrs: map[string]string{"k": "1.5"},
			key:   "k",
			want:  0,
		},
		{
			name:  "unparseable overflow",
			attrs: map[string]string{"k": "99999999999999999999999"},
			key:   "k",
			want:  0,
		},
		{
			name:  "unparseable whitespace",
			attrs: map[string]string{"k": " 42 "},
			key:   "k",
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseInt(tt.attrs, tt.key)
			if got != tt.want {
				t.Errorf("ParseInt(%v, %q) = %d, want %d", tt.attrs, tt.key, got, tt.want)
			}
		})
	}
}

// ---- ParseFloat ----

func TestParseFloat(t *testing.T) {
	maxFloat := math.MaxFloat64
	smallestFloat := math.SmallestNonzeroFloat64

	tests := []struct {
		name  string
		attrs map[string]string
		key   string
		want  *float64 // nil means "expect nil"
	}{
		{
			name:  "valid positive",
			attrs: map[string]string{"k": "3.14"},
			key:   "k",
			want:  ptrFloat(3.14),
		},
		{
			name:  "valid negative",
			attrs: map[string]string{"k": "-2.5"},
			key:   "k",
			want:  ptrFloat(-2.5),
		},
		{
			name:  "zero",
			attrs: map[string]string{"k": "0"},
			key:   "k",
			want:  ptrFloat(0),
		},
		{
			name:  "valid integer-shaped",
			attrs: map[string]string{"k": "42"},
			key:   "k",
			want:  ptrFloat(42),
		},
		{
			name:  "exponent form",
			attrs: map[string]string{"k": "1e-6"},
			key:   "k",
			want:  ptrFloat(1e-6),
		},
		{
			name:  "max float64",
			attrs: map[string]string{"k": strconv.FormatFloat(maxFloat, 'g', -1, 64)},
			key:   "k",
			want:  &maxFloat,
		},
		{
			name:  "smallest nonzero float64",
			attrs: map[string]string{"k": strconv.FormatFloat(smallestFloat, 'g', -1, 64)},
			key:   "k",
			want:  &smallestFloat,
		},
		{
			name:  "key absent",
			attrs: map[string]string{"other": "1.0"},
			key:   "k",
			want:  nil,
		},
		{
			name:  "empty map",
			attrs: map[string]string{},
			key:   "k",
			want:  nil,
		},
		{
			// Reading from a nil map returns the zero value with ok=false in Go,
			// so ParseFloat is safe on nil maps and returns nil.
			name:  "nil map safe",
			attrs: nil,
			key:   "k",
			want:  nil,
		},
		{
			name:  "unparseable not-a-number",
			attrs: map[string]string{"k": "infinity-bad"},
			key:   "k",
			want:  nil,
		},
		{
			name:  "unparseable empty string",
			attrs: map[string]string{"k": ""},
			key:   "k",
			want:  nil,
		},
		{
			name:  "unparseable garbage",
			attrs: map[string]string{"k": "abc"},
			key:   "k",
			want:  nil,
		},
		{
			name:  "unparseable overflow",
			attrs: map[string]string{"k": "1e500"},
			key:   "k",
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseFloat(tt.attrs, tt.key)
			switch {
			case tt.want == nil && got != nil:
				t.Errorf("ParseFloat(%v, %q) = %v, want nil", tt.attrs, tt.key, *got)
			case tt.want != nil && got == nil:
				t.Errorf("ParseFloat(%v, %q) = nil, want %v", tt.attrs, tt.key, *tt.want)
			case tt.want != nil && got != nil && *got != *tt.want:
				t.Errorf("ParseFloat(%v, %q) = %v, want %v", tt.attrs, tt.key, *got, *tt.want)
			}
		})
	}
}

// ---- ParseIntPtr ----

func TestParseIntPtr(t *testing.T) {
	maxInt := int64(math.MaxInt64)
	minInt := int64(math.MinInt64)

	tests := []struct {
		name  string
		attrs map[string]string
		key   string
		want  *int64 // nil means "expect nil"
	}{
		{
			name:  "valid positive",
			attrs: map[string]string{"k": "42"},
			key:   "k",
			want:  ptrInt64(42),
		},
		{
			name:  "valid negative",
			attrs: map[string]string{"k": "-7"},
			key:   "k",
			want:  ptrInt64(-7),
		},
		{
			name:  "zero",
			attrs: map[string]string{"k": "0"},
			key:   "k",
			want:  ptrInt64(0),
		},
		{
			name:  "max int64",
			attrs: map[string]string{"k": strconv.FormatInt(math.MaxInt64, 10)},
			key:   "k",
			want:  &maxInt,
		},
		{
			name:  "min int64",
			attrs: map[string]string{"k": strconv.FormatInt(math.MinInt64, 10)},
			key:   "k",
			want:  &minInt,
		},
		{
			name:  "key absent",
			attrs: map[string]string{"other": "1"},
			key:   "k",
			want:  nil,
		},
		{
			name:  "empty map",
			attrs: map[string]string{},
			key:   "k",
			want:  nil,
		},
		{
			// Reading from a nil map returns the zero value with ok=false in Go,
			// so ParseIntPtr is safe on nil maps and returns nil.
			name:  "nil map safe",
			attrs: nil,
			key:   "k",
			want:  nil,
		},
		{
			name:  "unparseable not-a-number",
			attrs: map[string]string{"k": "not-a-number"},
			key:   "k",
			want:  nil,
		},
		{
			name:  "unparseable empty string",
			attrs: map[string]string{"k": ""},
			key:   "k",
			want:  nil,
		},
		{
			name:  "unparseable float",
			attrs: map[string]string{"k": "1.5"},
			key:   "k",
			want:  nil,
		},
		{
			name:  "unparseable overflow",
			attrs: map[string]string{"k": "99999999999999999999999"},
			key:   "k",
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseIntPtr(tt.attrs, tt.key)
			switch {
			case tt.want == nil && got != nil:
				t.Errorf("ParseIntPtr(%v, %q) = %d, want nil", tt.attrs, tt.key, *got)
			case tt.want != nil && got == nil:
				t.Errorf("ParseIntPtr(%v, %q) = nil, want %d", tt.attrs, tt.key, *tt.want)
			case tt.want != nil && got != nil && *got != *tt.want:
				t.Errorf("ParseIntPtr(%v, %q) = %d, want %d", tt.attrs, tt.key, *got, *tt.want)
			}
		})
	}
}

// ---- helpers ----

func ptrFloat(f float64) *float64 { return &f }
func ptrInt64(n int64) *int64     { return &n }
