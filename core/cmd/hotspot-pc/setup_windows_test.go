package main

import (
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// A folder that somebody made before the installation, with an entry of their own in its access list, ends up
// with the program's list and nothing else.
func TestLockReplacesTheAccessList(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Hotspot")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Afterwards the test's own account may only read the folder: give it back, so that it can be cleaned up.
	t.Cleanup(func() { _ = exec.Command("icacls.exe", dir, "/reset", "/T", "/Q").Run() })
	if out, err := exec.Command("icacls.exe", dir, "/grant", "*S-1-1-0:(OI)(CI)F").CombinedOutput(); err != nil {
		t.Skipf("icacls: %v: %s", err, out)
	}
	if err := lock(dir, true); err != nil {
		t.Fatal(err)
	}
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	got := sd.String()
	for _, want := range []string{"(A;OICI;FA;;;SY)", "(A;OICI;FA;;;BA)", ";;;BU)"} {
		if !strings.Contains(got, want) {
			t.Errorf("%s is missing from %s", want, got)
		}
	}
	if !strings.HasPrefix(got, "D:P") || strings.Contains(got, ";;;WD)") || strings.Count(got, "(") != 3 {
		t.Errorf("the list is not the program's alone: %s", got)
	}

	// The private folder: nobody but SYSTEM and the administrators.
	private := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(private, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = exec.Command("icacls.exe", private, "/reset", "/T", "/Q").Run() })
	if err := lock(private, false); err != nil {
		t.Fatal(err)
	}
	if sd, err = windows.GetNamedSecurityInfo(private, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION); err != nil {
		t.Fatal(err)
	}
	if got := sd.String(); strings.Contains(got, ";;;BU)") || strings.Count(got, "(") != 2 {
		t.Errorf("the private folder's list: %s", got)
	}
}

func TestIsLink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if isLink(target) || isLink(filepath.Join(dir, "missing")) {
		t.Fatal("a plain folder, or nothing, taken for a link")
	}
	link := filepath.Join(dir, "link")
	if out, err := exec.Command("cmd.exe", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("no junction could be made: %v: %s", err, out)
	}
	if !isLink(link) {
		t.Fatal("a junction not taken for a link")
	}
}

// The address of a failed request, token and all, stays out of the error that is shown and logged.
func TestBareErrorHasNoAddress(t *testing.T) {
	inner := errors.New("connection refused")
	err := bare(&url.Error{Op: "Get", URL: "http://10.8.0.1:51823/phone?t=0123456789abcdef0123456789abcdef", Err: inner})
	if err != inner || strings.Contains(err.Error(), "0123") {
		t.Fatalf("got %v", err)
	}
	if bare(inner) != inner {
		t.Fatal("another error was changed")
	}
}

// The result file of a command run with administrator rights: a new file of the one name, never over another.
func TestResultFileIsOnlyEverNew(t *testing.T) {
	dir := t.TempDir()
	yes, f := true, false
	for name, written := range map[string]bool{"hotspot-result-123.txt": true, "settings.json": false, "hotspot-result-.txt": false} {
		path := filepath.Join(dir, name)
		q := quietFlags{elevated: &f, quiet: &yes, result: &path}
		_ = q.finish(errors.New("olmadı"))
		data, err := os.ReadFile(path)
		if written != (err == nil && string(data) == "olmadı") {
			t.Errorf("%s: written %v, want %v", name, err == nil, written)
		}
	}
	kept := filepath.Join(dir, "hotspot-result-7.txt")
	if err := os.WriteFile(kept, []byte("something else"), 0o644); err != nil {
		t.Fatal(err)
	}
	q := quietFlags{elevated: &f, quiet: &yes, result: &kept}
	_ = q.finish(errors.New("olmadı"))
	if data, _ := os.ReadFile(kept); string(data) != "something else" {
		t.Fatalf("a file that was there was written over: %q", data)
	}
}

