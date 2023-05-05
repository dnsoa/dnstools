package commands

import (
	"os"

	"github.com/dnsoa/dnstools/pkg/logs"
	"github.com/spf13/cobra"
)

var Root = New()

func New() *cobra.Command {
	var verbose bool
	root := &cobra.Command{
		Use:               "dnstools",
		Short:             "dns tools",
		SilenceUsage:      true, // Don't show usage on errors
		DisableAutoGenTag: true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if verbose {
				logs.Warn.SetOutput(os.Stderr)
				logs.Debug.SetOutput(os.Stderr)
			}
			logs.Progress.SetOutput(os.Stderr)

		},
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
		},
	}
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable debug logs")

	AddCommands(root)
	return root
}
