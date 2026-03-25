package style

import "testing"

func TestFormatTokenInt(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{1234567, "1,234,567"},
		{-1, "-1"},
		{-999, "-999"},
		{-1234, "-1,234"},
		{-1234567, "-1,234,567"},
		{-100, "-100"},
	}

	for _, tt := range tests {
		got := FormatTokenInt(tt.input)
		if got != tt.want {
			t.Errorf("FormatTokenInt(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
