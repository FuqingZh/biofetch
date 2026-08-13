package lipidmaps

import "testing"

func TestSourceSpecContract(t *testing.T) {
	spec := sourceSpec()
	if spec.Database != "lipidmaps" || spec.Asset != "lmsd" || spec.Source != "lipidmaps-download" {
		t.Fatalf("identity = %#v", spec)
	}
	if len(spec.Assets) != 3 {
		t.Fatalf("asset count = %d", len(spec.Assets))
	}
	for _, asset := range spec.Assets {
		if !asset.Default {
			t.Fatalf("asset is not default: %#v", asset)
		}
	}
}
