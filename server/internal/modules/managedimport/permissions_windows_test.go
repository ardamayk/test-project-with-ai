//go:build windows

package managedimport

import (
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

func TestRestrictManagedStorageFileProtectsSinglePrincipalACL(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "managed-storage-acl-")
	if err != nil {
		t.Fatalf("create Managed Storage ACL fixture: %v", err)
	}
	defer file.Close()
	if err := restrictManagedStorageFile(file); err != nil {
		t.Fatalf("restrict Managed Storage fixture: %v", err)
	}
	descriptor, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("read Managed Storage fixture ACL: %v", err)
	}
	control, _, err := descriptor.Control()
	if err != nil {
		t.Fatalf("read Managed Storage fixture ACL control: %v", err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatal("Managed Storage fixture DACL inherits permissions")
	}
	accessControlList, _, err := descriptor.DACL()
	if err != nil {
		t.Fatalf("read Managed Storage fixture DACL: %v", err)
	}
	if accessControlList == nil || accessControlList.AceCount != 1 {
		t.Fatalf("Managed Storage fixture DACL ACE count = %v, want 1", accessControlList)
	}
}
