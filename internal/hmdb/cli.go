package hmdb

import (
	"biofetch/internal/shared/archiveverify"
	"biofetch/internal/shared/bulkasset"
	"biofetch/internal/shared/httpx"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

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
		LockOnlyDeclaredAssets: true,
		ResolveCurrent: func(*http.Client) (string, error) {
			return "5.0", nil
		},
		Assets: []bulkasset.AssetSpec{
			hmdbAsset("metabolites", "hmdb_metabolites.zip", "hmdb_metabolites.xml"),
			hmdbAsset("proteins", "hmdb_proteins.zip", "hmdb_proteins.xml"),
			hmdbAsset("structures", "structures.zip", "structures.sdf"),
			{Name: "protein-fasta", Path: "fasta_proteins.zip", URL: baseURL + "/fasta_proteins.zip"},
			{Name: "gene-fasta", Path: "fasta_genes.zip", URL: baseURL + "/fasta_genes.zip"},
			{Name: "all-spectra-xml", Path: "all_spectra.zip", URL: baseURL + "/all_spectra.zip", Large: true},
		},
	})
}

func hmdbAsset(name, path, member string) bulkasset.AssetSpec {
	return bulkasset.AssetSpec{
		Name: name, Path: path, URL: baseURL + "/" + path, Default: true,
		VerifyDownloadedFile: archiveverify.ZIPRequiredMember(member),
		RecoverDownloadError: recoverAuthorizationFailure,
	}
}

func recoverAuthorizationFailure(fileOut string, err error) (bool, error) {
	var status httpx.UnexpectedStatusError
	if !errors.As(err, &status) {
		return false, nil
	}
	challenge := strings.EqualFold(strings.TrimSpace(status.CFMitigated), "challenge")
	if status.Code != http.StatusUnauthorized && status.Code != http.StatusForbidden && !challenge {
		return false, nil
	}
	snapshot := filepath.Dir(filepath.Dir(fileOut))
	return false, fmt.Errorf(
		"HMDB download requires browser authorization (%s); download the official files in an authorized browser into %s, then run `biofetch hmdb database lock %s`: %w",
		status.Status, filepath.Join(snapshot, "raw"), snapshot, err,
	)
}
