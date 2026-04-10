package accounts

// ListEntry is the CLI API representation of an account in list output.
// This preserves the existing shape of `sol account list --json` output.
type ListEntry struct {
	Handle      string `json:"handle"`
	Email       string `json:"email,omitempty"`
	Description string `json:"description,omitempty"`
	Default     bool   `json:"default"`
}
