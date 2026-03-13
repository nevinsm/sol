package doctor

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nevinsm/sol/internal/config"
	_ "modernc.org/sqlite"
)

// CheckResult represents the outcome of a single prerequisite check.
type CheckResult struct {
	Name    string `json:"name"` // short identifier: "tmux", "git", "claude", etc.
	Passed  bool   `json:"passed"`
	Message string `json:"message"` // human-readable status or error detail
	Fix     string `json:"fix"`     // actionable fix suggestion (empty if passed)
}

// Report holds the results of all prerequisite checks.
type Report struct {
	Checks []CheckResult `json:"checks"`
}

// AllPassed returns true if every check passed.
func (r *Report) AllPassed() bool {
	for _, c := range r.Checks {
		if !c.Passed {
			return false
		}
	}
	return true
}

// FailedCount returns the number of failed checks.
func (r *Report) FailedCount() int {
	n := 0
	for _, c := range r.Checks {
		if !c.Passed {
			n++
		}
	}
	return n
}

// CheckTmux verifies tmux is installed and executable.
func CheckTmux() CheckResult {
	path, err := exec.LookPath("tmux")
	if err != nil {
		return CheckResult{
			Name:    "tmux",
			Passed:  false,
			Message: "tmux not found in PATH",
			Fix:     "Install tmux: 'brew install tmux' (macOS) or 'apt install tmux' (Linux)",
		}
	}
	// Run tmux -V to get version string.
	out, err := exec.Command(path, "-V").Output()
	if err != nil {
		return CheckResult{
			Name:    "tmux",
			Passed:  false,
			Message: fmt.Sprintf("tmux found at %s but failed to run: %v", path, err),
			Fix:     "Check tmux installation — it may be corrupted or missing dependencies",
		}
	}
	version := strings.TrimSpace(string(out))
	return CheckResult{
		Name:    "tmux",
		Passed:  true,
		Message: fmt.Sprintf("%s (%s)", version, path),
	}
}

// CheckGit verifies git is installed and executable.
func CheckGit() CheckResult {
	path, err := exec.LookPath("git")
	if err != nil {
		return CheckResult{
			Name:    "git",
			Passed:  false,
			Message: "git not found in PATH",
			Fix:     "Install git: 'brew install git' (macOS) or 'apt install git' (Linux)",
		}
	}
	out, err := exec.Command(path, "--version").Output()
	if err != nil {
		return CheckResult{
			Name:    "git",
			Passed:  false,
			Message: fmt.Sprintf("git found at %s but failed to run: %v", path, err),
			Fix:     "Check git installation",
		}
	}
	version := strings.TrimSpace(string(out))
	return CheckResult{
		Name:    "git",
		Passed:  true,
		Message: fmt.Sprintf("%s (%s)", version, path),
	}
}

// CheckClaude verifies the Claude CLI is installed and executable.
func CheckClaude() CheckResult {
	path, err := exec.LookPath("claude")
	if err != nil {
		return CheckResult{
			Name:    "claude",
			Passed:  false,
			Message: "claude CLI not found in PATH",
			Fix:     "Install Claude Code: npm install -g @anthropic-ai/claude-code",
		}
	}
	// Just verify it's executable — don't run a full command
	// as that might trigger auth flows.
	return CheckResult{
		Name:    "claude",
		Passed:  true,
		Message: fmt.Sprintf("found at %s", path),
	}
}

// CheckJq verifies jq is installed and executable.
// Required by the apikey-helper.sh script that reads OAuth tokens
// from broker-managed credentials files.
func CheckJq() CheckResult {
	path, err := exec.LookPath("jq")
	if err != nil {
		return CheckResult{
			Name:    "jq",
			Passed:  false,
			Message: "jq not found in PATH",
			Fix:     "Install jq: 'brew install jq' (macOS) or 'apt install jq' (Linux)",
		}
	}
	out, err := exec.Command(path, "--version").Output()
	if err != nil {
		return CheckResult{
			Name:    "jq",
			Passed:  false,
			Message: fmt.Sprintf("jq found at %s but failed to run: %v", path, err),
			Fix:     "Check jq installation",
		}
	}
	version := strings.TrimSpace(string(out))
	return CheckResult{
		Name:    "jq",
		Passed:  true,
		Message: fmt.Sprintf("%s (%s)", version, path),
	}
}

