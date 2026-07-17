package sets

import (
	"reflect"
	"testing"
)

func TestSortedKeys(t *testing.T) {
	set := map[string]struct{}{"zeta": {}, "alpha": {}, "beta": {}}
	if actual, expected := SortedKeys(set), []string{"alpha", "beta", "zeta"}; !reflect.DeepEqual(actual, expected) {
		t.Fatalf("SortedKeys = %#v, want %#v", actual, expected)
	}
}

func TestSortedKeysEmpty(t *testing.T) {
	if actual := SortedKeys(nil); len(actual) != 0 {
		t.Fatalf("SortedKeys(nil) = %#v, want empty", actual)
	}
}
