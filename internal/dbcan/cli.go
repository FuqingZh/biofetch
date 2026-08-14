package dbcan

import (
	"github.com/FuqingZh/biofetch/internal/shared/bulkasset"

	"github.com/spf13/cobra"
)

const (
	VersionToken  = "db_v5-2-9_5-5-2026"
	SourceVersion = "5.2.9"
	SourceName    = "run-dbcan-s3"
	baseURL       = "https://dbcan.s3.us-west-2.amazonaws.com/" + VersionToken

	bytesCAZyDiamond            int64 = 2_177_198_564
	bytesDBCANHMM               int64 = 129_842_960
	bytesDBCANSubHMM            int64 = 5_132_429_983
	bytesFamilySubstrateMapping int64 = 94_399
	TotalBytes                  int64 = bytesCAZyDiamond + bytesDBCANHMM + bytesDBCANSubHMM + bytesFamilySubstrateMapping
)

type releaseSizes struct {
	cazyDiamond            int64
	dbcanHMM               int64
	dbcanSubHMM            int64
	familySubstrateMapping int64
}

var pinnedSizes = releaseSizes{
	cazyDiamond:            bytesCAZyDiamond,
	dbcanHMM:               bytesDBCANHMM,
	dbcanSubHMM:            bytesDBCANSubHMM,
	familySubstrateMapping: bytesFamilySubstrateMapping,
}

func NewCommand() *cobra.Command {
	spec := newSpec(baseURL, pinnedSizes)
	command := &cobra.Command{
		Use: "dbcan", Short: spec.DatabaseDescription, SilenceUsage: true, SilenceErrors: true, Args: cobra.NoArgs,
	}
	asset := bulkasset.NewAssetCommand(spec)
	asset.RunE = func(command *cobra.Command, _ []string) error { return command.Help() }
	command.AddCommand(asset)
	return command
}

func newSpec(urlBase string, sizes releaseSizes) bulkasset.Spec {
	return bulkasset.Spec{
		Database:                   "dbcan",
		Asset:                      "database",
		Source:                     SourceName,
		DatabaseDescription:        "Manage the pinned dbCAN CAZyme database collection",
		AssetDescription:           "Manage the required 6.93 GiB dbCAN CAZyme database collection",
		VersionDescription:         "Fixed dbCAN database token (only " + VersionToken + ")",
		FixedVersion:               VersionToken,
		SourceVersion:              SourceVersion,
		DefaultWorkers:             1,
		LockOnlyDeclaredAssets:     true,
		RejectUndeclaredAssets:     true,
		RequireDefaultAssetsOnLock: true,
		RequireCompleteAssets:      true,
		DisableAssetSelection:      true,
		Assets: []bulkasset.AssetSpec{
			{Name: "cazy-diamond", Path: "CAZy.dmnd", URL: urlBase + "/CAZy.dmnd", Default: true, Large: true, ExpectedBytes: sizes.cazyDiamond},
			{Name: "dbcan-hmm", Path: "dbCAN.hmm", URL: urlBase + "/dbCAN.hmm", Default: true, Large: true, ExpectedBytes: sizes.dbcanHMM},
			{Name: "dbcan-sub-hmm", Path: "dbCAN-sub.hmm", URL: urlBase + "/dbCAN_sub.hmm", Default: true, Large: true, ExpectedBytes: sizes.dbcanSubHMM},
			{Name: "family-substrate-mapping", Path: "fam-substrate-mapping.tsv", URL: urlBase + "/fam-substrate-mapping.tsv", Default: true, Large: true, ExpectedBytes: sizes.familySubstrateMapping},
		},
	}
}
