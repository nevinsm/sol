package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"text/tabwriter"

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

var accountListCmd = &cobra.Command{
	Use:          "list",
	Short:        "List registered accounts",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		reg, err := account.LoadRegistry()
		if err != nil {
			return err
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

// --- sol account login ---

var accountLoginCmd = &cobra.Command{
	Use:          "login <handle>",
	Short:        "Open a Claude session to complete OAuth login",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		handle := args[0]

		reg, err := account.LoadRegistry()
		if err != nil {
			return err
		}

		acct, exists := reg.Accounts[handle]
		if !exists {
			return fmt.Errorf("account %q not found — run: sol account add %s", handle, handle)
		}

		fmt.Printf("Starting Claude session for account %q...\n", handle)
		fmt.Println("Complete OAuth login with /login, then exit the session.")

		claude := exec.Command("claude")
		claude.Env = append(os.Environ(), "CLAUDE_CONFIG_DIR="+acct.ConfigDir)
		claude.Stdin = os.Stdin
		claude.Stdout = os.Stdout
		claude.Stderr = os.Stderr

		return claude.Run()
	},
}

func init() {
	rootCmd.AddCommand(accountCmd)

	accountCmd.AddCommand(accountAddCmd)
	accountAddCmd.Flags().StringVar(&accountAddEmail, "email", "", "email associated with the account")
	accountAddCmd.Flags().StringVar(&accountAddDescription, "description", "", "account description")

	accountCmd.AddCommand(accountListCmd)
	accountCmd.AddCommand(accountRemoveCmd)
	accountCmd.AddCommand(accountDefaultCmd)
	accountCmd.AddCommand(accountLoginCmd)
}
