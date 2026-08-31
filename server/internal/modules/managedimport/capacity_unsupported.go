//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows && !zos

package managedimport

import (
	"errors"
)

func availableStorageBytes(string) (int64, error) {
	return 0, errors.New("managed storage capacity inspection is unsupported on this platform")
}
