package complexportal

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/FuqingZh/biofetch/internal/shared/staticasset"
	"golang.org/x/net/html"
)

const sourceName = "embl-ebi-complex-portal"
const maxIndexBytes = 2 << 20 // Directory listings, never database payloads.

var tabName = regexp.MustCompile(`^[1-9][0-9]*(_predicted)?\.tsv$`)

func validateVersion(version string) error {
	if date, err := time.Parse("2006-01-02", version); err != nil || date.Format("2006-01-02") != version {
		return fmt.Errorf("Complex Portal version must be a valid YYYY-MM-DD release: %q", version)
	}
	return nil
}

func resolveVersion(client *http.Client, base, requested string) (string, error) {
	if requested != "" && requested != "current" {
		return requested, validateVersion(requested)
	}
	data, err := readIndex(client, base)
	if err != nil {
		return "", err
	}
	links, err := directoryLinks(data, base)
	if err != nil {
		return "", err
	}
	var versions []string
	for _, link := range links {
		if strings.HasSuffix(link, "/") {
			v := strings.TrimSuffix(link, "/")
			if validateVersion(v) == nil {
				versions = append(versions, v)
			}
		}
	}
	if len(versions) == 0 {
		return "", fmt.Errorf("no dated Complex Portal releases found at %s", base)
	}
	slices.Sort(versions)
	return versions[len(versions)-1], nil
}

func readIndex(client *http.Client, address string) ([]byte, error) {
	bounded := *client
	bounded.Timeout = 30 * time.Second
	response, err := bounded.Get(address)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Complex Portal metadata %s: %s", address, response.Status)
	}
	return boundedIndex(response.Body)
}

func boundedIndex(reader io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxIndexBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxIndexBytes {
		return nil, fmt.Errorf("Complex Portal directory listing exceeds %d bytes", maxIndexBytes)
	}
	return data, nil
}

