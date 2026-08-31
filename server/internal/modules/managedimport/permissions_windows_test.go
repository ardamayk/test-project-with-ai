//go:build windows

package managedimport

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestRestrictManagedStorageFileProtectsSinglePrincipalACL(t *testing.T) {
	rootPath := t.TempDir()
	file, err := os.CreateTemp(rootPath, "managed-storage-acl-")
	if err != nil {
		t.Fatalf("create Managed Storage ACL fixture: %v", err)
	}
	filePath := file.Name()
	if err := file.Close(); err != nil {
		t.Fatalf("close Managed Storage ACL fixture: %v", err)
	}
	if err := restrictManagedStoragePath(rootPath, filepath.Base(filePath), false); err != nil {
		t.Fatalf("restrict Managed Storage fixture: %v", err)
	}
	descriptor, err := windows.GetNamedSecurityInfo(filePath, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
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
