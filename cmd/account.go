package cmd

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/nevinsm/sol/internal/account"
	"github.com/nevinsm/sol/internal/broker"
	"github.com/nevinsm/sol/internal/cliapi/accounts"
	"github.com/nevinsm/sol/internal/cliformat"
	"github.com/nevinsm/sol/internal/quota"
	"github.com/spf13/cobra"
)

var accountCmd = &cobra.Command{
	Use:     "account",
	Short:   "Manage Claude OAuth accounts",
	GroupID: groupSetup,
}

// --- sol account add ---

var (
	accountAddEmail       string
	accountAddDescription string
	accountAddJSON        bool
)

var accountAddCmd = &cobra.Command{
	Use:          "add <handle>",
	Short:        "Register a new account",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		handle := args[0]

		var isDefault bool
		if err := account.LockedRegistryUpdate(func(reg *account.Registry) error {
			if err := reg.Add(handle, accountAddEmail, accountAddDescription); err != nil {
				return err
			}
			isDefault = reg.Default == handle
			return nil
		}); err != nil {
			return err
		}

		if accountAddJSON {
			return printJSON(accounts.Account{
				Handle:  handle,
				Default: isDefault,
			})
		}

		fmt.Printf("Added account %q\n", handle)
		if isDefault {
			fmt.Printf("Set as default (first account)\n")
		}
		return nil
	},
}

// --- sol account list ---

var accountListJSON bool

var accountListCmd = &cobra.Command{
	Use:          "list",
	Short:        "List registered accounts",
	SilenceUsage: true,
	RunE:         runAccountList,
}

func runAccountList(cmd *cobra.Command, args []string) error {
	reg, err := account.LoadRegistry()
	if err != nil {
		return err
	}

	if accountListJSON {
		entries := make([]accounts.ListEntry, 0, len(reg.Accounts))
		for handle, acct := range reg.Accounts {
			entries = append(entries, accounts.ListEntry{
				Handle:      handle,
				Email:       acct.Email,
				Description: acct.Description,
				Default:     handle == reg.Default,
			})
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Handle < entries[j].Handle
		})
		return printJSON(entries)
	}

	if len(reg.Accounts) == 0 {
		fmt.Println("No accounts registered.")
		return nil
	}

	// Resolve broker/quota state once so we don't re-read per row.
	brokerOffline := isBrokerOffline()
	limitedSet := limitedAccountsSet()

	// Sort handles for deterministic output.
	handles := make([]string, 0, len(reg.Accounts))
	for h := range reg.Accounts {
		handles = append(handles, h)
	}
	sort.Strings(handles)

	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "HANDLE\tTYPE\tSTATUS\tDEFAULT\n")
	now := time.Now()
	for _, handle := range handles {
		typeStr, statusStr := accountTypeAndStatus(handle, now, brokerOffline, limitedSet)
		def := cliformat.EmptyMarker
		if handle == reg.Default {
			def = "*"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", handle, typeStr, statusStr, def)
	}
	tw.Flush()
	fmt.Printf("\n%s\n", cliformat.FormatCount(len(reg.Accounts), "account", "accounts"))
	return nil
}

// isBrokerOffline reports whether the broker heartbeat is missing or stale,
// in which case per-account STATUS cannot be trusted and renders as unknown.
func isBrokerOffline() bool {
	info, err := broker.ReadProviderHealth()
	if err != nil || info == nil {
		return true
	}
	return info.Stale
}

// limitedAccountsSet returns the set of account handles currently in the
// "limited" quota state. Best-effort: returns an empty set on any error.
func limitedAccountsSet() map[string]bool {
	set := make(map[string]bool)
	state, err := quota.Load()
	if err != nil || state == nil {
		return set
	}
	for _, handle := range state.LimitedAccounts() {
		set[handle] = true
	}
	return set
}

