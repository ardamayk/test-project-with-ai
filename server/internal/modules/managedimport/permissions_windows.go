//go:build windows

package managedimport

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func restrictManagedStorageFile(file *os.File) error {
	connection, err := file.SyscallConn()
	if err != nil {
		return fmt.Errorf("access Managed Storage file handle: %w", err)
	}
	var permissionErr error
	if err := connection.Control(func(handle uintptr) {
		permissionErr = restrictManagedStorageHandle(windows.Handle(handle))
	}); err != nil {
		return fmt.Errorf("control Managed Storage file handle: %w", err)
	}
	return permissionErr
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
