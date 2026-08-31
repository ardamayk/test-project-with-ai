//go:build !windows

package managedimport

func restrictManagedStoragePath(string, string, bool) error {
	return nil
}
