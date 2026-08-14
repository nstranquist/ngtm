package gtm

import (
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestLaunchLedgerWithLock_Serializes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "launch-ledger.jsonl")
	led := LaunchLedger{Path: path}
	var inside atomic.Int32
	var max atomic.Int32
	done := make(chan struct{}, 2)
	work := func() {
		defer func() { done <- struct{}{} }()
		if err := led.WithLock(2*time.Second, func() error {
			n := inside.Add(1)
			if n > max.Load() {
				max.Store(n)
			}
			time.Sleep(20 * time.Millisecond)
			inside.Add(-1)
			return nil
		}); err != nil {
			t.Errorf("WithLock: %v", err)
		}
	}
	go work()
	go work()
	<-done
	<-done
	if max.Load() != 1 {
		t.Fatalf("concurrent holders = %d, want 1", max.Load())
	}
}

func TestLaunchLedgerWithLock_NilFunc(t *testing.T) {
	err := (LaunchLedger{Path: filepath.Join(t.TempDir(), "x.jsonl")}).WithLock(time.Second, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, ErrLaunchLedgerLocked) {
		t.Fatalf("nil fn should not look like lock timeout: %v", err)
	}
}