// accountTypeAndStatus computes the TYPE and STATUS columns for a single
// account row. Both default to cliformat.EmptyMarker when they cannot be
// determined (no token, broker offline, etc).
func accountTypeAndStatus(handle string, now time.Time, brokerOffline bool, limitedSet map[string]bool) (typeStr, statusStr string) {
	tok, err := account.ReadToken(handle)
	if err != nil || tok == nil {
		// No token on disk: we can't tell the type, and there is nothing
		// for the broker to report on. Render both as unknown.
		return cliformat.EmptyMarker, cliformat.EmptyMarker
	}

	switch tok.Type {
	case "oauth_token":
		typeStr = "oauth"
	case "api_key":
		typeStr = "api_key"
	default:
		typeStr = cliformat.EmptyMarker
	}

	// STATUS: unknown if we can't consult the broker.
	if brokerOffline {
		return typeStr, cliformat.EmptyMarker
	}

	switch {
	case limitedSet[handle]:
		statusStr = "limited"
	case tok.ExpiresAt != nil && now.After(*tok.ExpiresAt):
		statusStr = "expired"
	default:
		statusStr = "ok"
	}
	return typeStr, statusStr
}

// --- sol account delete (formerly: sol account remove) ---

var (
	accountDeleteConfirm bool
	accountDeleteForce   bool
	accountDeleteJSON    bool
)

const accountDeleteLong = `Delete a registered account and its stored credentials.

Requires --confirm to proceed; without it, prints what would be removed and
exits. Before deleting, sol scans for live bindings to the account:

  - quota state (.runtime/quota.json)
  - any world's default_account (world.toml)
  - any agent's claude-config metadata (.claude-config/<role>s/<agent>/.account)

If any live bindings are found and --force is not set, the command refuses to
delete the account and lists every binding it found. Pass --force to proceed
anyway; a warning is logged for each still-bound binding before the deletion.

Exit codes:
  0  account deleted (or dry-run preview when --confirm absent and no bindings)
  1  general failure (account not found, registry I/O error, or dry-run preview)
  2  refused: live bindings exist and --force was not supplied`

var accountDeleteCmd = &cobra.Command{
	Use:          "delete <handle>",
	Short:        "Delete a registered account",
	Long:         accountDeleteLong,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAccountDelete(args[0], accountDeleteConfirm, accountDeleteForce, accountDeleteJSON)
	},
}

// accountRemoveCmd is a hidden backwards-compatibility alias for
// `sol account delete`. It will be removed in a future release; for now it
// prints a deprecation notice to stderr and delegates to the same logic.
var accountRemoveCmd = &cobra.Command{
	Use:          "remove <handle>",
	Short:        "Deprecated: use 'sol account delete'",
	Long:         "Deprecated: this alias will be removed in a future release.\n\n" + accountDeleteLong,
	Args:         cobra.ExactArgs(1),
	Hidden:       true,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintln(os.Stderr, "warning: 'sol account remove' is deprecated; use 'sol account delete' instead")
		return runAccountDelete(args[0], accountDeleteConfirm, accountDeleteForce, accountDeleteJSON)
	},
}

// runAccountDelete performs the shared deletion flow used by both
// `sol account delete` and the hidden `sol account remove` alias.
func runAccountDelete(handle string, confirm, force, jsonMode bool) error {
	// Dry-run path: read-only, no lock needed.
	if !confirm {
		reg, err := account.LoadRegistry()
		if err != nil {
			return err
		}

		acct, exists := reg.Accounts[handle]
		if !exists {
			return fmt.Errorf("account %q not found", handle)
		}

		if !jsonMode {
			fmt.Printf("This will permanently delete account %q:\n", handle)
			if acct.Email != "" {
				fmt.Printf("  - Email: %s\n", acct.Email)
			}
			if acct.Description != "" {
				fmt.Printf("  - Description: %s\n", acct.Description)
			}
			if reg.Default == handle {
				fmt.Printf("  - This is the current default account\n")
			}

			// Surface any live bindings in the preview so the operator
			// learns about them before --confirm.
			bindings, bErr := account.FindBindings(handle)
			if bErr != nil {
				fmt.Printf("  - Warning: failed to scan for live bindings: %v\n", bErr)
			} else if len(bindings) > 0 {
				fmt.Printf("  - Live bindings (%d) — deletion will be refused without --force:\n", len(bindings))
				fmt.Println(account.FormatBindings(bindings))
			}

			fmt.Println()
			fmt.Println("Run with --confirm to proceed.")
		}
		return &exitError{code: 1}
	}

	var bindings []account.Binding
	var removeErr error
	if err := account.LockedRegistryUpdate(func(reg *account.Registry) error {
		bindings, removeErr = reg.Remove(handle, account.RemoveOpts{Force: force})
		return removeErr
	}); err != nil {
		// Live-binding refusal is exit code 2 (guard); other failures
		// fall through to the default exit code 1.
		if !force && len(bindings) > 0 {
			fmt.Fprintln(os.Stderr, err)
			return &exitError{code: 2}
		}
		return err
	}

	// On --force, log a warning per binding before reporting success.
	if force {
		for _, b := range bindings {
			fmt.Fprintf(os.Stderr, "warning: deleted account %q with live binding: %s\n", handle, b.String())
		}
	}

	if jsonMode {
		return printJSON(accounts.DeleteResponse{
			Handle:  handle,
			Deleted: true,
		})
	}

	fmt.Printf("Removed account %q\n", handle)
	return nil
}

