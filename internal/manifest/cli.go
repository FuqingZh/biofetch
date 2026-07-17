package manifest

import (
	"biofetch/internal/shared/cliopt"
	"fmt"

	"github.com/spf13/cobra"
)

func NewCommand() *cobra.Command {
	commandRoot := &cobra.Command{
		Use:           "manifest",
		Short:         "Build aggregate manifests for biofetch resource snapshots",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	commandRoot.AddCommand(newBuildCommand())
	return commandRoot
}

func newBuildCommand() *cobra.Command {
	var dirIn string
	var dirOut string
	formatsInput := []string{"toml"}
	command := &cobra.Command{
		Use:           "build",
		Short:         "Build a deterministic aggregate manifest from manifest.lock files",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			formats, err := cliopt.ExpandAtFileTokens(formatsInput, "formats")
			if err != nil {
				return err
			}
			result, err := Build(dirIn, dirOut, formats)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(
				command.OutOrStdout(),
				"manifest built: databases=%d snapshots=%d files=%d bytes=%d outputs=%d\n",
				result.DatabaseCount,
				result.SnapshotCount,
				result.FileCount,
				result.TotalBytes,
				len(result.Files),
			)
			return err
		},
	}
	flags := command.Flags()
	flags.SortFlags = false
	flags.StringVar(&dirIn, "dir_in", "", "Biofetch resource tree containing snapshot manifest.lock files")
	flags.StringVar(&dirOut, "dir_out", "", "Existing directory for fixed-name manifest output files")
	flags.StringSliceVar(&formatsInput, "formats", formatsInput, "Output formats: toml|tsv|json; repeat, comma-separate, or use @file")
	return command
}
