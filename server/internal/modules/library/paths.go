package library

import (
	"os"
	"path/filepath"
	"strings"
)

func isPathUnderRoots(path string, roots []string) bool {
	if path == "" || len(roots) == 0 {
		return false
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	for _, root := range roots {
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(absRoot, absPath)
		if err != nil {
			continue
		}
		if rel == "." || (!strings.HasPrefix(rel, "..") && !strings.Contains(rel, ".."+string(os.PathSeparator))) {
			return true
		}
	}

	return false
}