// --- sol account default ---

var accountDefaultJSON bool

var accountDefaultCmd = &cobra.Command{
	Use:          "default [<handle>]",
	Short:        "Show or set the default account",
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// No argument: show current default (read-only, no lock needed).
		if len(args) == 0 {
			reg, err := account.LoadRegistry()
			if err != nil {
				return err
			}
			if reg.Default == "" {
				if accountDefaultJSON {
					return printJSON(nil)
				}
				fmt.Println("No default account set.")
			} else {
				if accountDefaultJSON {
					now := time.Now()
					brokerOffline := isBrokerOffline()
					limitedSet := limitedAccountsSet()
					typeStr, statusStr := accountTypeAndStatus(reg.Default, now, brokerOffline, limitedSet)
					return printJSON(accounts.FromStoreAccount(reg.Default, reg.Accounts[reg.Default], typeStr, statusStr, true))
				}
				fmt.Println(reg.Default)
			}
			return nil
		}

		// Set default.
		handle := args[0]
		var acct account.Account
		if err := account.LockedRegistryUpdate(func(reg *account.Registry) error {
			if err := reg.SetDefault(handle); err != nil {
				return err
			}
			acct = reg.Accounts[handle]
			return nil
		}); err != nil {
			return err
		}

		if accountDefaultJSON {
			now := time.Now()
			brokerOffline := isBrokerOffline()
			limitedSet := limitedAccountsSet()
			typeStr, statusStr := accountTypeAndStatus(handle, now, brokerOffline, limitedSet)
			return printJSON(accounts.FromStoreAccount(handle, acct, typeStr, statusStr, true))
		}

		fmt.Printf("Default account set to %q\n", handle)
		return nil
	},
}

// --- sol account set-token ---

var accountSetTokenJSON bool

var accountSetTokenCmd = &cobra.Command{
	Use:          "set-token <handle> [token]",
	Short:        "Store an OAuth token for an account",
	Args:         cobra.RangeArgs(1, 2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		handle := args[0]

		reg, err := account.LoadRegistry()
		if err != nil {
			return err
		}
		if _, exists := reg.Accounts[handle]; !exists {
			return fmt.Errorf("account %q not found — run: sol account add %s", handle, handle)
		}

		var tokenValue string
		if len(args) == 2 {
			tokenValue = args[1]
		} else {
			if !accountSetTokenJSON {
				fmt.Println("To get a setup token:")
				fmt.Println("  1. Run 'claude setup-token' in another terminal")
				fmt.Println("  2. Complete the browser authentication")
				fmt.Println("  3. Copy the token printed by Claude")
				fmt.Println()
				fmt.Print("Paste token: ")
			}
			scanner := bufio.NewScanner(os.Stdin)
			if scanner.Scan() {
				tokenValue = strings.TrimSpace(scanner.Text())
			}
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("failed to read token: %w", err)
			}
		}

		if tokenValue == "" {
			return fmt.Errorf("token must not be empty")
		}

		now := time.Now().UTC()
		expires := now.Add(365 * 24 * time.Hour)
		tok := &account.Token{
			Type:      "oauth_token",
			Token:     tokenValue,
			CreatedAt: now,
			ExpiresAt: &expires,
		}

		if err := account.WriteToken(handle, tok); err != nil {
			return err
		}

		if accountSetTokenJSON {
			return printJSON(accounts.Account{
				Handle:  handle,
				Type:    "oauth",
				Default: reg.Default == handle,
			})
		}

		fmt.Printf("Token stored for account %q (expires %s)\n", handle, expires.Format("2006-01-02"))
		return nil
	},
}

