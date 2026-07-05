//go:build !release

package static

import (
	"io/fs"
)

// FS returns an error in non-release builds (no embedded assets)
func FS() (fs.FS, error) {
	return nil, fs.ErrNotExist
}

// HasEmbeddedAssets returns false in non-release builds
func HasEmbeddedAssets() bool {
	return false
}
