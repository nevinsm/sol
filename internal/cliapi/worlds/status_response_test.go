package worlds

import (
	"encoding/json"
	"testing"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/status"
)

func TestStatusResponseJSON(t *testing.T) {
	resp := StatusResponse{
		WorldStatus: &status.WorldStatus{
			World: "sol-dev",
		},
		Config: config.WorldConfig{},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got["world"] != "sol-dev" {
		t.Errorf("world = %v, want %q", got["world"], "sol-dev")
	}
	if _, ok := got["config"]; !ok {
		t.Error("missing config key")
	}
}
