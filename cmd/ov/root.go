package main

import "github.com/spf13/cobra"

var version = "0.0.1-phase0"

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "ov2",
		Short:         "Obsidian vault management (v2)",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newConfigCmd(), newDoctorCmd(), newInitCmd(), newInboxCmd(), newStaleCmd(), newReviewCmd(), newMocsCmd())
	return root
}
