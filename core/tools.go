//go:build tools

// Package tools pins the gomobile runtime support so that `go mod tidy` keeps
// it in go.mod; `gomobile bind` needs it to generate the bindings.
package tools

import _ "golang.org/x/mobile/bind"
