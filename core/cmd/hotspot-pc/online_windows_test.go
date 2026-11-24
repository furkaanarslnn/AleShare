package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A stand-in for GitHub: update.json and the program it names. The program carries no release signature (only the
// build's key can make one), so the test ends where a real release would go on to be installed.
func TestOnlineLookAndDownload(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	program := []byte("MZ this is not a signed release")
	sum := sha256.Sum256(program)
	served := program
	var manifest string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/update.json":
			fmt.Fprint(w, manifest)
		case "/files/AleShare.exe":
			w.Write(served)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	oldManifest, oldBase, oldVersion := manifestURL, downloadBase, version
	manifestURL, downloadBase, version = srv.URL+"/update.json", srv.URL+"/files/", "1.1"
	defer func() { manifestURL, downloadBase, version = oldManifest, oldBase, oldVersion }()
	file := func(name string) string {
		return fmt.Sprintf(`{"name":%q,"url":%q,"size":%d,"sha256":%q}`, name, srv.URL+"/files/"+name, len(program), hex.EncodeToString(sum[:]))
	}
	manifest = `{"code":40,"name":"1.1.1","apk":` + file("AleShare.apk") + `,"pc":` + file("AleShare.exe") + `}`

	s := checkOnline(true)
	rel := s.offer(39)
	if rel == nil || rel.Name != "1.1.1" || s.Checked.IsZero() {
		t.Fatalf("the look found %+v", s)
	}
	if again := loadOnline(); again.Release == nil || again.Release.Code != 40 {
		t.Fatal("what was found must be kept for the window and the icon")
	}

	// The right bytes, but not a release of this project: refused, and nothing is left lying around.
	_, err := fetchOnlineProgram(*rel)
	if err == nil || !strings.Contains(err.Error(), "imza") {
		t.Fatalf("an unsigned program must be refused for its signature, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(onlineDir(), "update", "AleShare.exe")); !os.IsNotExist(err) {
		t.Fatal("the refused file must be removed")
	}

	// Other bytes than update.json promised: refused before the signature is even looked at.
	served = []byte("MZ this is another file of the same size")[:len(program)]
	if _, err := fetchOnlineProgram(*rel); err == nil || !strings.Contains(err.Error(), "bozuk") {
		t.Fatalf("a file that does not match its checksum must be refused, got %v", err)
	}

	// GitHub does not answer: what was known stays, and the next try is put off.
	manifest = "<html>oops</html>"
	if after := checkOnline(true); after.offer(39) == nil || !after.Tried.After(after.Checked) && !after.Tried.Equal(after.Checked) {
		t.Fatalf("a failed look must keep the release known before: %+v", after)
	}
}
