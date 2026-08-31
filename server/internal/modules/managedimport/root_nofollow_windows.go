//go:build windows

package managedimport

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func openManagedStorageRoot(path string) (*os.Root, error) {
	handles, err := lockManagedStoragePath(path)
	if err != nil {
		return nil, err
	}
	root, openErr := os.OpenRoot(path)
	closeErr := closeManagedStorageHandles(handles)
	if openErr != nil {
		return nil, errors.Join(openErr, closeErr)
	}
	if closeErr != nil {
		return nil, errors.Join(closeErr, root.Close())
	}
	return root, nil
}

func lockManagedStoragePath(path string) ([]windows.Handle, error) {
	volume := filepath.VolumeName(path)
	currentPath := volume + string(filepath.Separator)
	components := strings.Split(strings.TrimPrefix(filepath.Clean(path), currentPath), string(filepath.Separator))
	handles := make([]windows.Handle, 0, len(components)+1)
	for index := -1; index < len(components); index++ {
		if index >= 0 {
			currentPath = filepath.Join(currentPath, components[index])
		}
		handle, err := openManagedStorageDirectory(currentPath)
		if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
			err = createManagedStorageDirectory(currentPath)
			if err == nil {
				handle, err = openManagedStorageDirectory(currentPath)
			}
		}
		if err != nil {
			return nil, errors.Join(err, closeManagedStorageHandles(handles))
		}
		handles = append(handles, handle)
	}
	return handles, nil
}

func openManagedStorageDirectory(path string) (windows.Handle, error) {
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return windows.InvalidHandle, fmt.Errorf("encode Managed Storage path component %q: %w", path, err)
	}
	handle, err := windows.CreateFile(pathPointer, windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return windows.InvalidHandle, fmt.Errorf("lock Managed Storage path component %q: %w", path, err)
	}
	var information windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &information); err != nil {
		return windows.InvalidHandle, errors.Join(fmt.Errorf("inspect Managed Storage path component %q: %w", path, err), windows.CloseHandle(handle))
	}
	if information.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return windows.InvalidHandle, errors.Join(fmt.Errorf("%w: Managed Storage root component %q is a reparse point", ErrUnsafeStoragePath, path), windows.CloseHandle(handle))
	}
	if information.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		return windows.InvalidHandle, errors.Join(fmt.Errorf("%w: Managed Storage root component %q is not a directory", ErrUnsafeStoragePath, path), windows.CloseHandle(handle))
	}
	return handle, nil
}

func createManagedStorageDirectory(path string) error {
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("encode Managed Storage path component %q: %w", path, err)
	}
	err = windows.CreateDirectory(pathPointer, nil)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return fmt.Errorf("create Managed Storage root component %q: %w", path, err)
	}
	return nil
}

func closeManagedStorageHandles(handles []windows.Handle) error {
	var closeErr error
	for _, handle := range handles {
		closeErr = errors.Join(closeErr, windows.CloseHandle(handle))
	}
	return closeErr
}