// ownFolders points the program's folder at a temporary one for a test, and gives the test's account its
// folders back afterwards (they end up open to SYSTEM and the administrators only).
func ownFolders(t *testing.T) {
	t.Helper()
	base := t.TempDir()
	t.Setenv("ProgramData", base)
	t.Cleanup(func() {
		_ = exec.Command("icacls.exe", filepath.Join(base, "Hotspot"), "/reset", "/T", "/C", "/Q").Run()
		_ = exec.Command("icacls.exe", filepath.Join(base, "Hotspot", "private"), "/reset", "/T", "/C", "/Q").Run()
	})
}

// A folder that was there before the installation, with a file somebody opened to everyone and a link that
// leads elsewhere in it: afterwards the folders carry the program's access lists, the file only what it inherits
// from them, and the link is gone (and what it led to is untouched).
func TestMakeFoldersTakesOverWhatWasThere(t *testing.T) {
	ownFolders(t)
	if err := os.MkdirAll(dataDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	planted := filepath.Join(dataDir(), "settings.json")
	if err := os.WriteFile(planted, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("icacls.exe", planted, "/grant", "*S-1-1-0:F").CombinedOutput(); err != nil {
		t.Skipf("icacls: %v: %s", err, out)
	}
	elsewhere := t.TempDir()
	if err := os.WriteFile(filepath.Join(elsewhere, "kept.txt"), []byte("kept"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dataDir(), "log")
	linked := exec.Command("cmd.exe", "/c", "mklink", "/J", link, elsewhere).Run() == nil

	if err := makeFolders(); err != nil {
		t.Fatal(err)
	}
	list := func(path string) string {
		sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		return sd.String()
	}
	if got := list(dataDir()); !strings.HasPrefix(got, "D:P") || strings.Count(got, "(") != 3 {
		t.Errorf("the program's folder: %s", got)
	}
	if got := list(privateDir()); !strings.HasPrefix(got, "D:P") || strings.Count(got, "(") != 2 || strings.Contains(got, ";;;BU)") {
		t.Errorf("the private folder: %s", got)
	}
	if got := list(planted); strings.Contains(got, ";;;WD)") {
		t.Errorf("the file kept the entry that opened it to everyone: %s", got)
	}
	if linked {
		if _, err := os.Lstat(link); !os.IsNotExist(err) {
			t.Errorf("the link is still there: %v", err)
		}
		if data, _ := os.ReadFile(filepath.Join(elsewhere, "kept.txt")); string(data) != "kept" {
			t.Error("what the link led to was touched")
		}
	}
}

// A window left open across two upgrades still runs the file the first one moved aside, which cannot be removed:
// the second upgrade moves the program aside under another name instead of failing.
func TestInstallSelfWhileAnOlderCopyStillRuns(t *testing.T) {
	ownFolders(t)
	if err := os.MkdirAll(dataDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "AleShare.exe")
	write := func(path, text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	installed := func() string {
		data, _ := os.ReadFile(installedExe())
		return string(data)
	}
	write(src, "release 1")
	if err := installSelf(src); err != nil || installed() != "release 1" {
		t.Fatalf("the first install: %v, %q", err, installed())
	}
	write(src, "release 2")
	if err := installSelf(src); err != nil || installed() != "release 2" {
		t.Fatalf("an upgrade: %v, %q", err, installed())
	}
	if left, _ := filepath.Glob(installedExe() + ".old*"); len(left) != 0 {
		t.Fatalf("what was moved aside stayed although nothing runs it: %v", left)
	}
	// Stands in for the program a window still runs: a file that is open cannot be removed.
	write(installedExe()+".old", "release 1, still running")
	held, err := os.Open(installedExe() + ".old")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	write(src, "release 3")
	if err := installSelf(src); err != nil || installed() != "release 3" {
		t.Fatalf("an upgrade while an older copy runs: %v, %q", err, installed())
	}
	if data, _ := os.ReadFile(installedExe() + ".old"); string(data) != "release 1, still running" {
		t.Fatalf("the copy that still runs was touched: %q", data)
	}
	if left, _ := filepath.Glob(installedExe() + ".old?*"); len(left) != 0 {
		t.Fatalf("release 2 stayed behind although nothing runs it: %v", left)
	}
}
