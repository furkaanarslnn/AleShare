// Command notices writes the license texts of everything the published programs are built from:
//
//	core/cmd/hotspot-pc/ui/licenses.txt        what AleShare.exe carries (it prints them with "licenses")
//	android/app/src/main/assets/licenses.txt   what the APK carries, as a file inside it
//
// Run it from core/ whenever a dependency changes:
//
//	go run ./cmd/notices
//
// The texts are not written by hand. The Go modules' come from their own license files in the module cache, Go's
// from GOROOT, the Android libraries' from the notices Google ships inside its libraries (the Gradle cache, so the
// app must have been built once), and the few that come from nowhere else from static/.
package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// rule frames the names of a section; no license text may hold a line like it (see write).
var rule = strings.Repeat("=", 80)

const intro = `AleShare - açık kaynak lisansları

AleShare aşağıdaki açık kaynak bileşenlerle yapılmıştır. Her bileşen kendi lisansıyla dağıtılır; lisans metinleri
aşağıdadır. AleShare'in kendi lisansı: https://github.com/furkaanarslnn/AleShare/blob/main/LICENSE

WireGuard, Jason A. Donenfeld'in tescilli markasıdır. AleShare, WireGuard projesiyle bağlantılı değildir.
`

const introAndroid = `
QR kod tarayıcı Google Play services ve ML Kit kitaplıklarını kullanır; bunlar Google'ın kendi koşullarıyla
dağıtılır, içlerindeki açık kaynak bileşenlerin lisansları da aşağıdadır.
`

// notices gathers sections: components that share a text, word for word, share a section.
type notices struct {
	order []string            // texts, in the order they were first seen
	names map[string][]string // text -> the components under it
}

func (n *notices) add(name, text string) {
	text = clean(text)
	if text == "" {
		fail("%s: no license text", name)
	}
	if n.names == nil {
		n.names = map[string][]string{}
	}
	if _, ok := n.names[text]; !ok {
		n.order = append(n.order, text)
	}
	for _, have := range n.names[text] {
		if have == name {
			return
		}
	}
	n.names[text] = append(n.names[text], name)
}

// clean gives a text LF line ends and no space at the ends of its lines, so that the same license from two
// places is seen as the same.
func clean(text string) string {
	text = strings.ReplaceAll(strings.TrimPrefix(text, string(rune(0xFEFF))), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t\r")
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n")
}

func (n *notices) write(path, head string) {
	var b bytes.Buffer
	b.WriteString(head)
	for _, text := range n.order {
		for _, line := range strings.Split(text, "\n") {
			if line == rule {
				fail("a license text holds the line that frames sections; change rule")
			}
		}
		fmt.Fprintf(&b, "\n%s\n%s\n%s\n\n%s\n", rule, strings.Join(n.names[text], "\n"), rule, text)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fail("%v", err)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		fail("%v", err)
	}
	fmt.Printf("%s: %d sections, %d KB\n", path, len(n.order), b.Len()/1024)
}

var licenseFile = regexp.MustCompile(`(?i)^(licen[sc]e|copying|notice)(\.|$)`)

// goModules adds the modules that the packages are built from for the given system, each with the license files
// at its root, and Go itself.
func (n *notices) goModules(goos, goarch string, packages ...string) {
	args := append([]string{"list", "-deps", "-f", "{{with .Module}}{{if not .Main}}{{.Path}}\t{{.Version}}\t{{.Dir}}{{end}}{{end}}"}, packages...)
	cmd := exec.Command("go", args...)
	cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=1")
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		fail("go list: %v", err)
	}
	seen := map[string]bool{}
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(string(out), "\r", ""), "\n") {
		if l != "" && !seen[l] {
			seen[l] = true
			lines = append(lines, l)
		}
	}
	sort.Strings(lines)
	for _, l := range lines {
		f := strings.Split(l, "\t")
		path, version, dir := f[0], f[1], f[2]
		entries, err := os.ReadDir(dir)
		if err != nil {
			fail("%s: %v (run go mod download)", path, err)
		}
		found := false
		for _, e := range entries {
			if !e.IsDir() && licenseFile.MatchString(e.Name()) {
				n.add(path+" "+version, read(filepath.Join(dir, e.Name())))
				found = true
			}
		}
		if !found {
			fail("%s: no license file in %s", path, dir)
		}
		if path == "github.com/jchv/go-webview2" { // carries Microsoft's loader library inside it
			n.add("Microsoft Edge WebView2 Loader (WebView2Loader.dll, "+path+" içinde)", read(filepath.Join(dir, "webviewloader", "sdk", "LICENSE.txt")))
		}
	}
	root, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		fail("go env: %v", err)
	}
	version, _ := exec.Command("go", "env", "GOVERSION").Output()
	n.add("Go "+strings.TrimPrefix(strings.TrimSpace(string(version)), "go")+" (derleyici ve standart kitaplık)", read(filepath.Join(strings.TrimSpace(string(root)), "LICENSE")))
}

