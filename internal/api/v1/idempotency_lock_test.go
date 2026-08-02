package v1

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestIdempotencyLocksSerializeAndReleaseEntries(t *testing.T) {
	const workers = 24
	var active atomic.Int32
	var overlap atomic.Bool
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			release := acquireIdempotencyLock(991, "shared-test-key")
			if active.Add(1) != 1 {
				overlap.Store(true)
			}
			time.Sleep(time.Millisecond)
			active.Add(-1)
			release()
		}()
	}
	wait.Wait()
	if overlap.Load() {
		t.Fatal("same key executed concurrently")
	}
	idempotencyLocks.Lock()
	_, retained := idempotencyLocks.entries["991:shared-test-key"]
	idempotencyLocks.Unlock()
	if retained {
		t.Fatal("unused keyed lock was retained")
	}
}

func TestIdempotencyLocksDoNotGrowWithKeyChurn(t *testing.T) {
	for i := range 100 {
		release := acquireIdempotencyLock(992, fmt.Sprintf("churn-%d", i))
		release()
	}
	idempotencyLocks.Lock()
	defer idempotencyLocks.Unlock()
	for i := range 100 {
		if _, retained := idempotencyLocks.entries[fmt.Sprintf("992:churn-%d", i)]; retained {
			t.Fatalf("lock %d retained", i)
		}
	}
}
