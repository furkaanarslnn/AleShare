package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

var consoleWindow = kernel32.NewProc("GetConsoleWindow")

// hasConsole reports whether this process has a console window: started from a terminal, or given one for an
// elevated command-line run. The window and the watcher service have none.
func hasConsole() bool {
	h, _, _ := consoleWindow.Call()
	return h != 0
}

// command is exec.Command for the console programs this one runs (netsh, PowerShell, schtasks…). Without a console
// of our own, each would open a black window for a moment; they get none. With one they share it, as usual.
func command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	if !hasConsole() {
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	}
	return cmd
}

// shortcutPath is the program's entry in every user's Start menu.
func shortcutPath() string { return startMenu("AleShare.lnk") }

// oldShortcutPath is that entry under the name the program had before 1.0.32.
func oldShortcutPath() string { return startMenu("Hotspot.lnk") }

func startMenu(name string) string {
	return filepath.Join(os.Getenv("ProgramData"), `Microsoft\Windows\Start Menu\Programs`, name)
}

func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// createShortcut puts AleShare in the Start menu, pointing at the installed program (its icon comes along), and
// takes the entry with the program's old name away.
func createShortcut() error {
	_ = os.Remove(oldShortcutPath())
	script := `$s = (New-Object -ComObject WScript.Shell).CreateShortcut(` + psQuote(shortcutPath()) + `); ` +
		`$s.TargetPath = ` + psQuote(installedExe()) + `; ` +
		`$s.WorkingDirectory = ` + psQuote(dataDir()) + `; ` +
		`$s.Description = 'Telefonun internetini bu bilgisayarda kullan'; $s.Save()`
	return run("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
}

// removeLater deletes the installed program and its folder once nothing runs it any more, for an uninstall run from
// that very file (Windows does not let a running program delete itself; its window may also stay open a while).
// It tries every two seconds for up to ten minutes.
func removeLater() {
	script := `for ($i = 0; $i -lt 300; $i++) { ` +
		`try { Remove-Item -LiteralPath ` + psQuote(installedExe()) + ` -Force -ErrorAction Stop; break } catch { Start-Sleep 2 } }; ` +
		`Remove-Item -Path ` + psQuote(installedExe()+".old*") + ` -Force -ErrorAction SilentlyContinue; ` +
		`Remove-Item -LiteralPath ` + psQuote(dataDir()) + ` -ErrorAction SilentlyContinue`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW | windows.DETACHED_PROCESS}
	_ = cmd.Start()
}

// removeFolderLater deletes a folder once nothing holds a file in it any more (the window keeps its browser data
// open while it shows): a folder that can be renamed is free, and only then is it removed, whole. It tries every
// two seconds for up to ten minutes.
func removeFolderLater(dir string) {
	if _, err := os.Stat(dir); err != nil {
		return
	}
	gone := dir + ".removed"
	script := `for ($i = 0; $i -lt 300; $i++) { ` +
		`if (-not (Test-Path -LiteralPath ` + psQuote(dir) + `)) { break }; ` +
		`try { Rename-Item -LiteralPath ` + psQuote(dir) + ` -NewName ` + psQuote(filepath.Base(gone)) + ` -ErrorAction Stop; ` +
		`Remove-Item -LiteralPath ` + psQuote(gone) + ` -Recurse -Force -ErrorAction SilentlyContinue; break } catch { Start-Sleep 2 } }`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW | windows.DETACHED_PROCESS}
	_ = cmd.Start()
}
