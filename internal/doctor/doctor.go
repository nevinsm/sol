package doctor

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/nevinsm/sol/internal/config"
	_ "modernc.org/sqlite"
)

const minTmuxMajor = 3
const minTmuxMinor = 1

// CheckResult represents the outcome of a single prerequisite check.
//
// Most checks are binary: Passed is true or false. A small number of
// checks surface advisory conditions that operators should know about but
// that do not block sol from running (e.g. pending migrations). Those
// checks set Passed=true and Warning=true, and the human-readable doctor
// output renders them with a ⚠ indicator instead of ✓.
type CheckResult struct {
	Name    string `json:"name"` // short identifier: "tmux", "git", "claude", etc.
	Passed  bool   `json:"passed"`
	Warning bool   `json:"warning,omitempty"` // advisory: passed but operator should notice
	Message string `json:"message"`           // human-readable status or error detail
	Fix     string `json:"fix"`               // actionable fix suggestion (empty if passed)
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

// CheckTmux verifies tmux is installed, executable, and meets the minimum
// version requirement (3.1+).
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
	return checkTmuxVersion(version, path)
}

// checkTmuxVersion validates the tmux version string against the minimum
// required version. Extracted for testability.
func checkTmuxVersion(version, path string) CheckResult {
	major, minor, ok := parseTmuxVersion(version)
	if !ok {
		// Unparseable version — pass with warning rather than blocking.
		return CheckResult{
			Name:    "tmux",
			Passed:  true,
			Warning: true,
			Message: fmt.Sprintf("%s (%s) — warning: could not parse version, minimum %d.%d required", version, path, minTmuxMajor, minTmuxMinor),
			Fix:     fmt.Sprintf("Ensure tmux %d.%d+ is installed: 'tmux -V' should print a recognizable version", minTmuxMajor, minTmuxMinor),
		}
	}

	if major < minTmuxMajor || (major == minTmuxMajor && minor < minTmuxMinor) {
		return CheckResult{
			Name:    "tmux",
			Passed:  false,
			Message: fmt.Sprintf("tmux %d.%d found, but sol requires tmux %d.%d or later", major, minor, minTmuxMajor, minTmuxMinor),
			Fix:     "Upgrade tmux: 'brew upgrade tmux' (macOS) or 'apt install --only-upgrade tmux' (Linux)",
		}
	}

	return CheckResult{
		Name:    "tmux",
		Passed:  true,
		Message: fmt.Sprintf("%s (%s)", version, path),
	}
}

// tmuxVersionRe matches version strings like "tmux 3.5a", "tmux 3.1",
// or "tmux next-3.4". It captures the major.minor numeric portion.
var tmuxVersionRe = regexp.MustCompile(`(\d+)\.(\d+)`)

// parseTmuxVersion extracts the major and minor version from a tmux -V
// output string. Returns (major, minor, true) on success, or (0, 0, false)
// if the version cannot be parsed.
func parseTmuxVersion(versionStr string) (int, int, bool) {
	m := tmuxVersionRe.FindStringSubmatch(versionStr)
	if m == nil {
		return 0, 0, false
	}
	major, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, 0, false
	}
	minor, err := strconv.Atoi(m[2])
	if err != nil {
		return 0, 0, false
	}
	return major, minor, true
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
// database and enabling WAL. The test database is created inside
// SOL_HOME so the check exercises the actual target filesystem.
// Falls back to the system temp directory if SOL_HOME does not exist yet.
func CheckSQLiteWAL() CheckResult {
	base := config.Home()
	if _, err := os.Stat(base); err != nil {
		base = ""
	}
	dir, err := os.MkdirTemp(base, "sol-doctor-wal-*")
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

	// Check .env files: sphere-level and any discovered world-level files.
	solHome := config.Home()
	worlds, err := discoverWorlds(solHome)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		// Surface the discovery failure explicitly rather than silently
		// pretending there are no worlds — the doctor's purpose is to
		// report problems. A missing SOL_HOME is already reported by
		// CheckSOLHome (the "will be created by 'sol init'" path), so
		// we only escalate other errors here (permission denied, etc.).
		report.Checks = append(report.Checks, CheckResult{
			Name:    "world_discovery",
			Passed:  false,
			Message: fmt.Sprintf("failed to scan SOL_HOME for worlds: %v", err),
			Fix:     "Ensure $SOL_HOME exists and is readable",
		})
	}
	report.Checks = append(report.Checks, CheckEnvFiles(solHome, worlds)...)

	// Check runtime binaries for all configured worlds.
	report.Checks = append(report.Checks, CheckRuntimeBinaries(worlds)...)

	// Check credential file permissions across the sphere.
	report.Checks = append(report.Checks, CheckCredentialPermissions(solHome, worlds))

	// Check for pending migrations (advisory warning, not a blocker).
	report.Checks = append(report.Checks, CheckMigrations())
	return report
}

