// Package version exposes build metadata, set via -ldflags.
package version

// Version is overridden at build time: -ldflags "-X .../internal/version.Version=v0.1.0".
var Version = "dev"
