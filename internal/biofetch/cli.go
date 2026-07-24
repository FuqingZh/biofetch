package biofetch

import (
	"biofetch/internal/eggnog"
	"biofetch/internal/geneontology"
	"biofetch/internal/interpro"
	"biofetch/internal/kegg"
	"biofetch/internal/manifest"
	"biofetch/internal/omnipath"
	"biofetch/internal/reactome"
	"biofetch/internal/stringdb"
	"biofetch/internal/uniprot"
	"biofetch/internal/wikipathways"

	"github.com/spf13/cobra"
)

var Version = "dev"

func RunCLI(args []string) error {
	commandRoot := &cobra.Command{
		Use:           "biofetch",
		Short:         "Fetch bioinformatics raw assets and write manifest.lock",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	commandRoot.AddCommand(eggnog.NewCommand())
	commandRoot.AddCommand(geneontology.NewCommand())
	commandRoot.AddCommand(interpro.NewCommand())
	commandRoot.AddCommand(stringdb.NewCommand())
	commandRoot.AddCommand(kegg.NewCommand())
	commandRoot.AddCommand(manifest.NewCommand())
	commandRoot.AddCommand(omnipath.NewCommand())
	commandRoot.AddCommand(reactome.NewCommand())
	commandRoot.AddCommand(uniprot.NewCommand())
	commandRoot.AddCommand(wikipathways.NewCommand())
	commandRoot.SetArgs(args)
	return commandRoot.Execute()
}
