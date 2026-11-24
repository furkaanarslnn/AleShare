package release

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

func TestSignAndVerify(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	program := append([]byte("MZ\x90\x00"), bytes.Repeat([]byte{7}, 5000)...)
	signed := Sign(program, Info{Code: 12, Name: "1.0.12"}, priv)

	info, err := VerifyWith(signed, pub)
	if err != nil || info != (Info{12, "1.0.12"}) {
		t.Fatalf("%+v, %v", info, err)
	}
	if !bytes.Equal(Strip(signed), program) || !bytes.Equal(Strip(program), program) {
		t.Fatal("Strip does not give the program back")
	}
	if got, ok := Read(signed); !ok || got.Code != 12 {
		t.Fatalf("Read: %+v %v", got, ok)
	}
	// Signing again replaces the signature instead of stacking another.
	again := Sign(signed, Info{Code: 13, Name: "1.0.13"}, priv)
	if info, err := VerifyWith(again, pub); err != nil || info.Code != 13 || len(again) > len(signed)+2 {
		t.Fatalf("re-signed: %+v %v (%d vs %d bytes)", info, err, len(again), len(signed))
	}
}

func TestVerifyRefuses(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	program := []byte("MZ a program")
	signed := Sign(program, Info{Code: 12, Name: "1.0.12"}, priv)

	tampered := bytes.Clone(signed)
	tampered[3] ^= 1
	cases := map[string][]byte{
		"unsigned":            program,
		"another key":         Sign(program, Info{Code: 12, Name: "1.0.12"}, other),
		"program changed":     tampered,
		"truncated":           signed[:len(signed)-3],
		"empty":               nil,
		"release made bigger": bytes.Replace(signed, []byte(`"code":12`), []byte(`"code":99`), 1),
	}
	for name, data := range cases {
		if _, err := VerifyWith(data, pub); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// What update.json calls a release for the apps of 1.1, which knew only their own rule.
func TestManifestName(t *testing.T) {
	for code, name := range map[int]string{38: "1.0.38", 39: "1.1", 40: "1.1.1", 41: "1.1.2", 42: "1.1.3"} {
		if got := ManifestName(code); got != name {
			t.Errorf("ManifestName(%d) = %q, want %q", code, got, name)
		}
	}
}

// The key in the code is a real Ed25519 public key (a typo would make every release unverifiable).
func TestPublicKey(t *testing.T) {
	if len(PublicKey) != ed25519.PublicKeySize {
		t.Fatal("bad key")
	}
}

// A release's name follows from its number: "1.0.N" before the first published one, then the names the owner gave
// ("1.1", "1.2"), and counting on from the last of them.
func TestNamesAndNumbers(t *testing.T) {
	for code, name := range map[int]string{1: "1.0.1", 38: "1.0.38", 39: "1.1", 40: "1.2", 41: "1.2.1", 51: "1.2.11"} {
		if Name(code) != name || Code(name) != code {
			t.Errorf("release %d: Name = %q, Code(%q) = %d", code, Name(code), name, Code(name))
		}
	}
	for _, name := range []string{"", "1.0.0", "1.0.39", "1.1.0", "1.1.1", "1.2.0", "1.2.07", "1.2.x", "1.3", "2.0", "v1.2", "1.2 "} {
		if got := Code(name); got != 0 {
			t.Errorf("Code(%q) = %d, but no release is called that", name, got)
		}
	}
}
