//go:build release

package static

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// FS returns the embedded frontend filesystem rooted at dist/
func FS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}

// HasEmbeddedAssets returns true if the embedded filesystem has content
func HasEmbeddedAssets() bool {
	entries, err := distFS.ReadDir("dist")
	return err == nil && len(entries) > 0
}
