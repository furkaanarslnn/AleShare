//go:build ignore

// gen turns a hosts file (StevenBlack/hosts, "0.0.0.0 name" lines) into ads.txt.gz: one name per line, sorted,
// the local names left out. Run from this folder: go run gen.go HOSTSFILE
package main

import (
	"bufio"
	"compress/gzip"
	"os"
	"sort"
	"strings"
)

func main() {
	in, err := os.Open(os.Args[1])
	if err != nil {
		panic(err)
	}
	defer in.Close()
	seen := map[string]bool{}
	sc := bufio.NewScanner(in)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		f := strings.Fields(line)
		if len(f) < 2 || (f[0] != "0.0.0.0" && f[0] != "127.0.0.1") {
			continue
		}
		name := strings.ToLower(strings.TrimSuffix(f[1], "."))
		switch name {
		case "localhost", "localhost.localdomain", "local", "broadcasthost", "0.0.0.0", "ip6-localhost", "ip6-loopback":
			continue
		}
		seen[name] = true
	}
	if err := sc.Err(); err != nil {
		panic(err)
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	out, err := os.Create("ads.txt.gz")
	if err != nil {
		panic(err)
	}
	zw, _ := gzip.NewWriterLevel(out, gzip.BestCompression)
	zw.Write([]byte(strings.Join(names, "\n") + "\n"))
	zw.Close()
	out.Close()
	println(len(names), "names")
}
