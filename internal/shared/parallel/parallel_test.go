package parallel

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestMapOrderedPreservesOrder(t *testing.T) {
	valuesMapped, err := MapOrdered([]int{1, 2, 3}, func(value int) (string, error) {
		switch value {
		case 1:
			time.Sleep(10 * time.Millisecond)
		case 2:
			time.Sleep(1 * time.Millisecond)
		}
		return fmt.Sprintf("v%d", value), nil
	})
	if err != nil {
		t.Fatalf("MapOrdered returned error: %v", err)
	}

	expected := []string{"v1", "v2", "v3"}
	if !reflect.DeepEqual(valuesMapped, expected) {
		t.Fatalf("MapOrdered = %#v, want %#v", valuesMapped, expected)
	}
}

func TestMapOrderedReturnsError(t *testing.T) {
	_, err := MapOrdered([]int{1, 2, 3}, func(value int) (string, error) {
		if value == 2 {
			return "", fmt.Errorf("bad value")
		}
		return fmt.Sprintf("v%d", value), nil
	})
	if err == nil || err.Error() != "bad value" {
		t.Fatalf("MapOrdered error = %v", err)
	}
}

func TestDeriveWorkerCountCapsByTaskCount(t *testing.T) {
	if value := deriveWorkerCount(1); value != 1 {
		t.Fatalf("deriveWorkerCount = %d, want 1", value)
	}
}

func TestMapOrderedWithWorkersPreservesOrder(t *testing.T) {
	valuesMapped, err := MapOrderedWithWorkers([]int{1, 2, 3}, 2, func(value int) (string, error) {
		switch value {
		case 1:
			time.Sleep(10 * time.Millisecond)
		case 2:
			time.Sleep(1 * time.Millisecond)
		}
		return fmt.Sprintf("v%d", value), nil
	})
	if err != nil {
		t.Fatalf("MapOrderedWithWorkers returned error: %v", err)
	}

	expected := []string{"v1", "v2", "v3"}
	if !reflect.DeepEqual(valuesMapped, expected) {
		t.Fatalf("MapOrderedWithWorkers = %#v, want %#v", valuesMapped, expected)
	}
}

func TestDeriveWorkerCountMaxHonorsWorkersMax(t *testing.T) {
	if value := deriveWorkerCountMax(4, 2); value != 2 {
		t.Fatalf("deriveWorkerCountMax = %d, want 2", value)
	}
}