// directoryLinks admits only immediate children of this exact upstream
// directory. Parent, sort, foreign-host and encoded traversal links are ignored.
func directoryLinks(data []byte, base string) ([]string, error) {
	root, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	document, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var links []string
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "a" {
			for _, attr := range node.Attr {
				if attr.Key != "href" {
					continue
				}
				ref, err := url.Parse(attr.Val)
				if err != nil || ref.RawQuery != "" || ref.Fragment != "" || ref.User != nil || ref.RawPath != "" {
					continue
				}
				target := root.ResolveReference(ref)
				if target.Scheme != root.Scheme || target.Host != root.Host || !strings.HasPrefix(target.Path, root.Path) {
					continue
				}
				name := strings.TrimPrefix(target.Path, root.Path)
				if name != "" && !strings.Contains(strings.TrimSuffix(name, "/"), "/") {
					links = append(links, name)
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(document)
	slices.Sort(links)
	return slices.Compact(links), nil
}

func tabInventory(data []byte, address string) ([]string, error) {
	links, err := directoryLinks(data, address)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, name := range links {
		if strings.HasSuffix(name, ".tsv") {
			if !tabName.MatchString(name) {
				return nil, fmt.Errorf("unrecognized ComplexTab file %q", name)
			}
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("ComplexTab inventory contains no species TSV files")
	}
	return names, nil
}

func resolveSource(client *http.Client, base, asset, version, action, snapshot string) (staticasset.Source, error) {
	source := staticasset.Source{
		Database: "complexportal", Asset: asset, Source: sourceName,
		Version: version, VersionToken: version, LockOnlyDeclaredAssets: true,
		RequireCompleteAssets: true, RejectUndeclaredAssets: true,
	}
	address := base + version + "/" + asset + "/"
	if asset == "psi30" {
		source.Assets = []staticasset.Asset{{
			Name: "complexes", Path: "raw/complexesMIF30.zip",
			URL: address + "complexesMIF30.zip", VerifyDownloadedFile: verifyXMLArchive,
		}}
	} else {
		data, err := snapshotIndex(client, address, version, action, snapshot)
		if err != nil {
			return source, err
		}
		names, err := tabInventory(data, address)
		if err != nil {
			return source, err
		}
		source.Assets = append(source.Assets, staticasset.Asset{
			Name: "inventory", Path: "raw/index.html", URL: address,
			VerifyDownloadedFile: func(path string) error {
				actual, err := localIndex(path)
				if err != nil {
					return err
				}
				found, err := tabInventory(actual, address)
				if err != nil {
					return err
				}
				if !slices.Equal(found, names) {
					return fmt.Errorf("ComplexTab inventory changed during acquisition")
				}
				return nil
			},
		})
		for _, name := range names {
			source.Assets = append(source.Assets, staticasset.Asset{
				Name: strings.TrimSuffix(name, ".tsv"), Path: "raw/" + name,
				URL: address + name, VerifyDownloadedFile: verifyComplexTab,
			})
		}
	}
	for _, item := range source.Assets {
		source.RequiredAssets = append(source.RequiredAssets, item.Name)
	}
	return source, nil
}

func localIndex(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return boundedIndex(file)
}

func snapshotIndex(client *http.Client, address, version, action, snapshot string) ([]byte, error) {
	path := filepath.Join(snapshot, "raw", "index.html")
	if action == "lock" {
		return localIndex(path)
	}
	manifest, exists, err := staticasset.ReadManifest(filepath.Join(snapshot, "manifest.lock"))
	if err != nil {
		return nil, err
	}
	if !exists {
		if action == "restore" {
			return nil, fmt.Errorf("manifest is missing: %s", filepath.Join(snapshot, "manifest.lock"))
		}
		return readIndex(client, address)
	}
	if manifest.Database != "complexportal" || manifest.Asset != "complextab" ||
		manifest.Source != sourceName || manifest.Version != version || manifest.VersionToken != version {
		return nil, fmt.Errorf("ComplexTab manifest identity or provenance mismatch")
	}
	var record *staticasset.ManifestFileRecord
	for i := range manifest.Files {
		item := &manifest.Files[i]
		if item.Path == "raw/index.html" && item.Asset == "inventory" {
			if record != nil || item.URL != address || item.Bytes <= 0 || item.Bytes > maxIndexBytes {
				return nil, fmt.Errorf("invalid ComplexTab inventory record")
			}
			record = item
		}
	}
	if record == nil {
		return nil, fmt.Errorf("ComplexTab manifest has no locked inventory")
	}
	matches := func(data []byte) bool {
		return int64(len(data)) == record.Bytes && fmt.Sprintf("%x", sha256.Sum256(data)) == record.SHA256
	}
	data, err := localIndex(path)
	if err == nil && matches(data) {
		return data, nil
	}
	// Recover only from the pinned URL and original digest; never rediscover
	// current or bless a changed upstream directory as the same snapshot.
	data, err = readIndex(client, address)
	if err != nil {
		return nil, err
	}
	if !matches(data) {
		return nil, fmt.Errorf("ComplexTab inventory differs from its locked SHA256")
	}
	return data, nil
}

func verifyComplexTab(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 16<<20)
	if !scanner.Scan() {
		return fmt.Errorf("empty ComplexTab file")
	}
	header := strings.Split(scanner.Text(), "\t")
	if len(header) < 5 || header[0] != "#Complex ac" || header[3] != "Taxonomy identifier" {
		return fmt.Errorf("invalid ComplexTab header")
	}
	for scanner.Scan() {
		row := strings.Split(scanner.Text(), "\t")
		if len(row) != len(header) || !strings.HasPrefix(row[0], "CPX-") {
			return fmt.Errorf("invalid ComplexTab row")
		}
	}
	return scanner.Err()
}

func verifyXMLArchive(path string) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer reader.Close()
	if len(reader.File) == 0 {
		return fmt.Errorf("empty Complex Portal XML archive")
	}
	for _, member := range reader.File {
		if member.FileInfo().IsDir() || !strings.HasSuffix(member.Name, ".xml") {
			return fmt.Errorf("unexpected Complex Portal ZIP member %q", member.Name)
		}
		handle, err := member.Open()
		if err != nil {
			return err
		}
		_, readErr := io.Copy(io.Discard, handle) // Verify every member CRC; do not extract.
		closeErr := handle.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func rejectSymlinks(snapshot string) error {
	raw := filepath.Join(snapshot, "raw")
	for _, path := range []string{snapshot, raw, filepath.Join(snapshot, "manifest.lock")} {
		info, err := os.Lstat(path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("snapshot symlink is not supported: %s", path)
		}
	}
	entries, err := os.ReadDir(raw)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("raw symlink is not supported: %s", entry.Name())
		}
	}
	return nil
}
