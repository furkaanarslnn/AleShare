package release

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// compiledManifest is a compiled AndroidManifest.xml reduced to what matters: no strings, the attribute names'
// resource numbers, a namespace, and <manifest> with a name-like attribute and android:versionCode.
func compiledManifest(code uint32) []byte {
	le := binary.LittleEndian
	chunk := func(kind, header uint16, body ...uint32) []byte {
		b := make([]byte, 8, 8+4*len(body))
		le.PutUint16(b, kind)
		le.PutUint16(b[2:], header)
		for _, v := range body {
			b = le.AppendUint32(b, v)
		}
		le.PutUint32(b[4:], uint32(len(b)))
		return b
	}
	none := uint32(0xFFFFFFFF)
	attr := func(name, kind, value uint32) []uint32 { // namespace, name, raw value, size 8 | zero | type, value
		return []uint32{none, name, none, 8 | kind<<24, value}
	}
	element := []uint32{1, none, none, 0, 20 | 20<<16, 2 /* attributes */, 0} // line, comment, ns, name, start|size, count|id, class|style
	element = append(element, attr(0, 0x03, 7)...)
	element = append(element, attr(1, 0x10, code)...)
	var body []byte
	body = append(body, chunk(0x0001, 28, 0, 0, 0, 0, 0)...)              // no strings
	body = append(body, chunk(0x0180, 8, 0x01010003, versionCodeAttr)...) // what the two names are
	body = append(body, chunk(0x0100, 16, 1, none, none, none)...)        // a namespace starts
	body = append(body, chunk(0x0102, 16, element...)...)
	file := chunk(0x0003, 8)
	le.PutUint32(file[4:], uint32(8+len(body)))
	return append(file, body...)
}

func TestAPKCode(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, files map[string][]byte) string {
		var buf bytes.Buffer
		z := zip.NewWriter(&buf)
		for n, data := range files {
			w, _ := z.Create(n)
			w.Write(data)
		}
		z.Close()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	manifest := compiledManifest(39)
	if got := APKCode(write("AleShare.apk", map[string][]byte{"classes.dex": []byte("dex"), "AndroidManifest.xml": manifest})); got != 39 {
		t.Fatalf("APKCode = %d, want 39", got)
	}
	if got := APKCode(write("empty.apk", map[string][]byte{"classes.dex": []byte("dex")})); got != 0 {
		t.Fatalf("an APK without a manifest is release %d", got)
	}
	notZip := filepath.Join(dir, "text.apk")
	os.WriteFile(notZip, []byte("not an archive"), 0o644)
	if got := APKCode(notZip); got != 0 {
		t.Fatalf("a file that is no archive is release %d", got)
	}
	for cut := 0; cut < len(manifest); cut += 7 { // a broken manifest must not crash the reader
		_ = manifestVersionCode(manifest[:cut])
	}
	if got := manifestVersionCode(manifest[:len(manifest)-4]); got != 0 {
		t.Fatalf("a cut manifest is release %d", got)
	}
}
