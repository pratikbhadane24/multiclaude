package cli

import "runtime/debug"

const (
	// develVersion is what a plain `go build` reports for the main module.
	develVersion = "(devel)"
	// unknownVersion is shown when no version was recorded at build time.
	unknownVersion = "dev"
)

// Version is the ccm release, set at build time with
// -ldflags "-X github.com/pratikbhadane24/multiclaude/internal/cli.Version=v1.2.3".
// It must stay a plain string with no initializer for -X to apply.
var Version string

// versionString is Version, else the module version recorded by the Go
// toolchain (go install ...@v1.2.3), else "dev".
func versionString() string {
	if Version != "" {
		return Version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == develVersion {
		return unknownVersion
	}
	return info.Main.Version
}
