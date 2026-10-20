// Package testfs provides synthetic filesystem fixtures for native boundary tests.
// Application code must not import this package.
package testfs

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// Junction creates and checks a real local directory junction. Unsupported
// filesystems or permissions fail the test: they cannot satisfy native evidence.
// The buffer follows Microsoft's Mount Point Reparse Data Buffer specification:
// https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-fscc/ca069dad-ed16-42aa-b057-b6b207f447cc
func Junction(t testing.TB, link, target string) {
	t.Helper()
	abs, err := filepath.Abs(target)
	if err != nil || strings.HasPrefix(abs, `\\`) || len(abs) < 3 || abs[1] != ':' {
		t.Fatalf("junction fixture requires a local absolute target: %q, %v", abs, err)
	}
	substitute, err := windows.UTF16FromString(`\??\` + abs)
	if err != nil {
		t.Fatal(err)
	}
	printName, err := windows.UTF16FromString(abs)
	if err != nil {
		t.Fatal(err)
	}
	// The eight-byte common header precedes eight bytes of path offsets/lengths.
	pathBytes := 2 * (len(substitute) + len(printName))
	if pathBytes+16 > 16<<10 {
		t.Fatal("junction fixture target too long")
	}
	buf := make([]byte, 16+pathBytes)
	binary.LittleEndian.PutUint32(buf, windows.IO_REPARSE_TAG_MOUNT_POINT)
	binary.LittleEndian.PutUint16(buf[4:], uint16(8+pathBytes))
	binary.LittleEndian.PutUint16(buf[10:], uint16(2*(len(substitute)-1)))
	binary.LittleEndian.PutUint16(buf[12:], uint16(2*len(substitute)))
	binary.LittleEndian.PutUint16(buf[14:], uint16(2*(len(printName)-1)))
	for i, c := range append(substitute, printName...) {
		binary.LittleEndian.PutUint16(buf[16+2*i:], c)
	}
	if err := os.Mkdir(link, 0700); err != nil {
		t.Fatal(err)
	}
	h := reparseHandle(t, link, windows.GENERIC_WRITE)
	var returned uint32
	err = windows.DeviceIoControl(h, windows.FSCTL_SET_REPARSE_POINT, &buf[0], uint32(len(buf)), nil, 0, &returned, nil)
	_ = windows.CloseHandle(h)
	if err != nil {
		t.Fatalf("create required native junction %q: %v", link, err)
	}
	RequireTag(t, link, windows.IO_REPARSE_TAG_MOUNT_POINT)
	got, err := filepath.EvalSymlinks(link)
	if err != nil || !strings.EqualFold(filepath.Clean(got), abs) {
		t.Fatalf("junction fixture target = %q, want %q: %v", got, abs, err)
	}
}

// Symlink requires a real symbolic link, including its native tag. These cases
// deliberately fail on unavailable privilege rather than silently skip evidence.
func Symlink(t testing.TB, link, target string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create required native symbolic link %q: %v", link, err)
	}
	RequireTag(t, link, windows.IO_REPARSE_TAG_SYMLINK)
}

func RequireTag(t testing.TB, name string, want uint32) {
	t.Helper()
	h := reparseHandle(t, name, 0)
	defer windows.CloseHandle(h)
	buf := make([]byte, 16<<10)
	var returned uint32
	if err := windows.DeviceIoControl(h, windows.FSCTL_GET_REPARSE_POINT, nil, 0, &buf[0], uint32(len(buf)), &returned, nil); err != nil {
		t.Fatal(err)
	}
	if returned < 8 || binary.LittleEndian.Uint32(buf) != want {
		t.Fatalf("reparse fixture tag = %#x (%d bytes), want %#x", binary.LittleEndian.Uint32(buf), returned, want)
	}
}

func reparseHandle(t testing.TB, name string, access uint32) windows.Handle {
	t.Helper()
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal(err)
	}
	return h
}
