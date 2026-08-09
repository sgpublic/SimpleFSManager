// Package buildinfo exposes metadata injected when the service is linked.
package buildinfo

// Version is replaced by -ldflags during release builds.
var Version = "dev"
