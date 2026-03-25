// Package service provides system service management for sol sphere daemons.
// On Linux it manages systemd user units; on macOS it manages launchd agents.
package service

import (
	"fmt"
	"strings"
	"text/template"
)

// Components lists the sphere daemons managed as system services.
var Components = []string{"prefect", "consul", "chronicle", "ledger", "broker"}

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
}

// GeneratePlist returns the launchd plist file content for a component.
// This function is platform-independent to allow testing on any OS.
func GeneratePlist(component, solBin, solHome string) (string, error) {
	label := ServiceLabel(component)
	var buf strings.Builder
	err := plistTmpl.Execute(&buf, PlistData{
		Label:     label,
		Component: component,
		SolBin:    solBin,
		SOLHome:   solHome,
		LogPath:   solHome + "/logs/" + component,
	})
	if err != nil {
		return "", fmt.Errorf("failed to render plist template for %s: %w", component, err)
	}
	return buf.String(), nil
}
