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

		reg, err := account.LoadRegistry()
		if err != nil {
			return err
		}

		if err := reg.Add(handle, accountAddEmail, accountAddDescription); err != nil {
			return err
		}

		if err := reg.Save(); err != nil {
			return err
		}

		fmt.Printf("Added account %q\n", handle)
		if reg.Default == handle {
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

var accountRemoveCmd = &cobra.Command{
	Use:          "remove <handle>",
	Short:        "Remove a registered account",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		handle := args[0]

		reg, err := account.LoadRegistry()
		if err != nil {
			return err
		}

		if err := reg.Remove(handle); err != nil {
			return err
		}

		if err := reg.Save(); err != nil {
			return err
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
		reg, err := account.LoadRegistry()
		if err != nil {
			return err
		}

		// No argument: show current default.
		if len(args) == 0 {
			if reg.Default == "" {
				fmt.Println("No default account set.")
			} else {
				fmt.Println(reg.Default)
			}
			return nil
		}

		// Set default.
		handle := args[0]
		if err := reg.SetDefault(handle); err != nil {
			return err
		}

		if err := reg.Save(); err != nil {
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
	accountCmd.AddCommand(accountDefaultCmd)
	accountCmd.AddCommand(accountSetTokenCmd)
	accountCmd.AddCommand(accountSetAPIKeyCmd)
}
