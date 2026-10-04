// Command taskroll is a work tracker kept in git: JSONL records as the
// source of truth, markdown views generated from them, and a CLI for people
// and agents. Run `taskroll init` at a repository's root to start one.
package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/spf13/cobra"

	"github.com/hvish/taskroll"
	"github.com/hvish/taskroll/cli"
)

// version is set at release: go build -ldflags "-X main.version=v0.1.0".
// A `go install ...@vX.Y.Z` build has no ldflags, so the module version
// recorded in the binary stands in.
var version = "dev"

func buildVersion() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return version
}

func main() {
	root := &cobra.Command{
		Use:           "taskroll",
		Short:         "A work tracker kept in git",
		Long:          "Epics, tasks and entries as JSONL records in the repository, markdown views generated\nfrom them, and every change made through these commands. Start with taskroll init.",
		SilenceUsage:  true,
		SilenceErrors: false,
		Args:          cobra.NoArgs,
	}
	root.AddCommand(cli.Commands(cli.Hooks{Invocation: "taskroll"})...)
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the tracker version and the record schema it reads",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "taskroll %s (record schema %d)\n", buildVersion(), taskroll.SchemaVersion)
		},
	})
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
