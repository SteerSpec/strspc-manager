// Package main is the entry point for the strspc-manager CLI.
package main

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/internal/version"
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
		Long: fmt.Sprintf(`
%s

  The SteerSpec Rule Manager validates, enforces, and evaluates
  rules across your codebase.

  %s  rule-lint, rule-diff, rule-eval, rule-resolve

  %s  https://steerspec.dev`,
			brand.Render("SteerSpec Rule Manager"),
			accent.Render("Modules:"),
			subtle.Render("Docs:"),
		),
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(newVersionCmd())

	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Printf("%s %s\n",
				brand.Render("strspc-manager"),
				accent.Render(version.Version),
			)
			fmt.Printf("  %s %s\n", subtle.Render("commit:"), version.Commit)
			fmt.Printf("  %s %s\n", subtle.Render("built:"), version.Date)
		},
	}
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		log.Error(err)
		os.Exit(1)
	}
}
