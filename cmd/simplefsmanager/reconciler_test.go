package main

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/sgpublic/simplefsmanager/internal/storage"
)

func TestTopologyReconcilerCoalescesEventsAndPreservesAddedDisks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var calls [][]string
	done := make(chan struct{}, 2)

	r := newTopologyReconciler(30*time.Millisecond, func(_ context.Context, added []string) {
		mu.Lock()
		calls = append(calls, append([]string(nil), added...))
		mu.Unlock()
		done <- struct{}{}
	})
	defer r.Stop()

	r.Notify(ctx, storage.UdevEvent{Action: "add", Devnode: "/dev/sdb", Devtype: "disk"})
	r.Notify(ctx, storage.UdevEvent{Action: "add", Devnode: "/dev/sdb1", Devtype: "partition"})
	r.Notify(ctx, storage.UdevEvent{Action: "change", Devnode: "/dev/sdb", Devtype: "disk"})
	r.Notify(ctx, storage.UdevEvent{Action: "add", Devnode: "/dev/sdc", Devtype: "disk"})
	r.Notify(ctx, storage.UdevEvent{Action: "add", Devnode: "/dev/sdb", Devtype: "disk"})

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reconciler did not trigger within timeout")
	}

	// Ensure no spurious second run occurs.
	select {
	case <-done:
		t.Fatal("expected burst of events to coalesce into a single run")
	case <-time.After(80 * time.Millisecond):
	}

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	want := []string{"/dev/sdb", "/dev/sdc"}
	if !slices.Equal(calls[0], want) {
		t.Fatalf("added disks = %v, want %v", calls[0], want)
	}
}
