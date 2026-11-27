//go:build !linux || !cgo || nopam

package auth

import "errors"

func authenticatePAM(_, _ string) error {
	return errors.New("PAM authentication is unavailable in this build")
}
