package filehash

import (
	"strings"
	"testing"
)

func TestSHA256(t *testing.T) {
	digest, err := SHA256(strings.NewReader("biofetch"))
	if err != nil {
		t.Fatal(err)
	}
	const expected = "9aed992f7fd2e32744663893d055b03d08bf6e296e57706f5a65237f997c931b"
	if digest != expected {
		t.Fatalf("digest = %q, want %q", digest, expected)
	}
}
