// Package service provides the CLI API types for service status output.
package service

// ServiceStatus is the CLI API representation of a managed service's state.
type ServiceStatus struct {
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	Active    bool   `json:"active"`
	Enabled   bool   `json:"enabled"`
	Manager   string `json:"manager"`
	UnitPath  string `json:"unit_path,omitempty"`
}

// UninstallResult is the CLI API representation of a service uninstall outcome.
type UninstallResult struct {
	Name        string `json:"name"`
	Uninstalled bool   `json:"uninstalled"`
}