// The Android libraries the app is built with. The first group is licensed under Apache 2.0 and ships no license
// file of its own; the second carries, as files inside each library, the notices of what Google built it from.
var (
	apacheLibraries = []string{
		"AndroidX ve Jetpack Compose kitaplıkları (The Android Open Source Project)",
		"Kotlin standart kitaplığı (JetBrains s.r.o. and Kotlin Programming Language contributors)",
		"kotlinx.coroutines (JetBrains s.r.o.)",
		"ZXing core (ZXing authors)",
		"Firebase Components ve Firebase Encoders (Google LLC)",
		"Google DataTransport (Google LLC)",
		"Dagger (The Dagger Authors)",
		"javax.inject (The JSR-330 Expert Group)",
	}
	googleLibraries = []string{
		"com.google.android.gms/play-services-base",
		"com.google.android.gms/play-services-basement",
		"com.google.android.gms/play-services-code-scanner",
		"com.google.android.gms/play-services-tasks",
		"com.google.mlkit/barcode-scanning-common",
		"com.google.mlkit/common",
		"com.google.mlkit/vision-common",
		"com.google.android.datatransport/transport-api",
		"com.google.android.datatransport/transport-backend-cct",
		"com.google.android.datatransport/transport-runtime",
		"com.google.firebase/firebase-components",
		"com.google.firebase/firebase-encoders-json",
	}
)

// androidLibraries adds what the Kotlin side of the app is built from.
func (n *notices) androidLibraries(apache string) {
	for _, name := range apacheLibraries {
		n.add(name, apache)
	}
	n.add("ZXing - NOTICE", read(filepath.Join("cmd", "notices", "static", "zxing-NOTICE.txt")))

	home, err := os.UserHomeDir()
	if err != nil {
		fail("%v", err)
	}
	cache := filepath.Join(home, ".gradle", "caches", "modules-2", "files-2.1")
	if dir := os.Getenv("GRADLE_USER_HOME"); dir != "" {
		cache = filepath.Join(dir, "caches", "modules-2", "files-2.1")
	}
	for _, lib := range googleLibraries {
		found, _ := filepath.Glob(filepath.Join(cache, filepath.FromSlash(lib), "*", "*", "*.aar"))
		if len(found) == 0 {
			fail("%s: not in the Gradle cache (%s); build the app once", lib, cache)
		}
		sort.Strings(found)
		n.thirdParty(found[len(found)-1]) // the newest release of it that was ever fetched
	}
}

// thirdParty adds the notices inside one of Google's libraries: third_party_licenses.txt holds the texts end to
// end, third_party_licenses.json says where each component's text starts and how long it is.
func (n *notices) thirdParty(aar string) {
	z, err := zip.OpenReader(aar)
	if err != nil {
		fail("%s: %v", aar, err)
	}
	defer z.Close()
	file := func(name string) []byte {
		for _, f := range z.File {
			if f.Name == name {
				r, err := f.Open()
				if err != nil {
					fail("%s: %v", aar, err)
				}
				defer r.Close()
				b, err := io.ReadAll(r)
				if err != nil {
					fail("%s: %v", aar, err)
				}
				return b
			}
		}
		return nil
	}
	index, texts := file("third_party_licenses.json"), file("third_party_licenses.txt")
	if len(bytes.TrimSpace(index)) == 0 {
		return
	}
	var where map[string]struct{ Start, Length int }
	if err := json.Unmarshal(index, &where); err != nil {
		fail("%s: %v", aar, err)
	}
	names := make([]string, 0, len(where))
	for name := range where {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		w := where[name]
		if w.Start < 0 || w.Length <= 0 || w.Start+w.Length > len(texts) {
			fail("%s: %s lies outside the texts", aar, name)
		}
		n.add(name, string(texts[w.Start:w.Start+w.Length]))
	}
}

func read(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		fail("%v", err)
	}
	return string(b)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "notices: "+format+"\n", args...)
	os.Exit(1)
}

func main() {
	if _, err := os.Stat("go.mod"); err != nil {
		fail("run from core/: go run ./cmd/notices")
	}
	hosts := read(filepath.Join("cmd", "notices", "static", "hosts-LICENSE.txt"))

	var pc notices
	pc.goModules("windows", "amd64", "./cmd/hotspot-pc")
	pc.write(filepath.Join("cmd", "hotspot-pc", "ui", "licenses.txt"), intro)

	var android notices
	android.goModules("android", "arm64", "./wgcore", "golang.org/x/mobile/bind/java", "golang.org/x/mobile/bind/seq")
	android.add("StevenBlack/hosts (reklam ve izleyici listesi)", hosts)
	apache := ""
	for _, text := range android.order { // the Apache 2.0 text as a module ships it
		if strings.Contains(text, "Apache License") && strings.Contains(text, "Version 2.0, January 2004") && strings.HasSuffix(text, "limitations under the License.") {
			apache = text
			break
		}
	}
	if apache == "" {
		fail("no module carries the plain Apache 2.0 text any more; keep a copy in static/")
	}
	android.androidLibraries(apache)
	android.write(filepath.Join("..", "android", "app", "src", "main", "assets", "licenses.txt"), intro+introAndroid)
}
