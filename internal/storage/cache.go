package storage

import (
	"context"
	"sync"
	"time"
)

const (
	DefaultActiveRefreshInterval = 15 * time.Second
	DefaultIdleRefreshInterval   = 5 * time.Minute
	DefaultActivityWindow        = 60 * time.Second
)

// DiskCacheOptions configures adaptive refresh intervals for DiskCache.
type DiskCacheOptions struct {
	ActiveInterval time.Duration
	IdleInterval   time.Duration
	ActivityWindow time.Duration
	Now            func() time.Time
}

// DiskCache maintains an in-memory snapshot of discovered disks and refreshes
// it in the background. When web clients are active it refreshes at
// ActiveInterval; once clients go quiet past ActivityWindow it slows down to
// IdleInterval.
type DiskCache struct {
	fetch          func(context.Context) ([]Disk, error)
	activeInterval time.Duration
	idleInterval   time.Duration
	activityWindow time.Duration
	now            func() time.Time

	refreshMu sync.Mutex

	mu           sync.RWMutex
	disks        []Disk
	updatedAt    time.Time
	stale        bool
	lastActivity time.Time
	inIdleMode   bool

	refreshCh  chan struct{}
	activityCh chan struct{}
}

func NewDiskCache(fetch func(context.Context) ([]Disk, error), opts ...DiskCacheOptions) *DiskCache {
	var cfg DiskCacheOptions
	if len(opts) > 0 {
		cfg = opts[0]
	}
	if cfg.ActiveInterval <= 0 {
		cfg.ActiveInterval = DefaultActiveRefreshInterval
	}
	if cfg.IdleInterval <= 0 {
		cfg.IdleInterval = DefaultIdleRefreshInterval
	}
	if cfg.ActivityWindow <= 0 {
		cfg.ActivityWindow = DefaultActivityWindow
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &DiskCache{
		fetch:          fetch,
		activeInterval: cfg.ActiveInterval,
		idleInterval:   cfg.IdleInterval,
		activityWindow: cfg.ActivityWindow,
		now:            cfg.Now,
		stale:          true,
		refreshCh:      make(chan struct{}, 1),
		activityCh:     make(chan struct{}, 1),
	}
}

// Start runs the background refresh loop until ctx is canceled.
func (c *DiskCache) Start(ctx context.Context) {
	go c.loop(ctx)
}

// RecordActivity marks web activity so the background loop switches to the
// faster active refresh cadence.
func (c *DiskCache) RecordActivity() {
	c.mu.Lock()
	c.lastActivity = c.now()
	wasIdle := c.inIdleMode
	c.mu.Unlock()

	if wasIdle {
		select {
		case c.activityCh <- struct{}{}:
		default:
		}
	}
}

// Invalidate marks the cached snapshot stale so the next read fetches fresh
// data, and wakes the background loop.
func (c *DiskCache) Invalidate() {
	c.mu.Lock()
	c.stale = true
	c.mu.Unlock()
	c.TriggerRefresh()
}

// TriggerRefresh asks the background loop to refresh as soon as possible.
func (c *DiskCache) TriggerRefresh() {
	select {
	case c.refreshCh <- struct{}{}:
	default:
	}
}

// Get returns the cached disk list immediately when fresh, or performs a
// synchronous refresh when forceRefresh is true or the cache is empty/stale.
func (c *DiskCache) Get(ctx context.Context, forceRefresh bool) ([]Disk, time.Time, error) {
	c.RecordActivity()

	if !forceRefresh {
		c.mu.RLock()
		if !c.updatedAt.IsZero() && !c.stale {
			snapshot := cloneDisks(c.disks)
			updatedAt := c.updatedAt
			c.mu.RUnlock()
			return snapshot, updatedAt, nil
		}
		c.mu.RUnlock()
	}

	return c.Refresh(ctx)
}

// Refresh executes a serialized disk discovery and updates the cache.
func (c *DiskCache) Refresh(ctx context.Context) ([]Disk, time.Time, error) {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()

	disks, err := c.fetch(ctx)
	if err != nil {
		return nil, time.Time{}, err
	}
	now := c.now()
	stored := cloneDisks(disks)

	c.mu.Lock()
	c.disks = stored
	c.updatedAt = now
	c.stale = false
	c.mu.Unlock()

	return cloneDisks(stored), now, nil
}

func (c *DiskCache) loop(ctx context.Context) {
	_, _, _ = c.Refresh(ctx)

	for {
		delay, idle := c.nextDelay()
		c.mu.Lock()
		c.inIdleMode = idle
		c.mu.Unlock()

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			_, _, _ = c.Refresh(ctx)
		case <-c.refreshCh:
			timer.Stop()
			_, _, _ = c.Refresh(ctx)
		case <-c.activityCh:
			timer.Stop()
			// Web activity resumed while sleeping on the slow idle timer.
			// If the cached data is already older than the active interval,
			// refresh right away; otherwise loop to sleep the remaining delta.
			c.mu.RLock()
			age := c.now().Sub(c.updatedAt)
			needsNow := c.updatedAt.IsZero() || c.stale || age >= c.activeInterval
			c.mu.RUnlock()
			if needsNow {
				_, _, _ = c.Refresh(ctx)
			}
		}
	}
}

func (c *DiskCache) nextDelay() (time.Duration, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	now := c.now()
	active := !c.lastActivity.IsZero() && now.Sub(c.lastActivity) < c.activityWindow
	interval := c.idleInterval
	if active {
		interval = c.activeInterval
	}
	if !c.updatedAt.IsZero() && !c.stale {
		elapsed := now.Sub(c.updatedAt)
		if elapsed >= interval {
			return 0, !active
		}
		return interval - elapsed, !active
	}
	return interval, !active
}

func cloneDisks(disks []Disk) []Disk {
	if disks == nil {
		return []Disk{}
	}
	cloned := make([]Disk, len(disks))
	for i, disk := range disks {
		cloned[i] = disk
		if disk.TemperatureCelsius != nil {
			v := *disk.TemperatureCelsius
			cloned[i].TemperatureCelsius = &v
		}
		if disk.SmartHealth != nil {
			v := *disk.SmartHealth
			cloned[i].SmartHealth = &v
		}
		if disk.SolidState != nil {
			v := *disk.SolidState
			cloned[i].SolidState = &v
		}
		if disk.DataWrittenBytes != nil {
			v := *disk.DataWrittenBytes
			cloned[i].DataWrittenBytes = &v
		}
		if disk.PowerOnHours != nil {
			v := *disk.PowerOnHours
			cloned[i].PowerOnHours = &v
		}
		cloned[i].Mountpoints = append([]string{}, disk.Mountpoints...)
		cloned[i].Partitions = make([]Partition, len(disk.Partitions))
		for j, part := range disk.Partitions {
			cloned[i].Partitions[j] = part
			cloned[i].Partitions[j].Mountpoints = append([]string{}, part.Mountpoints...)
			if part.Usage != nil {
				u := *part.Usage
				cloned[i].Partitions[j].Usage = &u
			}
		}
	}
	return cloned
}
