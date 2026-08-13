package jaspar

import "testing"

func TestSourceSpecContract(t *testing.T) {
	spec := sourceSpec()
	if spec.Database != "jaspar" || spec.Asset != "data" || spec.Source != "jaspar-download" || !spec.SupportsFixedVersion {
		t.Fatalf("spec identity = %#v", spec)
	}
	if len(spec.Assets) != 7 || spec.Assets[0].Path != "JASPAR{version}_CORE_non-redundant_pfms_jaspar.zip" {
		t.Fatalf("assets = %#v", spec.Assets)
	}
	for _, name := range []string{"core-pfm", "core-meme", "core-metadata"} {
		found := false
		for _, asset := range spec.Assets {
			if asset.Name == name {
				found = asset.Default
			}
		}
		if !found {
			t.Fatalf("default asset %q missing", name)
		}
	}
}
