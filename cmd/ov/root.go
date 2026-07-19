package main

import "github.com/spf13/cobra"

var version = "0.1.0"

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "ov",
		Short:         "Obsidian vault management",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newConfigCmd(), newDoctorCmd(), newInitCmd(), newInboxCmd(), newStaleCmd(), newReviewCmd(), newMocsCmd(), newCaptureCmd(), newTriageCmd(), newServeCmd(), newRenderCmd(), newPublishCmd(), newUnpublishCmd(), newNewCmd())
	return root
}
