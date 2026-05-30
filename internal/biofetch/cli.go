package biofetch

import (
	"biofetch/internal/geneontology"
	"biofetch/internal/interpro"
	"biofetch/internal/kegg"
	"biofetch/internal/omnipath"
	"biofetch/internal/reactome"
	"biofetch/internal/stringdb"
	"biofetch/internal/subcell"
	"biofetch/internal/uniprot"
	"biofetch/internal/wikipathways"

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
	commandRoot.AddCommand(interpro.NewCommand())
	commandRoot.AddCommand(stringdb.NewCommand())
	commandRoot.AddCommand(kegg.NewCommand())
	commandRoot.AddCommand(omnipath.NewCommand())
	commandRoot.AddCommand(reactome.NewCommand())
	commandRoot.AddCommand(subcell.NewCommand())
	commandRoot.AddCommand(uniprot.NewCommand())
	commandRoot.AddCommand(wikipathways.NewCommand())
	commandRoot.SetArgs(args)
	return commandRoot.Execute()
}
