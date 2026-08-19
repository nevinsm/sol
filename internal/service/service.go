// Package service provides system service management for sol sphere daemons.
// On Linux it manages systemd user units; on macOS it manages launchd agents.
package service

import (
	"errors"
	"fmt"
	"strings"
	"text/template"
)

// Components lists the sphere daemons managed as system services.
var Components = []string{"prefect", "consul", "chronicle", "ledger", "broker"}

// ErrServiceDegraded indicates that one or more sol service daemons are not
// in a running state (stopped, failed, or unknown to the service manager).
// The CLI layer translates this into exit code 2 so monitoring scripts can
// distinguish "degraded" from "command crashed" (exit 1).
var ErrServiceDegraded = errors.New("one or more sol sphere daemons are not running")

// ErrEmptyPATH indicates the installing user's PATH environment variable was
// empty (or unset) when generating a service unit. sol daemons exec runtime
// binaries (e.g. "claude") and sol's own subcommands by bare name in some
// code paths; a unit with no PATH cannot resolve them. Refusing to generate
// a unit here avoids silently reproducing that failure mode (see the broker
// "claude unreachable" bug this guards against).
var ErrEmptyPATH = errors.New("installing user PATH is empty; refusing to generate a service unit that cannot resolve child binaries by name")

// validatePATH returns ErrEmptyPATH if path is empty or whitespace-only.
// Both GenerateUnit (systemd) and GeneratePlist (launchd) call this so an
// empty installing-PATH is caught uniformly on every platform.
func validatePATH(path string) error {
	if strings.TrimSpace(path) == "" {
		return ErrEmptyPATH
	}
	return nil
}

// xmlEscapeText escapes the minimal set of characters that are unsafe inside
// plist XML text content. PATH values are not expected to contain these, but
// escaping is cheap insurance against a malformed generated plist.
func xmlEscapeText(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
	)
	return r.Replace(s)
}

// ServiceLabel returns the launchd service label for a component.
func ServiceLabel(component string) string {
	return fmt.Sprintf("com.sol.%s", component)
}

const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>{{.Label}}</string>
	<key>ProgramArguments</key>
	<array>
		<string>{{.SolBin}}</string>
		<string>{{.Component}}</string>
		<string>run</string>
	</array>
	<key>KeepAlive</key>
	<true/>
	<key>EnvironmentVariables</key>
	<dict>
		<key>SOL_HOME</key>
		<string>{{.SOLHome}}</string>
		<!-- PATH captured from the installing user's shell at
		     "sol service install" time. Re-run "sol service install" after
		     relocating toolchains (e.g. moving where a runtime binary
		     lives) to refresh this snapshot. -->
		<key>PATH</key>
		<string>{{.Path}}</string>
	</dict>
	<key>StandardOutPath</key>
	<string>{{.LogPath}}.out.log</string>
	<key>StandardErrorPath</key>
	<string>{{.LogPath}}.err.log</string>
</dict>
</plist>
`

var plistTmpl = template.Must(template.New("plist").Parse(plistTemplate))

// PlistData holds the template data for generating a launchd plist.
type PlistData struct {
	Label     string
	Component string
	SolBin    string
	SOLHome   string
	LogPath   string
	Path      string
}

// GeneratePlist returns the launchd plist file content for a component.
// path is the installing user's PATH environment variable, captured at
// generation time and embedded so launchd's default (minimal) PATH does not
// prevent the daemon from exec'ing runtime binaries by bare name. Returns
// ErrEmptyPATH if path is empty.
// This function is platform-independent to allow testing on any OS.
func GeneratePlist(component, solBin, solHome, path string) (string, error) {
	if err := validatePATH(path); err != nil {
		return "", fmt.Errorf("failed to render plist template for %s: %w", component, err)
	}
	label := ServiceLabel(component)
	var buf strings.Builder
	err := plistTmpl.Execute(&buf, PlistData{
		Label:     label,
		Component: component,
		SolBin:    solBin,
		SOLHome:   solHome,
		LogPath:   solHome + "/logs/" + component,
		Path:      xmlEscapeText(path),
	})
	if err != nil {
		return "", fmt.Errorf("failed to render plist template for %s: %w", component, err)
	}
	return buf.String(), nil
}