// CheckSOLHome verifies SOL_HOME exists and is writable.
// If SOL_HOME doesn't exist yet, checks that the parent directory is
// writable (so init can create it).
func CheckSOLHome() CheckResult {
	home := config.Home()

	info, err := os.Stat(home)
	if os.IsNotExist(err) {
		// SOL_HOME doesn't exist — check parent is writable.
		parent := filepath.Dir(home)
		if err := checkWritable(parent); err != nil {
			return CheckResult{
				Name:    "sol_home",
				Passed:  false,
				Message: fmt.Sprintf("SOL_HOME (%s) does not exist and parent is not writable", home),
				Fix:     fmt.Sprintf("Create directory manually: mkdir -p %s", home),
			}
		}
		return CheckResult{
			Name:    "sol_home",
			Passed:  true,
			Message: fmt.Sprintf("%s (will be created by 'sol init')", home),
		}
	} else if err != nil {
		return CheckResult{
			Name:    "sol_home",
			Passed:  false,
			Message: fmt.Sprintf("cannot stat SOL_HOME (%s): %v", home, err),
			Fix:     "Check directory permissions",
		}
	}

	if !info.IsDir() {
		return CheckResult{
			Name:    "sol_home",
			Passed:  false,
			Message: fmt.Sprintf("SOL_HOME (%s) exists but is not a directory", home),
			Fix:     fmt.Sprintf("Remove the file and create directory: rm %s && mkdir -p %s", home, home),
		}
	}

	if err := checkWritable(home); err != nil {
		return CheckResult{
			Name:    "sol_home",
			Passed:  false,
			Message: fmt.Sprintf("SOL_HOME (%s) is not writable", home),
			Fix:     fmt.Sprintf("Fix permissions: chmod u+w %s", home),
		}
	}

	return CheckResult{
		Name:    "sol_home",
		Passed:  true,
		Message: home,
	}
}

// CheckSQLiteWAL verifies SQLite WAL mode works by creating a temp
// database and enabling WAL.
func CheckSQLiteWAL() CheckResult {
	dir, err := os.MkdirTemp("", "sol-doctor-wal-*")
	if err != nil {
		return CheckResult{
			Name:    "sqlite_wal",
			Passed:  false,
			Message: fmt.Sprintf("cannot create temp directory: %v", err),
			Fix:     "Check temp directory permissions and disk space",
		}
	}
	defer os.RemoveAll(dir)

	dbPath := filepath.Join(dir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return CheckResult{
			Name:    "sqlite_wal",
			Passed:  false,
			Message: fmt.Sprintf("cannot open SQLite database: %v", err),
			Fix:     "This is unexpected with the embedded SQLite driver — file a bug",
		}
	}
	defer db.Close()

	var mode string
	err = db.QueryRow("PRAGMA journal_mode=WAL").Scan(&mode)
	if err != nil {
		return CheckResult{
			Name:    "sqlite_wal",
			Passed:  false,
			Message: fmt.Sprintf("failed to enable WAL mode: %v", err),
			Fix:     "Check filesystem supports WAL (some network filesystems don't)",
		}
	}

	if strings.ToLower(mode) != "wal" {
		return CheckResult{
			Name:    "sqlite_wal",
			Passed:  false,
			Message: fmt.Sprintf("WAL mode not supported (got journal_mode=%s)", mode),
			Fix:     "SOL_HOME must be on a local filesystem that supports WAL locks",
		}
	}

	return CheckResult{
		Name:    "sqlite_wal",
		Passed:  true,
		Message: "WAL mode supported",
	}
}

// RunAll executes all prerequisite checks and returns a report.
// Checks always run in full — a failing check does not short-circuit.
func RunAll() *Report {
	report := &Report{}
	report.Checks = append(report.Checks, CheckTmux())
	report.Checks = append(report.Checks, CheckGit())
	report.Checks = append(report.Checks, CheckClaude())
	report.Checks = append(report.Checks, CheckJq())
	report.Checks = append(report.Checks, CheckSOLHome())
	report.Checks = append(report.Checks, CheckSQLiteWAL())
	report.Checks = append(report.Checks, CheckEnvFiles()...)
	return report
}

// checkWritable tests if a directory is writable by creating and
// removing a temp file.
func checkWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".sol-doctor-*")
	if err != nil {
		return err
	}
	name := f.Name()
	_ = f.Close()
	return os.Remove(name)
}
