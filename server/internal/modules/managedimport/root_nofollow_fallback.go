//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd

package managedimport

import "os"

func openManagedStorageRoot(path string) (*os.Root, error) {
	return os.OpenRoot(path)
}
