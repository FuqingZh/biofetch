package rhea

import (
	"github.com/FuqingZh/biofetch/internal/shared/bulkasset"
	"regexp"

	"github.com/spf13/cobra"
)

const baseURL = "https://ftp.expasy.org/databases/rhea"

var patternReleaseDate = regexp.MustCompile(`rhea\.release\.date=([0-9]{4}-[0-9]{2}-[0-9]{2})`)

func NewCommand() *cobra.Command {
	return bulkasset.NewCommand(sourceSpec())
}

func sourceSpec() bulkasset.Spec {
	return bulkasset.Spec{
		Database:            "rhea",
		Asset:               "database",
		Source:              "expasy-ftp-https",
		DatabaseDescription: "Manage Rhea raw assets and manifest.lock",
		AssetDescription:    "Manage Rhea reactions, participants, and mappings",
		VersionDescription:  "Rhea release date; omit for current",
		ResolveCurrent:      bulkasset.ResolveVersionFromPage(baseURL+"/rhea-release.properties", patternReleaseDate),
		Assets: []bulkasset.AssetSpec{
			{Name: "release", Path: "rhea-release.properties", URL: baseURL + "/rhea-release.properties", Default: true},
			{Name: "directions", Path: "rhea-directions.tsv", URL: baseURL + "/tsv/rhea-directions.tsv", Default: true},
			{Name: "relationships", Path: "rhea-relationships.tsv", URL: baseURL + "/tsv/rhea-relationships.tsv", Default: true},
			{Name: "obsoletes", Path: "rhea-obsoletes.tsv", URL: baseURL + "/tsv/rhea-obsoletes.tsv", Default: true},
			{Name: "reaction-smiles", Path: "rhea-reaction-smiles.tsv", URL: baseURL + "/tsv/rhea-reaction-smiles.tsv", Default: true},
			{Name: "chebi-names", Path: "chebiId_name.tsv", URL: baseURL + "/tsv/chebiId_name.tsv", Default: true},
			{Name: "chebi-ph73", Path: "chebi_pH7_3_mapping.tsv", URL: baseURL + "/tsv/chebi_pH7_3_mapping.tsv", Default: true},
			{Name: "rhea2ec", Path: "rhea2ec.tsv", URL: baseURL + "/tsv/rhea2ec.tsv", Default: true},
			{Name: "rhea2go", Path: "rhea2go.tsv", URL: baseURL + "/tsv/rhea2go.tsv", Default: true},
			{Name: "rhea2uniprot-sprot", Path: "rhea2uniprot_sprot.tsv", URL: baseURL + "/tsv/rhea2uniprot_sprot.tsv", Default: true},
			{Name: "rhea2uniprot-trembl", Path: "rhea2uniprot_trembl.tsv.gz", URL: baseURL + "/tsv/rhea2uniprot_trembl.tsv.gz", Default: true},
			{Name: "rhea2xrefs", Path: "rhea2xrefs.tsv", URL: baseURL + "/tsv/rhea2xrefs.tsv", Default: true},
			{Name: "participants-sdf", Path: "rhea.sdf.gz", URL: baseURL + "/ctfiles/rhea.sdf.gz", Default: true},
			{Name: "rdf", Path: "rhea.rdf.gz", URL: baseURL + "/rdf/rhea.rdf.gz", Default: true},
			{Name: "biopax", Path: "rhea-biopax.owl.gz", URL: baseURL + "/biopax/rhea-biopax.owl.gz"},
			{Name: "license", Path: "LICENSE.txt", URL: baseURL + "/LICENSE.txt", Default: true},
		},
	}
}
