package chemont

import "testing"

func TestSourceSpecContract(t *testing.T) {
	spec := sourceSpec()
	if spec.Database != "chemont" || spec.Asset != "ontology" || spec.Source != "classyfire" {
		t.Fatalf("identity = %#v", spec)
	}
	if spec.FixedVersion != "" || len(spec.Assets) != 1 {
		t.Fatalf("version/assets = %q/%#v", spec.FixedVersion, spec.Assets)
	}
	asset := spec.Assets[0]
	if asset.Name != "obo" || asset.Path != "ChemOnt_2_1.obo.zip" || asset.URL != "http://classyfire.wishartlab.com/system/downloads/1_0/chemont/ChemOnt_2_1.obo.zip" || !asset.Default {
		t.Fatalf("asset = %#v", asset)
	}
}
