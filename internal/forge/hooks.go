package forge

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nevinsm/sol/internal/config"
)

// MergeHooksDir returns $SOL_HOME/{world}/forge/hooks.
func MergeHooksDir(world string) string {
	return filepath.Join(config.Home(), world, "forge", "hooks")
}

const commitMsgHook = `#!/bin/sh
# Written by sol forge per merge task. Replaces the whole commit message with
# the canonical "{writ title} ({writ ID})" line stored in message.txt.
dir=$(dirname "$0")
cat "$dir/message.txt" > "$1"
`

// prePushHookFmt is formatted with the writ ID (twice).
const prePushHookFmt = `#!/bin/sh
# Written by sol forge per merge task. Refuses to push unless HEAD's subject
# carries the writ tag.
subject=$(git log -1 --format=%%s HEAD)
case "$subject" in
  *"(%[1]s)"*) exit 0 ;;
esac
echo "sol forge: refusing to push: HEAD subject must end with (%[1]s), got: $subject" >&2
exit 1
`

// WriteMergeHooks (over)writes the per-task git hooks dir for a merge session
// and returns its path. message.txt holds the canonical message as raw bytes
// so titles with quotes, backticks, or $ need no shell escaping.
func WriteMergeHooks(world, title, writID string) (string, error) {
	dir := MergeHooksDir(world)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create hooks dir %q: %w", dir, err)
	}
	files := []struct {
		name, content string
		mode          os.FileMode
	}{
		{"message.txt", fmt.Sprintf("%s (%s)\n", title, writID), 0o644},
		{"commit-msg", commitMsgHook, 0o755},
		{"pre-push", fmt.Sprintf(prePushHookFmt, writID), 0o755},
	}
	for _, f := range files {
		path := filepath.Join(dir, f.name)
		if err := os.WriteFile(path, []byte(f.content), f.mode); err != nil {
			return "", fmt.Errorf("failed to write %q: %w", path, err)
		}
		if err := os.Chmod(path, f.mode); err != nil {
			return "", fmt.Errorf("failed to chmod %q: %w", path, err)
		}
	}
	return dir, nil
}

// MergeHooksEnv returns the env that activates hooksDir for a single session.
func MergeHooksEnv(hooksDir string) map[string]string {
	return map[string]string{
		"GIT_CONFIG_COUNT":   "1",
		"GIT_CONFIG_KEY_0":   "core.hooksPath",
		"GIT_CONFIG_VALUE_0": hooksDir,
	}
}
