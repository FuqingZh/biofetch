package biofetch

import (
	"biofetch/internal/geneontology"
	"biofetch/internal/kegg"
	"biofetch/internal/stringdb"

	"github.com/spf13/cobra"
)

func RunCLI(args []string) error {
	commandRoot := &cobra.Command{
		Use:           "biofetch",
		Short:         "Fetch bioinformatics raw assets and write manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	commandRoot.AddCommand(geneontology.NewCommand())
	commandRoot.AddCommand(stringdb.NewCommand())
	commandRoot.AddCommand(kegg.NewCommand())
	commandRoot.SetArgs(args)
	return commandRoot.Execute()
}
