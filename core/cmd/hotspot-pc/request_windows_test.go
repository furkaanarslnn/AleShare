package main

import (
	"encoding/json"
	"os"
	"testing"
)

// The service installs nothing from the inbox that is not a signed release: the request goes, and the window is
// told why.
func TestInboxTakesOnlySignedReleases(t *testing.T) {
	t.Setenv("ProgramData", t.TempDir())
	if err := os.MkdirAll(inboxDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if !requestWaitingAfter(t, []byte("MZ not a release")) {
		t.Fatal("the request was not seen")
	}
	takeRequest(47)
	if requestWaiting() {
		t.Fatal("the request stayed")
	}
	raw, err := os.ReadFile(resultPath())
	if err != nil {
		t.Fatal(err)
	}
	var r requestResult
	if json.Unmarshal(raw, &r) != nil || r.Error == "" || r.Name != "" || r.At == 0 {
		t.Fatalf("result %s", raw)
	}
	if _, err := os.Stat(installedExe()); err == nil {
		t.Fatal("something was installed")
	}
}

func requestWaitingAfter(t *testing.T, data []byte) bool {
	t.Helper()
	if err := os.WriteFile(requestPath(), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return requestWaiting()
}

// The inbox's access list lets a user's process hand over a release and take its own file back.
func TestInboxLetsUsersHandOver(t *testing.T) {
	t.Setenv("ProgramData", t.TempDir())
	if err := os.MkdirAll(dataDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ensureInbox(); err != nil {
		t.Fatal(err)
	}
	part := requestPath() + ".part"
	if err := os.WriteFile(part, []byte("MZ"), 0o644); err != nil {
		t.Fatalf("a user cannot add a file: %v", err)
	}
	if err := os.Rename(part, requestPath()); err != nil {
		t.Fatalf("a user cannot name it: %v", err)
	}
	if err := os.Remove(requestPath()); err != nil {
		t.Fatalf("a user cannot take it back: %v", err)
	}
}
