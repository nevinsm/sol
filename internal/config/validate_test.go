package config

import (
	"strings"
	"testing"
)

func TestValidateWritID(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid ID",
			id:      "sol-a1b2c3d4e5f6a7b8",
			wantErr: false,
		},
		{
			name:    "wrong prefix",
			id:      "car-a1b2c3d4e5f6a7b8",
			wantErr: true,
			errMsg:  "invalid writ ID",
		},
		{
			name:    "wrong length short",
			id:      "sol-abc123",
			wantErr: true,
			errMsg:  "invalid writ ID",
		},
		{
			name:    "wrong length long",
			id:      "sol-a1b2c3d4e5f6a7b8extra",
			wantErr: true,
			errMsg:  "invalid writ ID",
		},
		{
			name:    "empty",
			id:      "",
			wantErr: true,
			errMsg:  "invalid writ ID",
		},
		{
			name:    "no prefix",
			id:      "a1b2c3d4e5f6a7b8abcd",
			wantErr: true,
			errMsg:  "invalid writ ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWritID(tt.id)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for ID %q, got nil", tt.id)
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Fatalf("expected error containing %q, got: %v", tt.errMsg, err)
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error for ID %q, got: %v", tt.id, err)
				}
			}
		})
	}
}

func TestValidateCaravanID(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid ID",
			id:      "car-a1b2c3d4e5f6a7b8",
			wantErr: false,
		},
		{
			name:    "wrong prefix",
			id:      "sol-a1b2c3d4e5f6a7b8",
			wantErr: true,
			errMsg:  "invalid caravan ID",
		},
		{
			name:    "wrong length short",
			id:      "car-abc123",
			wantErr: true,
			errMsg:  "invalid caravan ID",
		},
		{
			name:    "wrong length long",
			id:      "car-a1b2c3d4e5f6a7b8extra",
			wantErr: true,
			errMsg:  "invalid caravan ID",
		},
		{
			name:    "empty",
			id:      "",
			wantErr: true,
			errMsg:  "invalid caravan ID",
		},
		{
			name:    "no prefix",
			id:      "a1b2c3d4e5f6a7b8abcd",
			wantErr: true,
			errMsg:  "invalid caravan ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCaravanID(tt.id)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for ID %q, got nil", tt.id)
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Fatalf("expected error containing %q, got: %v", tt.errMsg, err)
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error for ID %q, got: %v", tt.id, err)
				}
			}
		})
	}
}
