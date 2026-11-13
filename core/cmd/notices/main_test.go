package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every module go.mod asks for must be named in the license texts of a published program, so that a new
// dependency cannot be shipped without its license: after adding one, run this command again.
func TestEveryModuleIsNamed(t *testing.T) {
	mod, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	var texts string
	for _, path := range []string{
		filepath.Join("..", "hotspot-pc", "ui", "licenses.txt"),
		filepath.Join("..", "..", "..", "android", "app", "src", "main", "assets", "licenses.txt"),
	} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), rule) {
			t.Errorf("%s has no sections", path)
		}
		texts += string(b)
	}
	// Only the build uses these (gomobile's generator); nothing of them ends up in a program.
	tools := map[string]bool{"golang.org/x/mod": true, "golang.org/x/sync": true, "golang.org/x/tools": true}
	required := regexp.MustCompile(`(?m)^\s+(\S+) v\S+`).FindAllStringSubmatch(string(mod), -1)
	if len(required) < 5 {
		t.Fatalf("go.mod was not understood: %d modules", len(required))
	}
	for _, m := range required {
		if !tools[m[1]] && !strings.Contains(texts, "\n"+m[1]+" v") {
			t.Errorf("%s is not named in the license texts; run: go run ./cmd/notices", m[1])
		}
	}
}

func TestSameTextSharesASection(t *testing.T) {
	var n notices
	n.add("a", "The text.\r\n  \r\nMore.  \n")
	n.add("b", "The text.\n\nMore.")
	n.add("c", "Another.")
	n.add("a", "The text.\n\nMore.")
	if len(n.order) != 2 || strings.Join(n.names[n.order[0]], ",") != "a,b" {
		t.Fatalf("sections: %q, names: %v", n.order, n.names)
	}
}
