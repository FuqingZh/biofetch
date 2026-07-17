package confirm

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRequireLiteralAcceptsTrimmedInput(t *testing.T) {
	var output bytes.Buffer
	if err := RequireLiteral(strings.NewReader("  DELETE  \n"), &output, "Destructive action", "DELETE"); err != nil {
		t.Fatalf("RequireLiteral returned error: %v", err)
	}
	for _, expected := range []string{"Destructive action", `Type "DELETE" to continue.`, "> "} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("prompt %q does not contain %q", output.String(), expected)
		}
	}
}

func TestRequireLiteralRejectsMismatch(t *testing.T) {
	err := RequireLiteral(strings.NewReader("delete\n"), &bytes.Buffer{}, "risk", "DELETE")
	if err == nil || !strings.Contains(err.Error(), `expected "DELETE"`) {
		t.Fatalf("RequireLiteral error = %v", err)
	}
}

func TestRequireLiteralReportsReadError(t *testing.T) {
	errExpected := errors.New("read failed")
	err := RequireLiteral(errorReader{err: errExpected}, &bytes.Buffer{}, "risk", "DELETE")
	if !errors.Is(err, errExpected) {
		t.Fatalf("RequireLiteral error = %v, want wrapped %v", err, errExpected)
	}
}

type errorReader struct {
	err error
}

func (reader errorReader) Read([]byte) (int, error) {
	return 0, reader.err
}
