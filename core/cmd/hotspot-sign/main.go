// Command hotspot-sign makes the release key and signs the Windows program of a release (see internal/release).
//
//	hotspot-sign keygen KEYFILE                      a new key; prints its public half for release.PublicKey
//	hotspot-sign sign -key KEYFILE -code N PROGRAM   signs PROGRAM (in place) as release number N
//	hotspot-sign name N                              prints what release number N is called (for the build scripts)
//	hotspot-sign apk FILE                            prints the number and the name of the release an APK is
//	hotspot-sign verify PROGRAM                      checks PROGRAM against release.PublicKey
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"hotspot/core/internal/release"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "hotspot-sign:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("keygen, sign, name, apk or verify")
	}
	switch args[0] {
	case "keygen":
		if len(args) != 2 {
			return errors.New("keygen KEYFILE")
		}
		if _, err := os.Stat(args[1]); err == nil {
			return fmt.Errorf("%s exists; a new key would orphan every computer that trusts the old one", args[1])
		}
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}
		if err := os.WriteFile(args[1], []byte(base64.StdEncoding.EncodeToString(priv.Seed())+"\n"), 0o600); err != nil {
			return err
		}
		fmt.Println(base64.StdEncoding.EncodeToString(pub))
		return nil
	case "name":
		code := 0
		if len(args) == 2 {
			code, _ = strconv.Atoi(args[1])
		}
		if code <= 0 {
			return errors.New("name N")
		}
		fmt.Println(release.Name(code))
		return nil
	case "apk":
		if len(args) != 2 {
			return errors.New("apk FILE")
		}
		code := release.APKCode(args[1])
		if code <= 0 {
			return errors.New("not an APK with a release number")
		}
		fmt.Println(code, release.Name(code))
		return nil
	case "sign":
		fs := flag.NewFlagSet("sign", flag.ContinueOnError)
		keyFile := fs.String("key", "", "the release key")
		code := fs.Int("code", 0, "the release number")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 1 || *keyFile == "" || *code <= 0 {
			return errors.New("sign -key KEYFILE -code N PROGRAM")
		}
		raw, err := os.ReadFile(*keyFile)
		if err != nil {
			return err
		}
		seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
		if err != nil || len(seed) != ed25519.SeedSize {
			return errors.New("not a release key")
		}
		key := ed25519.NewKeyFromSeed(seed)
		if !key.Public().(ed25519.PublicKey).Equal(release.PublicKey) {
			return errors.New("this key is not the one release.PublicKey trusts")
		}
		data, err := os.ReadFile(fs.Arg(0))
		if err != nil {
			return err
		}
		signed := release.Sign(data, release.Info{Code: *code, Name: release.Name(*code)}, key)
		return os.WriteFile(fs.Arg(0), signed, 0o755)
	case "verify":
		if len(args) != 2 {
			return errors.New("verify PROGRAM")
		}
		data, err := os.ReadFile(args[1])
		if err != nil {
			return err
		}
		info, err := release.Verify(data)
		if err != nil {
			return err
		}
		fmt.Printf("%s (release %d): signature good\n", info.Name, info.Code)
		return nil
	}
	return fmt.Errorf("unknown command %q", args[0])
}
