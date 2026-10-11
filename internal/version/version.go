// Package version reports which release of kyu a binary is.
package version

import "runtime/debug"

// version is stamped at release time:
//
//	-ldflags "-X github.com/koppathachan/kyu/internal/version.version=v0.1.0"
//
// A binary built without the stamp falls back to what the Go toolchain
// recorded, which is the module version for `go install …@v0.1.0` and
// "(devel)" for a build from a checkout.
var version string

// String is the release this binary was built from.
func String() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "(devel)"
}
