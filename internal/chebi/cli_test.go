package chebi

import "testing"

func TestSourceSpecContract(t *testing.T) {
	spec := sourceSpec()
	if spec.Database != "chebi" || spec.Asset != "database" || spec.Source != "ebi-ftp-https" {
		t.Fatalf("identity = %#v", spec)
	}
	if len(spec.Assets) != 11 || spec.Assets[0].URL != baseURL+"/ontology/chebi.obo.gz" {
		t.Fatalf("assets = %#v", spec.Assets)
	}
	if spec.Assets[0].Name != "ontology" || !spec.Assets[0].Default {
		t.Fatalf("ontology asset = %#v", spec.Assets[0])
	}
	if !spec.Assets[len(spec.Assets)-1].Large || spec.Assets[len(spec.Assets)-1].Name != "postgres-dump" {
		t.Fatalf("postgres dump asset = %#v", spec.Assets[len(spec.Assets)-1])
	}
}