// CheckRuntimeBinaries discovers configured runtimes from all world configs
// and verifies the required binary exists on PATH for each. The "claude"
// runtime is skipped because CheckClaude() already covers it.
//
// Worlds whose world.toml fails to parse are surfaced as "world_config:{world}"
// findings rather than silently dropped — without this, a malformed world is
// invisible to doctor and operators only learn about the parse failure when
// they try to run something against the world.
func CheckRuntimeBinaries(worlds []string) []CheckResult {
	// Collect runtimes → set of worlds that need them.
	runtimeWorlds := make(map[string][]string)
	roles := []string{"outpost", "envoy", "forge", "sentinel"}

	var results []CheckResult
	for _, world := range worlds {
		cfg, err := config.LoadWorldConfig(world)
		if err != nil {
			// Surface the parse/load failure as a doctor finding so the
			// operator learns about it. The world is then skipped for
			// runtime collection because we have no config to inspect.
			results = append(results, CheckResult{
				Name:    fmt.Sprintf("world_config:%s", world),
				Passed:  false,
				Message: fmt.Sprintf("failed to load world config for %q: %v", world, err),
				Fix:     fmt.Sprintf("Fix syntax in %s", config.WorldConfigPath(world)),
			})
			continue
		}
		seen := make(map[string]bool)
		for _, role := range roles {
			rt := cfg.ResolveRuntime(role)
			if !seen[rt] {
				seen[rt] = true
				runtimeWorlds[rt] = append(runtimeWorlds[rt], world)
			}
		}
	}

	// Sort runtime keys so output is deterministic across calls.
	runtimes := make([]string, 0, len(runtimeWorlds))
	for rt := range runtimeWorlds {
		runtimes = append(runtimes, rt)
	}
	sort.Strings(runtimes)
	for _, rt := range runtimes {
		if rt == "claude" {
			// Already checked by CheckClaude().
			continue
		}
		results = append(results, checkRuntimeBinary(rt, runtimeWorlds[rt]))
	}
	return results
}

// checkRuntimeBinary checks that a runtime binary exists on PATH and reports
// which worlds require it.
func checkRuntimeBinary(runtime string, worlds []string) CheckResult {
	name := fmt.Sprintf("runtime:%s", runtime)
	worldList := strings.Join(worlds, ", ")

	path, err := exec.LookPath(runtime)
	if err != nil {
		return CheckResult{
			Name:    name,
			Passed:  false,
			Message: fmt.Sprintf("%s not found in PATH (required by worlds: %s)", runtime, worldList),
			Fix:     fmt.Sprintf("Install the %s CLI or update world configs to use a different runtime", runtime),
		}
	}
	return CheckResult{
		Name:    name,
		Passed:  true,
		Message: fmt.Sprintf("found at %s (used by worlds: %s)", path, worldList),
	}
}

// credentialFileNames lists the basenames of files written by sol's runtime
// adapters that contain (or symlink) authentication tokens. Files matching
// these names under a world directory are checked by CheckCredentialPermissions.
//
//   - auth.json: codex adapter writes this under {world}/{role}s/{agent}/.codex-home/
//     containing the API key in plaintext JSON.
//   - .credentials.json: claude adapter creates this as a symlink under
//     {world}/.claude-config/{role}/{agent}/ pointing at an account's token.json.
var credentialFileNames = map[string]bool{
	"auth.json":         true,
	".credentials.json": true,
}

// CheckCredentialPermissions walks the credential-bearing directories and
// reports any files with overly-permissive modes (any group- or world-readable
// bit set, i.e., mode bits beyond 0600).
//
// Walks:
//   - $SOL_HOME/.accounts/ — sphere-level account credentials (token.json files)
//     written by `sol account add`. Every regular file under .accounts/ is
//     considered sensitive.
//   - For each world, $SOL_HOME/{world}/ — searched for files named auth.json
//     (codex adapter) or .credentials.json (claude adapter). Symlinks are
//     skipped because the target's permissions are governed where they live
//     (under .accounts/) and a symlink's own mode bits are not meaningful.
//
// Returns a single aggregated CheckResult so the doctor output stays compact
// regardless of how many credential files exist. A passing result lists the
// number of files inspected; a failing result lists each offending file.
//
// Skipped on Windows: file mode bits there are unreliable and don't carry the
// same security meaning as POSIX permission bits.
func CheckCredentialPermissions(solHome string, worlds []string) CheckResult {
	const name = "credential_permissions"

	if runtime.GOOS == "windows" {
		return CheckResult{
			Name:    name,
			Passed:  true,
			Message: "skipped on Windows (file mode bits not enforced)",
		}
	}

	var bad []string
	var inspected int

	// 1. Walk .accounts/ — every regular file is treated as sensitive.
	accounts := filepath.Join(solHome, ".accounts")
	if info, err := os.Stat(accounts); err == nil && info.IsDir() {
		_ = filepath.WalkDir(accounts, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil // tolerate transient errors; keep scanning siblings
			}
			if !d.Type().IsRegular() {
				return nil
			}
			fi, err := d.Info()
			if err != nil {
				return nil
			}
			inspected++
			if fi.Mode().Perm()&0o077 != 0 {
				bad = append(bad, fmt.Sprintf("%s (mode %04o)", path, fi.Mode().Perm()))
			}
			return nil
		})
	}

	// 2. For each world, walk for known credential filenames.
	for _, world := range worlds {
		worldDir := filepath.Join(solHome, world)
		_ = filepath.WalkDir(worldDir, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if !credentialFileNames[d.Name()] {
				return nil
			}
			// Skip symlinks: the target's permissions (under .accounts/) are
			// what matter, and symlink mode bits are typically 0777 without
			// implying any access. .accounts/ is already walked above.
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			fi, err := d.Info()
			if err != nil {
				return nil
			}
			if !fi.Mode().IsRegular() {
				return nil
			}
			inspected++
			if fi.Mode().Perm()&0o077 != 0 {
				bad = append(bad, fmt.Sprintf("%s (mode %04o)", path, fi.Mode().Perm()))
			}
			return nil
		})
	}

	if len(bad) == 0 {
		return CheckResult{
			Name:    name,
			Passed:  true,
			Message: fmt.Sprintf("%d credential file(s) inspected, all with mode 0600 or stricter", inspected),
		}
	}
	sort.Strings(bad)
	return CheckResult{
		Name:    name,
		Passed:  false,
		Message: fmt.Sprintf("%d credential file(s) with group- or world-accessible permissions:\n  %s", len(bad), strings.Join(bad, "\n  ")),
		Fix:     "Restrict permissions on each listed file: chmod 600 <path>",
	}
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
