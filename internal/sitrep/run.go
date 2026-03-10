package sitrep

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/config"
)

// Run invokes the Claude CLI with the given prompt and returns the response.
func Run(ctx context.Context, cfg config.SitrepSection, prompt string) (string, error) {
	timeout, err := time.ParseDuration(cfg.Timeout)
	if err != nil {
		timeout = 60 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	assessCmd := cfg.AssessCommand
	if assessCmd == "" {
		assessCmd = "claude"
	}

	model := cfg.Model
	if model == "" {
		model = "claude-sonnet-4-6"
	}

	cmd := exec.CommandContext(ctx, assessCmd, "-p", "--model", model)
	cmd.Stdin = strings.NewReader(prompt)

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("sitrep assessment failed: %w", err)
	}

	return strings.TrimSpace(stdout.String()), nil
}
