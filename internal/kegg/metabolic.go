package kegg

import (
	"biofetch/internal/shared/bulkasset"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const metabolicBaseURL = "https://rest.kegg.jp"

var patternMetabolicLastUpdate = regexp.MustCompile(`Last update\s+([0-9]{4})/([0-9]{2})/[0-9]{2}`)

func createMetabolicCommand() *cobra.Command {
	return bulkasset.NewAssetCommand(bulkasset.Spec{
		Database:             "kegg",
		Asset:                "metabolic",
		Source:               "rest",
		AssetDescription:     "Manage KEGG global metabolic entries and mappings",
		VersionDescription:   "KEGG snapshot token; omit for current",
		SupportsFixedVersion: false,
		ResolveCurrent:       resolveMetabolicCurrentVersion,
		ExpandAssets:         expandMetabolicEntryAssets,
		DefaultRequestWait:   defaultKEGGRequestInterval,
		Assets: []bulkasset.AssetSpec{
			{Name: "compound", Path: "compound/list.tsv", URL: metabolicBaseURL + "/list/compound", Default: true},
			{Name: "reaction", Path: "reaction/list.tsv", URL: metabolicBaseURL + "/list/reaction", Default: true},
			{Name: "enzyme", Path: "enzyme/list.tsv", URL: metabolicBaseURL + "/list/enzyme", Default: true},
			{Name: "module", Path: "module/list.tsv", URL: metabolicBaseURL + "/list/module", Default: true},
			{Name: "compound-reaction", Path: "compound_reaction.tsv", URL: metabolicBaseURL + "/link/reaction/compound", Default: true},
			{Name: "reaction-enzyme", Path: "reaction_enzyme.tsv", URL: metabolicBaseURL + "/link/enzyme/reaction", Default: true},
			{Name: "reaction-ko", Path: "reaction_ko.tsv", URL: metabolicBaseURL + "/link/ko/reaction", Default: true},
			{Name: "reaction-pathway", Path: "reaction_pathway.tsv", URL: metabolicBaseURL + "/link/pathway/reaction", Default: true},
			{Name: "reaction-module", Path: "reaction_module.tsv", URL: metabolicBaseURL + "/link/module/reaction", Default: true},
			{Name: "module-pathway", Path: "module_pathway.tsv", URL: metabolicBaseURL + "/link/pathway/module", Default: true},
			{Name: "compound-pubchem", Path: "compound_pubchem.tsv", URL: metabolicBaseURL + "/conv/pubchem/compound", Default: true},
		},
	})
}

func expandMetabolicEntryAssets(clientHTTP *http.Client, selected []bulkasset.AssetSpec) ([]bulkasset.AssetSpec, error) {
	result := make([]bulkasset.AssetSpec, 0, len(selected))
	for _, asset := range selected {
		result = append(result, asset)
		switch asset.Name {
		case "compound", "reaction", "enzyme", "module":
			ids, err := resolveMetabolicEntryIDs(clientHTTP, asset.URL)
			if err != nil {
				return nil, err
			}
			result = append(result, buildMetabolicEntryBatches(asset.Name, ids, metabolicBaseURL)...)
			time.Sleep(defaultKEGGRequestInterval)
		}
	}
	return result, nil
}

func buildMetabolicEntryBatches(database string, ids []string, baseURL string) []bulkasset.AssetSpec {
	result := make([]bulkasset.AssetSpec, 0, (len(ids)+9)/10)
	for index := 0; index < len(ids); index += 10 {
		end := index + 10
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[index:end]
		result = append(result, bulkasset.AssetSpec{
			Name: fmt.Sprintf("%s-entry-%06d", database, index/10+1),
			Path: filepath.ToSlash(filepath.Join(database, "entries", fmt.Sprintf("%06d.keg", index/10+1))),
			URL:  strings.TrimRight(baseURL, "/") + "/get/" + strings.Join(batch, "+"),
		})
	}
	return result
}

func resolveMetabolicEntryIDs(clientHTTP *http.Client, urlList string) ([]string, error) {
	response, err := clientHTTP.Get(urlList)
	if err != nil {
		return nil, fmt.Errorf("discover KEGG metabolic entries from %s: %w", urlList, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discover KEGG metabolic entries from %s: unexpected status %s", urlList, response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 64*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read KEGG metabolic entry list %s: %w", urlList, err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	ids := make([]string, 0, len(lines))
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		id := strings.TrimSpace(fields[0])
		if id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("KEGG metabolic entry list is empty: %s", urlList)
	}
	return ids, nil
}

func resolveMetabolicCurrentVersion(clientHTTP *http.Client) (string, error) {
	response, err := clientHTTP.Get(metabolicBaseURL + "/info/pathway")
	if err != nil {
		return "", fmt.Errorf("resolve KEGG metabolic release: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("resolve KEGG metabolic release: unexpected status %s", response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024))
	if err != nil {
		return "", fmt.Errorf("read KEGG metabolic release: %w", err)
	}
	return parseMetabolicVersion(data)
}

func parseMetabolicVersion(data []byte) (string, error) {
	match := patternMetabolicLastUpdate.FindStringSubmatch(string(data))
	if match == nil {
		return "", fmt.Errorf("KEGG pathway Last update field not found in %q", strings.TrimSpace(string(data)))
	}
	version := match[1] + "-" + match[2]
	if !isValidKEGGSnapshotVersionToken(version) {
		return "", fmt.Errorf("invalid KEGG metabolic version %q at %s", version, time.Now().Format(time.RFC3339))
	}
	return version, nil
}
