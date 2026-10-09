package cleaner

import "os"

// cacheLeafIsLink refuses symlinks and non-symlink Windows reparse points,
// which Go reports as ModeIrregular. Unknown irregular leaves fail closed
// on other platforms too.
func cacheLeafIsLink(mode os.FileMode) bool {
	return mode&(os.ModeSymlink|os.ModeIrregular) != 0
}
