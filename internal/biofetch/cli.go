package biofetch

import (
	"biofetch/internal/chebi"
	"biofetch/internal/chemont"
	"biofetch/internal/eggnog"
	"biofetch/internal/geneontology"
	"biofetch/internal/hmdb"
	"biofetch/internal/interpro"
	"biofetch/internal/jaspar"
	"biofetch/internal/kegg"
	"biofetch/internal/lipidmaps"
	"biofetch/internal/manifest"
	"biofetch/internal/omnipath"
	"biofetch/internal/reactome"
	"biofetch/internal/rhea"
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
	commandRoot.AddCommand(chebi.NewCommand())
	commandRoot.AddCommand(chemont.NewCommand())
	commandRoot.AddCommand(geneontology.NewCommand())
	commandRoot.AddCommand(hmdb.NewCommand())
	commandRoot.AddCommand(interpro.NewCommand())
	commandRoot.AddCommand(jaspar.NewCommand())
	commandRoot.AddCommand(lipidmaps.NewCommand())
	commandRoot.AddCommand(stringdb.NewCommand())
	commandRoot.AddCommand(kegg.NewCommand())
	commandRoot.AddCommand(manifest.NewCommand())
	commandRoot.AddCommand(omnipath.NewCommand())
	commandRoot.AddCommand(reactome.NewCommand())
	commandRoot.AddCommand(rhea.NewCommand())
	commandRoot.AddCommand(uniprot.NewCommand())
	commandRoot.AddCommand(wikipathways.NewCommand())
	commandRoot.SetArgs(args)
	return commandRoot.Execute()
}
