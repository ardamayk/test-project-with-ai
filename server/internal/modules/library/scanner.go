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
				abs, err := filepath.Abs(root)
				if err != nil {
					abs = root
				}
				if _, dup := seen[abs]; !dup {
					meta, err := readFileMetadata(abs, format, info)
					if err != nil {
						return nil, err
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
			abs, err := filepath.Abs(path)
			if err != nil {
				abs = path
			}
			if _, dup := seen[abs]; dup {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			meta, err := readFileMetadata(abs, format, info)
			if err != nil {
				return err
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
