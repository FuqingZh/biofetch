package httpx

import (
	"slices"
	"sync"
	"testing"
	"time"
)

func TestRequestLimiterWaitSpacesConcurrentCalls(t *testing.T) {
	limiter := NewRequestLimiter(20 * time.Millisecond)

	var group sync.WaitGroup
	group.Add(3)

	times := make([]time.Time, 0, 3)
	var mutexTimes sync.Mutex

	for range 3 {
		go func() {
			defer group.Done()
			limiter.Wait()
			mutexTimes.Lock()
			times = append(times, time.Now())
			mutexTimes.Unlock()
		}()
	}

	group.Wait()

	if len(times) != 3 {
		t.Fatalf("len(times) = %d, want 3", len(times))
	}
	slices.SortFunc(times, func(left, right time.Time) int {
		switch {
		case left.Before(right):
			return -1
		case right.Before(left):
			return 1
		default:
			return 0
		}
	})

	for index := 1; index < len(times); index++ {
		delta := times[index].Sub(times[index-1])
		if delta < 15*time.Millisecond {
			t.Fatalf("delta[%d] = %s, want >= 15ms", index, delta)
		}
	}
}
