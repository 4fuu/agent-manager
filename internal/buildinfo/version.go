package buildinfo

import (
	_ "embed"
	"strings"
)

//go:embed version.txt
var version string

var Version = strings.TrimSpace(version)

// Commit is set by the release build; source builds retain the local marker.
var Commit = "local"
