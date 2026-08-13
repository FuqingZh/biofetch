package chemont

import (
	"github.com/FuqingZh/biofetch/internal/shared/bulkasset"
	"net/http"

	"github.com/spf13/cobra"
)

func NewCommand() *cobra.Command {
	return bulkasset.NewCommand(sourceSpec())
}

func sourceSpec() bulkasset.Spec {
	return bulkasset.Spec{
		Database:            "chemont",
		Asset:               "ontology",
		Source:              "classyfire",
		DatabaseDescription: "Manage ChemOnt raw assets and manifest.lock",
		AssetDescription:    "Manage the ClassyFire ChemOnt ontology",
		VersionDescription:  "ChemOnt version; omit for current",
		ResolveCurrent: func(*http.Client) (string, error) {
			return "2.1", nil
		},
		Assets: []bulkasset.AssetSpec{{
			Name:    "obo",
			Path:    "ChemOnt_2_1.obo.zip",
			URL:     "http://classyfire.wishartlab.com/system/downloads/1_0/chemont/ChemOnt_2_1.obo.zip",
			Default: true,
		}},
	}
}