// --- sol account set-api-key ---

var accountSetAPIKeyJSON bool

var accountSetAPIKeyCmd = &cobra.Command{
	Use:          "set-api-key <handle> [key]",
	Short:        "Store an API key for an account",
	Args:         cobra.RangeArgs(1, 2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		handle := args[0]

		reg, err := account.LoadRegistry()
		if err != nil {
			return err
		}
		if _, exists := reg.Accounts[handle]; !exists {
			return fmt.Errorf("account %q not found — run: sol account add %s", handle, handle)
		}

		var keyValue string
		if len(args) == 2 {
			keyValue = args[1]
		} else {
			if !accountSetAPIKeyJSON {
				fmt.Print("Paste API key: ")
			}
			scanner := bufio.NewScanner(os.Stdin)
			if scanner.Scan() {
				keyValue = strings.TrimSpace(scanner.Text())
			}
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("failed to read API key: %w", err)
			}
		}

		if keyValue == "" {
			return fmt.Errorf("API key must not be empty")
		}

		now := time.Now().UTC()
		tok := &account.Token{
			Type:      "api_key",
			Token:     keyValue,
			CreatedAt: now,
		}

		if err := account.WriteToken(handle, tok); err != nil {
			return err
		}

		if accountSetAPIKeyJSON {
			return printJSON(accounts.Account{
				Handle:  handle,
				Type:    "api_key",
				Default: reg.Default == handle,
			})
		}

		fmt.Printf("API key stored for account %q\n", handle)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(accountCmd)

	accountCmd.AddCommand(accountAddCmd)
	accountAddCmd.Flags().StringVar(&accountAddEmail, "email", "", "email associated with the account")
	accountAddCmd.Flags().StringVar(&accountAddDescription, "description", "", "account description")
	accountAddCmd.Flags().BoolVar(&accountAddJSON, "json", false, "output as JSON")

	accountCmd.AddCommand(accountListCmd)
	accountListCmd.Flags().BoolVar(&accountListJSON, "json", false, "output as JSON")

	accountCmd.AddCommand(accountDeleteCmd)
	accountDeleteCmd.Flags().BoolVar(&accountDeleteConfirm, "confirm", false,
		"confirm deletion")
	accountDeleteCmd.Flags().BoolVar(&accountDeleteForce, "force", false,
		"proceed even if the account has live bindings (logs a warning per binding)")
	accountDeleteCmd.Flags().BoolVar(&accountDeleteJSON, "json", false, "output as JSON")

	// Hidden backwards-compatibility alias for 'sol account delete'. Reuses
	// the same --confirm/--force flag variables so both verbs behave
	// identically at the CLI surface.
	accountCmd.AddCommand(accountRemoveCmd)
	accountRemoveCmd.Flags().BoolVar(&accountDeleteConfirm, "confirm", false,
		"confirm deletion")
	accountRemoveCmd.Flags().BoolVar(&accountDeleteForce, "force", false,
		"proceed even if the account has live bindings (logs a warning per binding)")
	accountRemoveCmd.Flags().BoolVar(&accountDeleteJSON, "json", false, "output as JSON")
	accountCmd.AddCommand(accountDefaultCmd)
	accountDefaultCmd.Flags().BoolVar(&accountDefaultJSON, "json", false, "output as JSON")
	accountCmd.AddCommand(accountSetTokenCmd)
	accountSetTokenCmd.Flags().BoolVar(&accountSetTokenJSON, "json", false, "output as JSON")
	accountCmd.AddCommand(accountSetAPIKeyCmd)
	accountSetAPIKeyCmd.Flags().BoolVar(&accountSetAPIKeyJSON, "json", false, "output as JSON")
}
