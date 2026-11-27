package main

import (
	"context"
	"sync"
	"time"

	"github.com/sgpublic/simplefsmanager/internal/storage"
)

// topologyReconciler coalesces rapid udev block events into serialized
// reconciliation runs after a debounce window.
type topologyReconciler struct {
	delay     time.Duration
	onTrigger func(ctx context.Context, addedDisks []string)

	mu         sync.Mutex
	timer      *time.Timer
	addedDisks []string
	addedSet   map[string]struct{}
	running    bool
	pending    bool
}

func newTopologyReconciler(delay time.Duration, onTrigger func(ctx context.Context, addedDisks []string)) *topologyReconciler {
	return &topologyReconciler{
		delay:     delay,
		onTrigger: onTrigger,
		addedSet:  make(map[string]struct{}),
	}
}

func (r *topologyReconciler) Notify(ctx context.Context, event storage.UdevEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if event.Action == "add" && event.Devtype == "disk" && event.Devnode != "" {
		if _, exists := r.addedSet[event.Devnode]; !exists {
			r.addedSet[event.Devnode] = struct{}{}
			r.addedDisks = append(r.addedDisks, event.Devnode)
		}
	}
	r.pending = true
	if r.running {
		return
	}
	r.resetTimerLocked(ctx)
}

func (r *topologyReconciler) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.timer != nil {
		r.timer.Stop()
		r.timer = nil
	}
	r.pending = false
}

func (r *topologyReconciler) resetTimerLocked(ctx context.Context) {
	if r.timer != nil {
		r.timer.Stop()
	}
	r.timer = time.AfterFunc(r.delay, func() {
		r.run(ctx)
	})
}

func (r *topologyReconciler) run(ctx context.Context) {
	for {
		r.mu.Lock()
		if !r.pending || ctx.Err() != nil {
			r.running = false
			r.timer = nil
			r.mu.Unlock()
			return
		}
		r.running = true
		r.pending = false
		r.timer = nil
		added := r.addedDisks
		r.addedDisks = nil
		r.addedSet = make(map[string]struct{})
		r.mu.Unlock()

		r.onTrigger(ctx, added)

		r.mu.Lock()
		if !r.pending || ctx.Err() != nil {
			r.running = false
			r.mu.Unlock()
			return
		}
		// New events arrived while reconciling; wait one debounce window before
		// running again so bursts still coalesce.
		r.running = false
		r.resetTimerLocked(ctx)
		r.mu.Unlock()
		return
	}
}
