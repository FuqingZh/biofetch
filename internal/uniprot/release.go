package uniprot

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

const uniprotCurrentVersionToken = "current"

var uniprotCurrentReleaseBaseURL = "https://ftp.uniprot.org/pub/databases/uniprot/current_release/"
var patternUniProtRelease = regexp.MustCompile(`(?m)^UniProt Release ([0-9]{4}_[0-9]{2})\b`)
var patternUniProtVersion = regexp.MustCompile(`^[0-9]{4}_[0-9]{2}$`)

func buildUniProtCurrentReleaseURL(baseURL string, pathParts ...string) string {
	url := strings.TrimRight(baseURL, "/")
	for _, part := range pathParts {
		part = strings.Trim(part, "/")
		if part == "" {
			continue
		}
		url += "/" + part
	}
	return url
}

func resolveUniProtFetchVersionToken(clientHTTP *http.Client, value string, baseURLCurrentRelease string, label string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = uniprotCurrentVersionToken
	}
	if strings.EqualFold(value, uniprotCurrentVersionToken) {
		return resolveUniProtCurrentVersionToken(clientHTTP, baseURLCurrentRelease, label)
	}
	return normalizeUniProtFixedVersionToken(value, label)
}

func normalizeUniProtFixedVersionToken(value string, label string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, uniprotCurrentVersionToken) {
		return "", fmt.Errorf("%s version must be a fixed release token for this operation, not current", label)
	}
	if !patternUniProtVersion.MatchString(value) {
		return "", fmt.Errorf("%s version must look like 2026_01: %s", label, value)
	}
	return value, nil
}

func resolveUniProtCurrentVersionToken(clientHTTP *http.Client, baseURLCurrentRelease string, label string) (string, error) {
	request, err := http.NewRequest(http.MethodGet, buildUniProtCurrentReleaseURL(baseURLCurrentRelease, "relnotes.txt"), nil)
	if err != nil {
		return "", fmt.Errorf("build %s current release request: %w", label, err)
	}
	request.Header.Set("Accept", "text/plain")
	response, err := clientHTTP.Do(request)
	if err != nil {
		return "", fmt.Errorf("resolve %s current release version: %w", label, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("resolve %s current release version: unexpected status %s", label, response.Status)
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return "", fmt.Errorf("read %s current release version: %w", label, err)
	}
	return parseUniProtReleaseNotes(data, label)
}

func parseUniProtReleaseNotes(data []byte, label string) (string, error) {
	matches := patternUniProtRelease.FindSubmatch(data)
	if len(matches) != 2 {
		return "", fmt.Errorf("parse %s current release version: release token not found", label)
	}
	return string(matches[1]), nil
}
