// Package wgkey generates and encodes WireGuard keys.
package wgkey

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// Key is a 32-byte Curve25519 key or a pre-shared key.
type Key [32]byte

// Generate returns a new, clamped private key.
func Generate() Key {
	k := Random()
	k[0] &= 248
	k[31] = (k[31] & 127) | 64
	return k
}

// Random returns 32 random bytes, suitable as a pre-shared key.
func Random() Key {
	var k Key
	if _, err := rand.Read(k[:]); err != nil {
		panic(err)
	}
	return k
}

// Public derives the public key that belongs to a private key.
func (k Key) Public() Key {
	priv, err := ecdh.X25519().NewPrivateKey(k[:])
	if err != nil {
		panic(err)
	}
	var pub Key
	copy(pub[:], priv.PublicKey().Bytes())
	return pub
}

// IsZero reports whether k is unset.
func (k Key) IsZero() bool { return k == Key{} }

// Hex is the encoding used by the WireGuard userspace configuration protocol.
func (k Key) Hex() string { return hex.EncodeToString(k[:]) }

// Base64 is the encoding used in .conf files.
func (k Key) Base64() string { return base64.StdEncoding.EncodeToString(k[:]) }

// String never reveals the key, so keys cannot leak through logs by accident.
func (k Key) String() string { return "wgkey.Key(redacted)" }

func (k Key) MarshalText() ([]byte, error) { return []byte(k.Base64()), nil }

func (k *Key) UnmarshalText(text []byte) error {
	b, err := base64.StdEncoding.DecodeString(string(text))
	if err != nil {
		return err
	}
	if len(b) != 32 {
		return fmt.Errorf("wgkey: want 32 bytes, got %d", len(b))
	}
	copy(k[:], b)
	return nil
}
