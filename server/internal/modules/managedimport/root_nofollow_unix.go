//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package managedimport

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/sys/unix"
)

func openManagedStorageRoot(path string) (*os.Root, error) {
	flags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC
	descriptor, err := unix.Open(string(filepath.Separator), flags, 0)
	if err != nil {
		return nil, fmt.Errorf("open filesystem root: %w", err)
	}
	for _, component := range strings.Split(strings.TrimPrefix(filepath.Clean(path), string(filepath.Separator)), string(filepath.Separator)) {
		nextDescriptor, openErr := unix.Openat(descriptor, component, flags|unix.O_NOFOLLOW, 0)
		closeErr := unix.Close(descriptor)
		if openErr != nil {
			if errors.Is(openErr, unix.ELOOP) || errors.Is(openErr, unix.ENOTDIR) {
				openErr = fmt.Errorf("%w: Managed Storage root component %q changed or is a symbolic link", ErrUnsafeStoragePath, component)
			}
			return nil, errors.Join(openErr, closeErr)
		}
		if closeErr != nil {
			return nil, errors.Join(closeErr, unix.Close(nextDescriptor))
		}
		descriptor = nextDescriptor
	}
	descriptorRoot := "/dev/fd"
	if runtime.GOOS == "linux" {
		descriptorRoot = "/proc/self/fd"
	}
	root, openErr := os.OpenRoot(filepath.Join(descriptorRoot, fmt.Sprint(descriptor)))
	closeErr := unix.Close(descriptor)
	if openErr != nil {
		return nil, errors.Join(fmt.Errorf("open descriptor-backed Managed Storage root: %w", openErr), closeErr)
	}
	if closeErr != nil {
		return nil, errors.Join(closeErr, root.Close())
	}
	return root, nil
}
