//go:build installer

package main

import _ "embed"

// installedEdition is the program this file installs: the same program, built on its own (see edition.go).
//
//go:embed embedded/program.exe
var installedEdition []byte
