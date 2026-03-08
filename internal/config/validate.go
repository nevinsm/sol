package config

import (
	"fmt"
	"strings"
)

// ValidateWritID checks that a writ ID has the expected format: sol-<16 hex chars>.
func ValidateWritID(id string) error {
	if !strings.HasPrefix(id, "sol-") || len(id) != 20 {
		return fmt.Errorf("invalid writ ID %q: expected format sol-<16 hex chars>", id)
	}
	return nil
}

// ValidateCaravanID checks that a caravan ID has the expected format: car-<16 hex chars>.
func ValidateCaravanID(id string) error {
	if !strings.HasPrefix(id, "car-") || len(id) != 20 {
		return fmt.Errorf("invalid caravan ID %q: expected format car-<16 hex chars>", id)
	}
	return nil
}
