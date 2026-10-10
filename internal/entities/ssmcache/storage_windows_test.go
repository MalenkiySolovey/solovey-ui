package ssmcache

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestPrivateWindowsDirectoryHandle(t *testing.T) {
	name := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(name, 0700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(name)
	if err != nil {
		t.Fatal("open directory", err)
	}
	defer f.Close()
	descriptor, e := windows.GetSecurityInfo(windows.Handle(f.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if e != nil {
		t.Fatal("query opened directory", e)
	}
	owner, _, e := descriptor.Owner()
	if e != nil {
		t.Fatal(e)
	}
	user, e := windows.GetCurrentProcessToken().GetTokenUser()
	if e != nil {
		t.Fatal(e)
	}
	t.Log("owner_is_user", owner.String() == user.User.Sid.String(), "owner_is_admins", owner.String() == "S-1-5-32-544")
	if err = secureCreated(f); err != nil {
		t.Fatal("secure directory handle", err)
	}
	if err = checkPrivate(f); err != nil {
		t.Fatal("check directory handle", err)
	}
}

func TestWindowsSDDLAliasesPreserveExactTrusteeAuthority(t *testing.T) {
	for _, alias := range []string{"LA", "SY", "BA", "WD", "BU"} {
		descriptor, err := windows.SecurityDescriptorFromString("O:" + alias)
		if err != nil {
			t.Fatal("resolve local SDDL trustee", err)
		}
		owner, _, err := descriptor.Owner()
		if err != nil || owner == nil {
			t.Fatal("resolved trustee missing")
		}
		exact := map[string]bool{owner.String(): true}
		if !trustedSDDLTrustee(alias, exact) || !trustedSDDLTrustee(owner.String(), exact) {
			t.Fatal("OS alias differs from its exact SID")
		}
		if trustedSDDLTrustee(alias, map[string]bool{}) {
			t.Fatal("alias grants independent authority")
		}
	}
	trusted := map[string]bool{"S-1-5-18": true, "S-1-5-32-544": true}
	for _, value := range []string{"WD", "BU", "ZZ", "S-1-1-0", "SY)D:(A;;FA;;;WD"} {
		if trustedSDDLTrustee(value, trusted) {
			t.Fatal("untrusted or malformed trustee accepted")
		}
	}
}

func TestCacheRejectsPublicWindowsACLWithoutChangingPreimage(t *testing.T) {
	s := testStore(t, "state.json")
	if err := s.Write(context.Background(), []byte("{}")); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(Root(), "state.json")
	// This deliberately broad ACL is applied only to a newly created test file.
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err = windows.SetNamedSecurityInfo(name, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	before, err := windows.GetNamedSecurityInfo(name, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = New(name); err == nil {
		t.Fatal("public file ACL accepted")
	}
	if _, err = s.Read(context.Background()); err == nil {
		t.Fatal("public read accepted")
	}
	if err = s.Write(context.Background(), []byte("{}")); err == nil {
		t.Fatal("public preimage overwritten")
	}
	after, err := windows.GetNamedSecurityInfo(name, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil || before.String() != after.String() {
		t.Fatal("existing ACL changed")
	}
	data, _ := os.ReadFile(name)
	if string(data) != "{}" {
		t.Fatal("preimage changed")
	}
	if NamespaceKey(name) != NamespaceKey(strings.ToUpper(name)) {
		t.Fatal("case-insensitive owners may collide")
	}
	seed := filepath.Join(Root(), "restored", "fixture", "generation", "state.seed")
	if NamespaceKey(seed) != NamespaceKey(strings.ToUpper(seed)) {
		t.Fatal("case-insensitive restore seed ownership differs")
	}
}
