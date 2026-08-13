package jaspar

import (
	"fmt"
	"github.com/FuqingZh/biofetch/internal/shared/bulkasset"
	"regexp"

	"github.com/spf13/cobra"
)

var patternRelease = regexp.MustCompile(`/download/data/([0-9]{4})/`)

func NewCommand() *cobra.Command {
	command := bulkasset.NewCommand(sourceSpec())
	// Keep the removed name explicitly rejected so Cobra cannot treat it as a
	// parent command that merely prints help. JASPAR's upstream product name is
	// "data"; there is intentionally no compatibility alias for "profiles".
	command.AddCommand(&cobra.Command{
		Use:    "profiles",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return fmt.Errorf("unknown command %q for jaspar; use %q", "profiles", "data")
		},
	})
	return command
}

func sourceSpec() bulkasset.Spec {
	return bulkasset.Spec{
		Database:             "jaspar",
		Asset:                "data",
		Source:               "jaspar-download",
		DatabaseDescription:  "Manage JASPAR raw assets and manifest.lock",
		AssetDescription:     "Manage JASPAR motif profiles and metadata",
		VersionDescription:   "JASPAR release year; omit for current",
		SupportsFixedVersion: true,
		ResolveCurrent:       bulkasset.ResolveVersionFromPage("https://jaspar.elixir.no/downloads/", patternRelease),
		Assets: []bulkasset.AssetSpec{
			{
				Name:    "core-pfm",
				Path:    "JASPAR{version}_CORE_non-redundant_pfms_jaspar.zip",
				URL:     "https://jaspar.elixir.no/download/data/{version}/CORE/JASPAR{version}_CORE_non-redundant_pfms_jaspar.zip",
				Default: true,
			},
			{
				Name:    "core-meme",
				Path:    "JASPAR{version}_CORE_non-redundant_pfms_meme.txt",
				URL:     "https://jaspar.elixir.no/download/data/{version}/CORE/JASPAR{version}_CORE_non-redundant_pfms_meme.txt",
				Default: true,
			},
			{
				Name:    "core-metadata",
				Path:    "ultimate_metadata_table_CORE.tsv",
				URL:     "https://mencius.uio.no/JASPAR/JASPAR_metadata/{version}/ultimate_metadata_table_CORE.tsv",
				Default: true,
			},
			{Name: "unvalidated-metadata", Path: "ultimate_metadata_table_UNVALIDATED.tsv", URL: "https://mencius.uio.no/JASPAR/JASPAR_metadata/{version}/ultimate_metadata_table_UNVALIDATED.tsv"},
			{Name: "dl-metadata", Path: "ultimate_metadata_table_DL.tsv", URL: "https://mencius.uio.no/JASPAR/JASPAR_metadata/{version}/ultimate_metadata_table_DL.tsv"},
			{Name: "sites", Path: "sites.tar.gz", URL: "https://jaspar.elixir.no/download/data/{version}/sites.tar.gz"},
			{Name: "bed", Path: "bed.tar.gz", URL: "https://jaspar.elixir.no/download/data/{version}/bed.tar.gz"},
		},
	}
}
