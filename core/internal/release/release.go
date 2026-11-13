// Package release signs the Windows program of each release, so that a computer installs a program it got from a
// phone only when it was built by this project's build (Android checks an APK's signature by itself).
//
// The signature rides at the end of the .exe, where Windows ignores it (bytes after the program's last section):
//
//	program | descriptor (JSON) | descriptor length (uint32, little endian) | "HSREL1\x00\x00"
//
// The descriptor holds the release (code and name), the program's SHA-256 and an Ed25519 signature over
// "hotspot-pc|<code>|<name>|<sha256>". The private key stays on the computer that builds the releases.
package release

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// PublicKey checks the releases. Its private half is android/keystore/release-ed25519.key (not in the repository).
var PublicKey = mustKey("aDFL9DtzrR1E9CE3z3yVPLZPtkPWGziZVVzGSlA50wU=")

var magic = []byte("HSREL1\x00\x00")

// firstPublished is the first release that was published. The ones before it were called "1.0.<code>"; it is "1.1",
// and the ones after it count on from there: "1.1.1" is the next one, then "1.1.2".
const firstPublished = 39

// Name is what the release with the number code is called.
func Name(code int) string {
	switch {
	case code < firstPublished:
		return fmt.Sprintf("1.0.%d", code)
	case code == firstPublished:
		return "1.1"
	}
	return fmt.Sprintf("1.1.%d", code-firstPublished)
}

// Code is the number of the release that is called name, 0 when no release is.
func Code(name string) int {
	var n int
	switch {
	case name == "1.1":
		return firstPublished
	case strings.HasPrefix(name, "1.1."):
		n, _ = strconv.Atoi(name[len("1.1."):])
		n += firstPublished
	case strings.HasPrefix(name, "1.0."):
		n, _ = strconv.Atoi(name[len("1.0."):])
	}
	if n <= 0 || Name(n) != name { // also refuses "1.0.39", "1.1.0" and "1.1.07"
		return 0
	}
	return n
}

// Info is the release a signed program belongs to.
type Info struct {
	Code int    `json:"code"` // the release number, the same as the APK's versionCode
	Name string `json:"name"` // what the release is called: Name(Code)
}

type descriptor struct {
	Info
	SHA256 string `json:"sha256"`
	Sig    []byte `json:"sig"`
}

func message(code int, name, sum string) []byte {
	return []byte(fmt.Sprintf("hotspot-pc|%d|%s|%s", code, name, sum))
}

// Sign returns the program with its signature appended. A signature already there is replaced.
func Sign(program []byte, info Info, key ed25519.PrivateKey) []byte {
	program = Strip(program)
	sum := sha256.Sum256(program)
	d := descriptor{Info: info, SHA256: hex.EncodeToString(sum[:])}
	d.Sig = ed25519.Sign(key, message(info.Code, info.Name, d.SHA256))
	tail, _ := json.Marshal(d)
	out := make([]byte, 0, len(program)+len(tail)+4+len(magic))
	out = append(out, program...)
	out = append(out, tail...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(tail)))
	return append(out, magic...)
}

// Verify checks a signed program against PublicKey and returns its release.
func Verify(data []byte) (Info, error) { return VerifyWith(data, PublicKey) }

// VerifyWith is Verify with another key (tests sign with their own).
func VerifyWith(data []byte, key ed25519.PublicKey) (Info, error) {
	program, d, err := split(data)
	if err != nil {
		return Info{}, err
	}
	sum := sha256.Sum256(program)
	if hex.EncodeToString(sum[:]) != d.SHA256 {
		return Info{}, errors.New("release: the program does not match its signature")
	}
	if !ed25519.Verify(key, message(d.Code, d.Name, d.SHA256), d.Sig) {
		return Info{}, errors.New("release: the signature is not this project's")
	}
	return d.Info, nil
}

// Read returns the release a program says it belongs to, without checking the signature: for showing it.
func Read(data []byte) (Info, bool) {
	_, d, err := split(data)
	return d.Info, err == nil
}

// Strip returns the program without its signature (the program itself when it has none).
func Strip(data []byte) []byte {
	if program, _, err := split(data); err == nil {
		return program
	}
	return data
}

func split(data []byte) ([]byte, descriptor, error) {
	var d descriptor
	if len(data) < len(magic)+4 || !bytes.Equal(data[len(data)-len(magic):], magic) {
		return nil, d, errors.New("release: not signed")
	}
	n := int(binary.LittleEndian.Uint32(data[len(data)-len(magic)-4:]))
	end := len(data) - len(magic) - 4
	if n <= 0 || n > 4096 || n > end {
		return nil, d, errors.New("release: broken signature")
	}
	if err := json.Unmarshal(data[end-n:end], &d); err != nil || d.Code <= 0 {
		return nil, d, errors.New("release: broken signature")
	}
	return data[:end-n], d, nil
}

func mustKey(s string) ed25519.PublicKey {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(b) != ed25519.PublicKeySize {
		panic("release: bad public key")
	}
	return b
}
