package hmdb

import (
	"biofetch/internal/shared/bulkasset"
	"net/http"

	"github.com/spf13/cobra"
)

const baseURL = "https://hmdb.ca/system/downloads/current"

func NewCommand() *cobra.Command {
	return bulkasset.NewCommand(bulkasset.Spec{
		Database:            "hmdb",
		Asset:               "database",
		Source:              "hmdb-download",
		DatabaseDescription: "Manage HMDB raw assets and manifest.lock",
		AssetDescription:    "Manage HMDB metabolite, protein, and spectra exports",
		VersionDescription:  "HMDB release version; omit for current",
		ResolveCurrent: func(*http.Client) (string, error) {
			return "5.0", nil
		},
		Assets: []bulkasset.AssetSpec{
			{Name: "metabolites", Path: "hmdb_metabolites.zip", URL: baseURL + "/hmdb_metabolites.zip", Default: true},
			{Name: "proteins", Path: "hmdb_proteins.zip", URL: baseURL + "/hmdb_proteins.zip", Default: true},
			{Name: "structures", Path: "structures.zip", URL: baseURL + "/structures.zip", Default: true},
			{Name: "protein-fasta", Path: "fasta_proteins.zip", URL: baseURL + "/fasta_proteins.zip"},
			{Name: "gene-fasta", Path: "fasta_genes.zip", URL: baseURL + "/fasta_genes.zip"},
			{Name: "all-spectra-xml", Path: "all_spectra.zip", URL: baseURL + "/all_spectra.zip", Large: true},
		},
	})
}
