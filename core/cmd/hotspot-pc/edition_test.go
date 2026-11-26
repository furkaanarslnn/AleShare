package main

import (
	"os"
	"testing"
)

// The download installs the program it carries, never itself; the program installed installs itself as before.
func TestTheDownloadInstallsTheProgramItCarries(t *testing.T) {
	t.Setenv("ProgramData", t.TempDir())
	if err := os.MkdirAll(dataDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	old := installedEdition
	defer func() { installedEdition = old }()

	installedEdition = nil
	if path, done, err := programToInstall(`C:\x\AleShare.exe`); err != nil || path != `C:\x\AleShare.exe` {
		t.Fatalf("without a carried program: %q %v", path, err)
	} else {
		done()
	}

	installedEdition = []byte("MZ the program")
	path, done, err := programToInstall(`C:\x\AleShare.exe`)
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "MZ the program" {
		t.Fatalf("wrote %q", data)
	}
	done()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("the written program stayed")
	}

	// Installed from this download, or this download installed by the service: nothing new to install either way.
	if !sameProgram([]byte("MZ download"), []byte("MZ the program")) || !sameProgram([]byte("MZ download"), []byte("MZ download")) {
		t.Fatal("the same program taken for another")
	}
	if sameProgram([]byte("MZ download"), []byte("MZ older")) {
		t.Fatal("another program taken for the same")
	}
}
