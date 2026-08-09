//go:build !libudev || !linux || !cgo

package storage

import (
	"context"
)

func WatchUdev(context.Context, func()) error {
	return ErrUdevUnavailable
}
