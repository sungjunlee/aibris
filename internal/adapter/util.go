package adapter

import (
	"os"
	"path/filepath"
)

func estimateDirSize(path string) int64 {
	var total int64
	filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return filepath.SkipDir
		}
		info, err := d.Info()
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

func detectProjectName(path string) string {
	entries, err := os.ReadDir(path)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() && !isHiddenDir(e.Name()) {
			return e.Name()
		}
	}
	return ""
}

func isHiddenDir(name string) bool {
	return len(name) > 0 && name[0] == '.'
}
