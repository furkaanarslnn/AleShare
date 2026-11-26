package main

import (
	"os"
	"path/filepath"
)

// The file people download (AleShare.exe) is the program with the program inside it: the build makes the program
// first, signs it, and builds the download around it (build tag "installer"). Setting up and upgrading from the
// download then writes that inner program into place, never the running file itself. A program that copies its own
// file into ProgramData and makes the copy a service is what Kaspersky's System Watcher takes for a trojan settling
// in ("PDM:Trojan.Win32.Generic", 26 November 2026): it stopped AleShare's setup there every time, and deleted the
// file. Writing another program and making that a service it lets through (tried on this computer the same day,
// with a test program: once its own copy, stopped; once a program it carried, through, also when that program was
// built from the same code). The program that is installed carries nothing (installedEdition is nil there).

// programToInstall gives the file setup and upgrade install when this file is self: the program it carries, written
// beside the installation, or self when it carries none. done removes what was written.
func programToInstall(self string) (path string, done func(), err error) {
	if len(installedEdition) == 0 {
		return self, func() {}, nil
	}
	path = filepath.Join(dataDir(), "hotspot-pc.edition")
	if err := os.WriteFile(path, installedEdition, 0o755); err != nil {
		return "", nil, err
	}
	return path, func() { _ = os.Remove(path) }, nil
}

// sameProgram reports whether the program at path is what this file would install: this very file, or the program
// it carries.
func sameProgram(self []byte, installed []byte) bool {
	return string(self) == string(installed) || (len(installedEdition) > 0 && string(installedEdition) == string(installed))
}
