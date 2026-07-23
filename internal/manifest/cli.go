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
	var resourceRoot string
	var dirOut string
	formatsInput := []string{"toml"}
	var formatsFile string
	command := &cobra.Command{
		Use:           "build RESOURCE-ROOT",
		Short:         "Build a deterministic aggregate manifest from manifest.lock files",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			resourceRoot = args[0]
			formats, err := cliopt.ExpandListTokens(formatsInput, formatsFile, "formats")
			if err != nil {
				return err
			}
			result, err := Build(resourceRoot, dirOut, formats)
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
	flags.StringVarP(&dirOut, "output", "o", "", "Existing directory for fixed-name manifest output files")
	flags.StringSliceVar(&formatsInput, "formats", formatsInput, "Output formats: toml|tsv|json; repeat or comma-separate")
	flags.StringVar(&formatsFile, "formats-file", "", "Read output formats from a file")
	return command
}
