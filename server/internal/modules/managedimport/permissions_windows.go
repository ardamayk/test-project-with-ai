//go:build windows

package managedimport

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func restrictManagedStoragePath(rootPath, relativePath string, isDirectory bool) error {
	handles, err := lockManagedStoragePath(rootPath)
	if err != nil {
		return err
	}
	currentPath := rootPath
	components := strings.Split(filepath.Clean(relativePath), string(filepath.Separator))
	for index, component := range components {
		currentPath = filepath.Join(currentPath, component)
		isFinal := index == len(components)-1
		handle, openErr := openManagedStorageObject(currentPath, isFinal, isFinal && isDirectory)
		if openErr != nil {
			return errors.Join(openErr, closeManagedStorageHandles(handles))
		}
		handles = append(handles, handle)
	}
	permissionErr := restrictManagedStorageHandle(handles[len(handles)-1])
	return errors.Join(permissionErr, closeManagedStorageHandles(handles))
}

func restrictManagedStorageHandle(handle windows.Handle) error {
	tokenUser, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fmt.Errorf("resolve Managed Storage owner: %w", err)
	}
	access := []windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.ACCESS_MASK(windows.GENERIC_ALL),
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(tokenUser.User.Sid),
		},
	}}
	accessControlList, err := windows.ACLFromEntries(access, nil)
	if err != nil {
		return fmt.Errorf("build restrictive Managed Storage ACL: %w", err)
	}
	securityInformation := windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION)
	if err := windows.SetSecurityInfo(handle, windows.SE_FILE_OBJECT, securityInformation, nil, nil, accessControlList, nil); err != nil {
		return fmt.Errorf("apply restrictive Managed Storage ACL: %w", err)
	}
	return nil
}
