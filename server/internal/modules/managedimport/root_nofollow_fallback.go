//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows && !zos

package managedimport

import (
	"fmt"
	"os"
)

func openManagedStorageRoot(string) (*os.Root, error) {
	return nil, fmt.Errorf("%w: secure Managed Storage roots are unsupported on this platform", ErrUnsafeStoragePath)
}
