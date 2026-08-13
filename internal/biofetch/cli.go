package biofetch

import (
	"github.com/FuqingZh/biofetch/internal/chebi"
	"github.com/FuqingZh/biofetch/internal/chemont"
	"github.com/FuqingZh/biofetch/internal/dbcan"
	"github.com/FuqingZh/biofetch/internal/eggnog"
	"github.com/FuqingZh/biofetch/internal/geneontology"
	"github.com/FuqingZh/biofetch/internal/hmdb"
	"github.com/FuqingZh/biofetch/internal/interpro"
	"github.com/FuqingZh/biofetch/internal/jaspar"
	"github.com/FuqingZh/biofetch/internal/kegg"
	"github.com/FuqingZh/biofetch/internal/lipidmaps"
	"github.com/FuqingZh/biofetch/internal/manifest"
	"github.com/FuqingZh/biofetch/internal/omnipath"
	"github.com/FuqingZh/biofetch/internal/reactome"
	"github.com/FuqingZh/biofetch/internal/rhea"
	"github.com/FuqingZh/biofetch/internal/stringdb"
	"github.com/FuqingZh/biofetch/internal/uniprot"
	"github.com/FuqingZh/biofetch/internal/wikipathways"

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
	commandRoot.AddCommand(dbcan.NewCommand())
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
