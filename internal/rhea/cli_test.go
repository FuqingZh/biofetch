package rhea

import "testing"

func TestSourceSpecContract(t *testing.T) {
	spec := sourceSpec()
	if spec.Database != "rhea" || spec.Asset != "database" || spec.Source != "expasy-ftp-https" {
		t.Fatalf("identity = %#v", spec)
	}
	if len(spec.Assets) != 16 || spec.Assets[0].URL != baseURL+"/rhea-release.properties" {
		t.Fatalf("assets = %#v", spec.Assets)
	}
	if spec.Assets[0].Name != "release" || !spec.Assets[0].Default {
		t.Fatalf("release asset = %#v", spec.Assets[0])
	}
	if spec.Assets[14].Default || spec.Assets[14].Name != "biopax" {
		t.Fatalf("optional biopax asset = %#v", spec.Assets[14])
	}
}
