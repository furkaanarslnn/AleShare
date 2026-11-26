package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"

	"hotspot/core/internal/release"
)

// A newer release is installed by the watcher service, not by an elevated copy of the program the user opened.
// An elevated program that runs from the user's folder, stops a service, copies itself into ProgramData and puts
// the service back looks to antivirus programs like malware settling in (Kaspersky's System Watcher stopped one
// half-way, 26 November 2026, and took the file). The service does the same for the releases it takes from the
// phone, and was never stopped doing it. So the window, which runs with the user's rights, leaves the release in
// the inbox, a folder of the program's where users may add files, and the service installs it at its next look
// (every few seconds): only with this project's release signature and only when it is newer, as from the phone.
// No consent prompt either: a signed, newer release is what the service installs by itself anyway.

// maxRequest is the largest file the inbox takes, well above the program's size.
const maxRequest = 64 << 20

func inboxDir() string    { return filepath.Join(dataDir(), "inbox") }
func requestPath() string { return filepath.Join(inboxDir(), "install.request") }
func resultPath() string  { return filepath.Join(inboxDir(), "install.result") }

// ensureInbox makes the inbox with its own access list: the system and the administrators may do anything, users
// may list it and add files, the files they add are theirs, and what the service writes (the result) they may read.
// Run with administrator rights (setup, upgrade) or by the service.
func ensureInbox() error {
	dir := inboxDir()
	if isLink(dir) {
		if err := os.Remove(dir); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;;0x1200ab;;;BU)(A;OICIIO;FR;;;BU)(A;OICIIO;FA;;;CO)")
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	_ = command("icacls.exe", dir, "/setowner", "*S-1-5-32-544", "/L", "/Q").Run()
	return windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}

// requestResult is what the service says of a request: the release it installed, or why it did not.
type requestResult struct {
	Name  string `json:"name,omitempty"`
	Error string `json:"error,omitempty"`
	At    int64  `json:"at"` // Unix seconds
}

// requestWaiting reports whether a release waits in the inbox.
func requestWaiting() bool {
	_, err := os.Lstat(requestPath())
	return err == nil
}

// takeRequest installs the release waiting in the inbox, if any, when it carries the release signature and is newer
// than own. The file is read once and exactly the bytes that were checked are installed (from the program's own
// folder, where users cannot write). Run by the service, in its look (see cmdReconcile).
func takeRequest(own int) {
	fi, err := os.Lstat(requestPath())
	if err != nil {
		return
	}
	defer os.Remove(requestPath()) // a link is removed itself, never what it leads to
	_ = os.Remove(resultPath())
	answer := func(r requestResult) {
		r.At = time.Now().Unix()
		if data, err := json.Marshal(r); err == nil {
			_ = os.WriteFile(resultPath(), data, 0o644)
		}
	}
	if !fi.Mode().IsRegular() || fi.Size() > maxRequest {
		answer(requestResult{Error: "Kurulacak dosya geçersiz."})
		return
	}
	data, err := os.ReadFile(requestPath())
	if err != nil {
		answer(requestResult{Error: err.Error()})
		return
	}
	info, err := release.Verify(data)
	if err != nil {
		answer(requestResult{Error: "Dosya AleShare'in imzasını taşımıyor; kurulmadı."})
		return
	}
	if info.Code <= own {
		answer(requestResult{Error: fmt.Sprintf("%s bu bilgisayardakinden yeni değil.", info.Name)})
		return
	}
	path := filepath.Join(dataDir(), "hotspot-pc.request")
	defer os.Remove(path)
	if err := os.WriteFile(path, data, 0o755); err != nil {
		answer(requestResult{Error: err.Error()})
		return
	}
	if err := upgradeFrom(path, info.Name); err != nil {
		logf("pencereden gelen %s kurulamadı: %v", info.Name, err)
		answer(requestResult{Error: err.Error()})
		return
	}
	logf("pencereden %s geldi ve kuruldu (imza doğru)", info.Name)
	answer(requestResult{Name: info.Name})
}

// errNoWatcher: the service does not run (or the installation is older than the inbox); the caller installs the
// old way, with administrator rights.
var errNoWatcher = errors.New("izleme hizmeti çalışmıyor")

// installViaWatcher hands the release in data to the watcher service and waits for its answer.
func installViaWatcher(data []byte) error {
	if watchServiceState() != "çalışıyor" {
		return errNoWatcher
	}
	if fi, err := os.Stat(inboxDir()); err != nil || !fi.IsDir() {
		return errNoWatcher
	}
	began := time.Now().Unix()
	part := filepath.Join(inboxDir(), fmt.Sprintf("install-%d.part", time.Now().UnixNano()))
	if err := os.WriteFile(part, data, 0o644); err != nil {
		return errNoWatcher // no right to add files: an inbox made by hand, or another account's request is there
	}
	if err := os.Rename(part, requestPath()); err != nil {
		os.Remove(part)
		return errNoWatcher
	}
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		raw, err := os.ReadFile(resultPath())
		if err != nil {
			continue
		}
		var r requestResult
		if json.Unmarshal(raw, &r) != nil || r.At < began-1 {
			continue // an older answer, or one being written
		}
		if r.Error != "" {
			return errors.New(r.Error)
		}
		return nil
	}
	_ = os.Remove(requestPath())
	return errors.New("İzleme hizmeti yanıt vermedi; birazdan yeniden dene.")
}
