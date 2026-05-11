// Package version holds build-time version information injected via ldflags.
//
// Build example:
//
//	go build \
//	  -ldflags "-X github.com/facturaIA/invoice-ocr-service/internal/version.Version=v2.41.0
//	            -X github.com/facturaIA/invoice-ocr-service/internal/version.Commit=abc1234" \
//	  ./cmd/server
package version

// Version is the semver string, overridden at build time via ldflags.
// Default "dev" prevents silent empty-string surprises.
var Version = "dev"

// Commit is the short git SHA, overridden at build time via ldflags.
var Commit = "unknown"
