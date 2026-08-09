//go:build linux && cgo && libudev

package storage

import (
	"context"
	"fmt"

	udev "github.com/jochenvg/go-udev"
)

// WatchUdev calls changed when a block device is added, removed, or changed.
func WatchUdev(ctx context.Context, changed func()) error {
	u := udev.Udev{}
	monitor := u.NewMonitorFromNetlink("udev")
	if err := monitor.FilterAddMatchSubsystem("block"); err != nil {
		return fmt.Errorf("filter udev block events: %w", err)
	}
	events, errors, err := monitor.DeviceChan(ctx)
	if err != nil {
		return fmt.Errorf("listen for udev events: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-errors:
			if err != nil {
				return fmt.Errorf("read udev event: %w", err)
			}
		case device := <-events:
			if device != nil && (device.Action() == "add" || device.Action() == "remove" || device.Action() == "change") {
				changed()
			}
		}
	}
}
