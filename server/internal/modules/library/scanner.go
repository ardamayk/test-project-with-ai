package library

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

type ScannedFile struct {
	Metadata FileMetadata
}

func WalkMusicPaths(paths []string) ([]ScannedFile, error) {
	var files []ScannedFile
	seen := make(map[string]struct{})

	for _, root := range paths {
		root = filepath.Clean(root)
		info, err := os.Stat(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("stat music path %q: %w", root, err)
		}
		if !info.IsDir() {
			if format, ok := isSupportedFile(info.Name()); ok {
				abs, absolutePathErr := filepath.Abs(root)
				if absolutePathErr != nil {
					abs = root
				}
				if _, dup := seen[abs]; !dup {
					meta, metadataErr := readFileMetadata(abs, format, info)
					if metadataErr != nil {
						return nil, metadataErr
					}
					files = append(files, ScannedFile{Metadata: meta})
					seen[abs] = struct{}{}
				}
			}
			continue
		}

		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			format, ok := isSupportedFile(d.Name())
			if !ok {
				return nil
			}
			abs, absolutePathErr := filepath.Abs(path)
			if absolutePathErr != nil {
				abs = path
			}
			if _, dup := seen[abs]; dup {
				return nil
			}
			info, infoErr := d.Info()
			if infoErr != nil {
				return infoErr
			}
			meta, metadataErr := readFileMetadata(abs, format, info)
			if metadataErr != nil {
				return metadataErr
			}
			files = append(files, ScannedFile{Metadata: meta})
			seen[abs] = struct{}{}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk %q: %w", root, err)
		}
	}

	return files, nil
}
