package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

var (
	attachConsole = kernel32.NewProc("AttachConsole")
	allocConsole  = kernel32.NewProc("AllocConsole")
)

// useConsole gives this windowless program (built for the GUI, so a double click opens no black window) a console
// for a command typed on the command line: the terminal it was started from, or, when own is set, a console of its
// own (a command reopened with administrator rights). Output that is redirected to a file or a pipe stays there.
func useConsole(own bool) {
	if h, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE); err == nil && h != 0 && h != windows.InvalidHandle {
		if t, _ := windows.GetFileType(h); t == windows.FILE_TYPE_PIPE || t == windows.FILE_TYPE_DISK {
			return
		}
	}
	const attachParent = ^uintptr(0) // ATTACH_PARENT_PROCESS
	if r, _, _ := attachConsole.Call(attachParent); r == 0 {
		if !own {
			return
		}
		if r, _, _ := allocConsole.Call(); r == 0 {
			return
		}
	}
	if f, err := os.OpenFile("CONOUT$", os.O_RDWR, 0); err == nil {
		os.Stdout, os.Stderr = f, f
	}
	if f, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
		os.Stdin = f
	}
}

// enableColors lets the console take ANSI color sequences (Windows 10 and later) and reports whether it does.
func enableColors() bool {
	h, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	if err != nil {
		return false
	}
	var mode uint32
	if windows.GetConsoleMode(h, &mode) != nil {
		return false
	}
	return windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) == nil
}

// showQR prints a QR code in the console.
func showQR(text string) error {
	rows, err := qrRows(text, enableColors())
	if err != nil {
		return err
	}
	fmt.Println()
	for _, r := range rows {
		fmt.Println("  " + r)
	}
	fmt.Println()
	return nil
}

// readHidden reads a line from the console without showing what is typed.
func readHidden(prompt string) string {
	fmt.Print(prompt)
	h, err := windows.GetStdHandle(windows.STD_INPUT_HANDLE)
	var mode uint32
	if err == nil && windows.GetConsoleMode(h, &mode) == nil {
		windows.SetConsoleMode(h, mode&^windows.ENABLE_ECHO_INPUT)
		defer windows.SetConsoleMode(h, mode)
	}
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	fmt.Println()
	return strings.TrimRight(line, "\r\n")
}
