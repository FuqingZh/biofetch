package lipidmaps

import (
	"biofetch/internal/shared/bulkasset"
	"regexp"

	"github.com/spf13/cobra"
)

var patternLMSDRelease = regexp.MustCompile(`Last updated:\s*([0-9]{4}-[0-9]{2}-[0-9]{2})`)

func NewCommand() *cobra.Command {
	return bulkasset.NewCommand(bulkasset.Spec{
		Database:            "lipidmaps",
		Asset:               "lmsd",
		Source:              "lipidmaps-download",
		DatabaseDescription: "Manage LIPID MAPS raw assets and manifest.lock",
		AssetDescription:    "Manage LIPID MAPS Structure Database exports",
		VersionDescription:  "LMSD release date; omit for current",
		ResolveCurrent:      bulkasset.ResolveVersionFromPage("https://www.lipidmaps.org/databases/lmsd/download", patternLMSDRelease),
		Assets: []bulkasset.AssetSpec{
			{Name: "structures", Path: "LMSD.sdf.zip", URL: "https://www.lipidmaps.org/files/?ext=sdf.zip&file=LMSD", Default: true},
			{Name: "rdf", Path: "LMSD.ttl", URL: "https://www.lipidmaps.org/files/?ext=ttl&file=sparql_lipids", Default: true},
			{Name: "identifiers", Path: "lipidmaps_ids_cc0.tsv", URL: "https://www.lipidmaps.org/files/?ext=tsv&file=lipidmaps_ids_cc0", Default: true},
		},
	})
}
