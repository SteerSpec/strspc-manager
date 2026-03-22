// Package main is the entry point for the strspc-manager CLI.
package main

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/internal/version"
	"github.com/SteerSpec/strspc-manager/src/realmlint"
	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/rulelint"
)

var (
	brand = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7C3AED"))

	subtle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280"))

	accent = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#10B981"))
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "strspc-manager",
		Short: brand.Render("SteerSpec Rule Manager") + subtle.Render(" — core enforcement engine"),
		Long: fmt.Sprintf(
			"%s\n\n"+
				"\tThe SteerSpec Rule Manager validates, enforces, and evaluates\n"+
				"\trules across your codebase.\n\n"+
				"\t%s  rule-lint, rule-diff, rule-eval, rule-resolve\n\n"+
				"\t%s  https://steerspec.dev",
			brand.Render("SteerSpec Rule Manager"),
			accent.Render("Modules:"),
			subtle.Render("Docs:"),
		),
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(newVersionCmd())
	root.AddCommand(newRealmLintCmd())

	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Printf("%s %s\n",
				brand.Render("strspc-manager"),
				accent.Render(version.Version),
			)
			cmd.Printf("  %s %s\n", subtle.Render("commit:"), version.Commit)
			cmd.Printf("  %s %s\n", subtle.Render("built:"), version.Date)
		},
	}
}

var (
	errStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444"))
	warnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))
	infoStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
)

func newRealmLintCmd() *cobra.Command {
	var strict bool
	cmd := &cobra.Command{
		Use:   "realm-lint <dir>",
		Short: "Validate a Realm directory",
		Long:  "Validates realm.json, directory structure, entity files, and EUID uniqueness.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := args[0]
			rl := rulelint.New(rulelint.WithStrict(strict))
			linter := realmlint.New(
				realmlint.WithStrict(strict),
				realmlint.WithRuleLinter(rl),
			)
			res := linter.Lint(dir)
			printDiagnostics(cmd, res)
			if !res.OK() {
				return fmt.Errorf("realm validation failed with errors")
			}
			cmd.Println(accent.Render("✓ Realm is valid"))
			return nil
		},
	}
	cmd.Flags().BoolVar(&strict, "strict", false, "treat warnings as errors")
	return cmd
}

func printDiagnostics(cmd *cobra.Command, res *result.Result) {
	for _, d := range res.Diagnostics {
		var style lipgloss.Style
		switch d.Severity {
		case result.Error:
			style = errStyle
		case result.Warning:
			style = warnStyle
		default:
			style = infoStyle
		}
		prefix := style.Render(fmt.Sprintf("[%s]", d.Severity))
		code := subtle.Render(d.Code)
		msg := d.Message
		if d.Path != "" {
			cmd.Printf("%s %s %s (%s)\n", prefix, code, msg, d.Path)
		} else {
			cmd.Printf("%s %s %s\n", prefix, code, msg)
		}
	}
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		log.Error(err)
		os.Exit(1)
	}
}
