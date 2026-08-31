//go:build !windows

package managedimport

import "os"

func restrictManagedStorageFile(*os.File) error {
	return nil
}
