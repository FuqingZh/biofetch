package chebi

import (
	"biofetch/internal/shared/bulkasset"

	"github.com/spf13/cobra"
)

const baseURL = "https://ftp.ebi.ac.uk/pub/databases/chebi"

func NewCommand() *cobra.Command {
	return bulkasset.NewCommand(bulkasset.Spec{
		Database:            "chebi",
		Asset:               "database",
		Source:              "ebi-ftp-https",
		DatabaseDescription: "Manage ChEBI raw assets and manifest.lock",
		AssetDescription:    "Manage ChEBI ontology, tables, and structures",
		VersionDescription:  "ChEBI release date; omit for the current monthly release",
		ResolveCurrent:      bulkasset.ResolveVersionFromLastModified(baseURL + "/SDF/chebi.sdf.gz"),
		Assets: []bulkasset.AssetSpec{
			{Name: "ontology", Path: "chebi.obo.gz", URL: baseURL + "/ontology/chebi.obo.gz", Default: true},
			{Name: "structures", Path: "chebi.sdf.gz", URL: baseURL + "/SDF/chebi.sdf.gz", Default: true},
			{Name: "compounds", Path: "compounds.tsv.gz", URL: baseURL + "/flat_files/compounds.tsv.gz", Default: true},
			{Name: "names", Path: "names.tsv.gz", URL: baseURL + "/flat_files/names.tsv.gz", Default: true},
			{Name: "database-accessions", Path: "database_accession.tsv.gz", URL: baseURL + "/flat_files/database_accession.tsv.gz", Default: true},
			{Name: "chemical-data", Path: "chemical_data.tsv.gz", URL: baseURL + "/flat_files/chemical_data.tsv.gz", Default: true},
			{Name: "relations", Path: "relation.tsv.gz", URL: baseURL + "/flat_files/relation.tsv.gz", Default: true},
			{Name: "secondary-ids", Path: "secondary_ids.tsv.gz", URL: baseURL + "/flat_files/secondary_ids.tsv.gz", Default: true},
			{Name: "structures-table", Path: "structures.tsv.gz", URL: baseURL + "/flat_files/structures.tsv.gz", Default: true},
			{Name: "license", Path: "LICENSE", URL: baseURL + "/ontology/LICENSE", Default: true},
			{Name: "postgres-dump", Path: "pgsql_allstars.dump", URL: baseURL + "/generic_dumps/pgsql_allstars.dump", Large: true},
		},
	})
}
