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

		fmt.Printf("Added account %q\n", handle)
		if isDefault {
			fmt.Printf("Set as default (first account)\n")
		}
		return nil
	},
}

// --- sol account list ---

var accountListJSON bool

// accountEntry is a JSON-friendly representation of an account for list output.
type accountEntry struct {
	Handle      string `json:"handle"`
	Email       string `json:"email,omitempty"`
	Description string `json:"description,omitempty"`
	Default     bool   `json:"default"`
}

var accountListCmd = &cobra.Command{
	Use:          "list",
	Short:        "List registered accounts",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		reg, err := account.LoadRegistry()
		if err != nil {
			return err
		}

		if accountListJSON {
			entries := make([]accountEntry, 0, len(reg.Accounts))
			for handle, acct := range reg.Accounts {
				entries = append(entries, accountEntry{
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

		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintf(tw, "HANDLE\tEMAIL\tDESCRIPTION\tDEFAULT\n")
		for handle, acct := range reg.Accounts {
			def := ""
			if handle == reg.Default {
				def = "*"
			}
			email := acct.Email
			if email == "" {
				email = "-"
			}
			desc := acct.Description
			if desc == "" {
				desc = "-"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", handle, email, desc, def)
		}
		tw.Flush()
		return nil
	},
}

// --- sol account remove ---

var (
	accountRemoveConfirm bool
	accountRemoveForce   bool
)

var accountRemoveCmd = &cobra.Command{
	Use:   "remove <handle>",
	Short: "Remove a registered account",
	Long: `Remove a registered account and its stored credentials.

Requires --confirm to proceed; without it, prints what would be removed and
exits. Before deleting, sol scans for live bindings to the account:

  - quota state (.runtime/quota.json)
  - any world's default_account (world.toml)
  - any agent's claude-config metadata (.claude-config/<role>s/<agent>/.account)

If any live bindings are found and --force is not set, the command refuses to
delete the account and lists every binding it found. Pass --force to proceed
anyway; a warning is logged for each still-bound binding before the deletion.

Exit codes:
  0  account removed (or dry-run preview when --confirm absent and no bindings)
  1  general failure (account not found, registry I/O error, or dry-run preview)
  2  refused: live bindings exist and --force was not supplied`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		handle := args[0]

		// Dry-run path: read-only, no lock needed.
		if !accountRemoveConfirm {
			reg, err := account.LoadRegistry()
			if err != nil {
				return err
			}

			acct, exists := reg.Accounts[handle]
			if !exists {
				return fmt.Errorf("account %q not found", handle)
			}

			fmt.Printf("This will permanently remove account %q:\n", handle)
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
				fmt.Printf("  - Live bindings (%d) — removal will be refused without --force:\n", len(bindings))
				fmt.Println(account.FormatBindings(bindings))
			}

			fmt.Println()
			fmt.Println("Run with --confirm to proceed.")
			return &exitError{code: 1}
		}

		var bindings []account.Binding
		var removeErr error
		if err := account.LockedRegistryUpdate(func(reg *account.Registry) error {
			bindings, removeErr = reg.Remove(handle, account.RemoveOpts{Force: accountRemoveForce})
			return removeErr
		}); err != nil {
			// Live-binding refusal is exit code 2 (guard); other failures
			// fall through to the default exit code 1.
			if !accountRemoveForce && len(bindings) > 0 {
				fmt.Fprintln(os.Stderr, err)
				return &exitError{code: 2}
			}
			return err
		}

		// On --force, log a warning per binding before reporting success.
		if accountRemoveForce {
			for _, b := range bindings {
				fmt.Fprintf(os.Stderr, "warning: removed account %q with live binding: %s\n", handle, b.String())
			}
		}

		fmt.Printf("Removed account %q\n", handle)
		return nil
	},
}

// --- sol account default ---

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
				fmt.Println("No default account set.")
			} else {
				fmt.Println(reg.Default)
			}
			return nil
		}

		// Set default.
		handle := args[0]
		if err := account.LockedRegistryUpdate(func(reg *account.Registry) error {
			return reg.SetDefault(handle)
		}); err != nil {
			return err
		}

		fmt.Printf("Default account set to %q\n", handle)
		return nil
	},
}

// --- sol account set-token ---

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
			fmt.Println("To get a setup token:")
			fmt.Println("  1. Run 'claude setup-token' in another terminal")
			fmt.Println("  2. Complete the browser authentication")
			fmt.Println("  3. Copy the token printed by Claude")
			fmt.Println()
			fmt.Print("Paste token: ")
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

		fmt.Printf("Token stored for account %q (expires %s)\n", handle, expires.Format("2006-01-02"))
		return nil
	},
}

// --- sol account set-api-key ---

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
			fmt.Print("Paste API key: ")
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

		fmt.Printf("API key stored for account %q\n", handle)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(accountCmd)

	accountCmd.AddCommand(accountAddCmd)
	accountAddCmd.Flags().StringVar(&accountAddEmail, "email", "", "email associated with the account")
	accountAddCmd.Flags().StringVar(&accountAddDescription, "description", "", "account description")

	accountCmd.AddCommand(accountListCmd)
	accountListCmd.Flags().BoolVar(&accountListJSON, "json", false, "output as JSON")

	accountCmd.AddCommand(accountRemoveCmd)
	accountRemoveCmd.Flags().BoolVar(&accountRemoveConfirm, "confirm", false,
		"confirm removal")
	accountRemoveCmd.Flags().BoolVar(&accountRemoveForce, "force", false,
		"proceed even if the account has live bindings (logs a warning per binding)")
	accountCmd.AddCommand(accountDefaultCmd)
	accountCmd.AddCommand(accountSetTokenCmd)
	accountCmd.AddCommand(accountSetAPIKeyCmd)
}
