package pairing

import "hotspot/core/internal/wgkey"

// ProofForTest is the proof a computer of code makes for its key, nonce and name.
func ProofForTest(code QRCode, client wgkey.Key, nonce []byte, name string) []byte {
	return clientProof(code.Secret[:], client, nonce, code.Named, name)
}
