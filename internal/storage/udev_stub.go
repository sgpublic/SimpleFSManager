//go:build !libudev || !linux || !cgo

package storage

import (
	"context"
)

func WatchUdev(context.Context, func(UdevEvent)) error {
	return ErrUdevUnavailable
}
