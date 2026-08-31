//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris || zos

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
		nextDescriptor, openErr := openManagedStorageDirectoryAt(descriptor, component, flags)
		closeErr := closeManagedStorageDescriptor(descriptor)
		if openErr != nil {
			return nil, errors.Join(openErr, closeErr)
		}
		if closeErr != nil {
			return nil, errors.Join(closeErr, closeManagedStorageDescriptor(nextDescriptor))
		}
		descriptor = nextDescriptor
	}
	descriptorRoot := "/dev/fd"
	if runtime.GOOS == "linux" {
		descriptorRoot = "/proc/self/fd"
	}
	root, openErr := os.OpenRoot(filepath.Join(descriptorRoot, fmt.Sprint(descriptor)))
	closeErr := closeManagedStorageDescriptor(descriptor)
	if openErr != nil {
		return nil, errors.Join(fmt.Errorf("open descriptor-backed Managed Storage root: %w", openErr), closeErr)
	}
	if closeErr != nil {
		return nil, errors.Join(closeErr, closeManagedStorageRoot(root))
	}
	return root, nil
}

func closeManagedStorageDescriptor(descriptor int) error {
	if err := unix.Close(descriptor); err != nil {
		return fmt.Errorf("close Managed Storage directory descriptor %d: %w", descriptor, err)
	}
	return nil
}

func openManagedStorageDirectoryAt(parentDescriptor int, component string, flags int) (int, error) {
	descriptor, err := unix.Openat(parentDescriptor, component, flags|unix.O_NOFOLLOW, 0)
	if errors.Is(err, unix.ENOENT) {
		mkdirErr := unix.Mkdirat(parentDescriptor, component, 0o700)
		if mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
			return -1, fmt.Errorf("create Managed Storage root component %q: %w", component, mkdirErr)
		}
		descriptor, err = unix.Openat(parentDescriptor, component, flags|unix.O_NOFOLLOW, 0)
	}
	if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
		return -1, fmt.Errorf("%w: Managed Storage root component %q changed or is a symbolic link", ErrUnsafeStoragePath, component)
	}
	if err != nil {
		return -1, fmt.Errorf("open Managed Storage root component %q: %w", component, err)
	}
	return descriptor, nil
}
