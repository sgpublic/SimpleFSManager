package storage

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestDiskCacheServesCachedAndSupportsForceRefreshAndInvalidate(t *testing.T) {
	var fetches atomic.Int32
	cache := NewDiskCache(func(context.Context) ([]Disk, error) {
		n := fetches.Add(1)
		return []Disk{{
			Path: "/dev/sdb",
			Name: "sdb",
			SizeBytes: uint64(n) * 1000,
			Partitions: []Partition{{Path: "/dev/sdb1", UUID: "u1"}},
		}}, nil
	}, DiskCacheOptions{
		ActiveInterval: time.Hour,
		IdleInterval:   time.Hour,
		ActivityWindow: time.Hour,
	})

	ctx := context.Background()
	first, ts1, err := cache.Get(ctx, false)
	if err != nil || len(first) != 1 || first[0].SizeBytes != 1000 || ts1.IsZero() {
		t.Fatalf("first Get = %#v, %v, %v", first, ts1, err)
	}
	// Mutating the returned slice must not affect the cached snapshot.
	first[0].Partitions[0].MountPath = "/mutated"

	second, _, err := cache.Get(ctx, false)
	if err != nil || second[0].SizeBytes != 1000 || second[0].Partitions[0].MountPath != "" {
		t.Fatalf("second Get = %#v, %v", second, err)
	}
	if fetches.Load() != 1 {
		t.Fatalf("fetches = %d, want 1", fetches.Load())
	}

	forced, _, err := cache.Get(ctx, true)
	if err != nil || forced[0].SizeBytes != 2000 {
		t.Fatalf("forced Get = %#v, %v", forced, err)
	}
	if fetches.Load() != 2 {
		t.Fatalf("fetches = %d, want 2", fetches.Load())
	}

	cache.Invalidate()
	afterInvalidate, _, err := cache.Get(ctx, false)
	if err != nil || afterInvalidate[0].SizeBytes != 3000 {
		t.Fatalf("afterInvalidate Get = %#v, %v", afterInvalidate, err)
	}
}

func TestDiskCacheAdaptiveRefreshRate(t *testing.T) {
	var fetches atomic.Int32
	cache := NewDiskCache(func(context.Context) ([]Disk, error) {
		fetches.Add(1)
		return []Disk{{Path: "/dev/sdb"}}, nil
	}, DiskCacheOptions{
		ActiveInterval: 25 * time.Millisecond,
		IdleInterval:   400 * time.Millisecond,
		ActivityWindow: 80 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cache.Start(ctx)

	// Wait for initial background fetch while idle.
	deadline := time.Now().Add(time.Second)
	for fetches.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if fetches.Load() != 1 {
		t.Fatalf("initial fetches = %d, want 1", fetches.Load())
	}

	// While idle (400ms interval), 70ms later there should still be no extra fetch.
	time.Sleep(70 * time.Millisecond)
	if got := fetches.Load(); got != 1 {
		t.Fatalf("idle fetches after 70ms = %d, want 1", got)
	}

	// Record web activity: should wake from idle and refresh at the 25ms active rate.
	cache.RecordActivity()
	time.Sleep(90 * time.Millisecond)
	activeFetches := fetches.Load()
	if activeFetches < 3 {
		t.Fatalf("active fetches = %d, want >= 3", activeFetches)
	}

	// Stop activity so the 80ms activity window expires and cache returns to idle.
	time.Sleep(110 * time.Millisecond)
	afterCooldown := fetches.Load()
	time.Sleep(80 * time.Millisecond)
	if got := fetches.Load(); got != afterCooldown {
		t.Fatalf("fetches after returning to idle grew from %d to %d", afterCooldown, got)
	}
}
