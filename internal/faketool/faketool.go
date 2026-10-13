// Package faketool lets a test stand a shell script in for a real executable
// — ghpm's vendored gh, sheesh's kebab — on every platform.
//
// On Unix a script with a #! line is an executable, and that is all Write
// does. Windows runs only a real program from a path ending in .exe, so there
// the fake is the test binary itself, copied to that path, with the script
// beside it: when a binary started that way finds its script, Main runs it
// with sh and exits, before any test does. A package whose tests call Write
// therefore has to call Main first thing in its TestMain.
package faketool

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// scriptSuffix names the script that sits beside a fake on windows.
const scriptSuffix = ".fake.sh"

// Main runs the script this binary is standing in for, if it is standing in
// for one, and does not return. Otherwise it returns at once and the tests
// run as usual.
func Main() {
	if runtime.GOOS != "windows" {
		return
	}
	self, err := os.Executable()
	if err != nil {
		return
	}
	script := self + scriptSuffix
	if _, err := os.Stat(script); err != nil {
		return
	}
	sh, err := findSh()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(127)
	}
	// forward slashes, so `dirname "$0"` in the script finds its directory
	cmd := exec.Command(sh, append([]string{filepath.ToSlash(script)}, os.Args[1:]...)...) //nolint:gosec
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err = cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		os.Exit(0)
	case errors.As(err, &exit):
		os.Exit(exit.ExitCode())
	default:
		fmt.Fprintln(os.Stderr, err)
		os.Exit(127)
	}
}

// findSh locates a POSIX sh on windows: the one Git for Windows ships. Never
// a bare "bash", which on windows can be the WSL launcher in System32.
func findSh() (string, error) {
	for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramW6432"), `C:\Program Files`} {
		if root == "" {
			continue
		}
		for _, rel := range []string{`Git\bin\sh.exe`, `Git\usr\bin\sh.exe`} {
			if path := filepath.Join(root, rel); fileExists(path) {
				return path, nil
			}
		}
	}
	if path, err := exec.LookPath("sh"); err == nil {
		return path, nil
	}
	return "", errors.New("faketool: no sh found (Git for Windows provides one)")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// Write stands script — the body of a shell script, with no #! line — in for
// the executable at path, and returns the bytes now at path, for a test that
// pins the fake by content. On windows a test that cannot be given an sh is
// skipped.
func Write(t testing.TB, path, script string) []byte {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		body := []byte("#!/bin/sh\n" + script + "\n")
		if err := os.WriteFile(path, body, 0755); err != nil {
			t.Fatal(err)
		}
		return body
	}
	if _, err := findSh(); err != nil {
		t.Skip(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	body, err := copyFile(self, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+scriptSuffix, []byte(script+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return body
}

func copyFile(src, dst string) ([]byte, error) {
	in, err := os.Open(src)
	if err != nil {
		return nil, err
	}
	defer func() { _ = in.Close() }()
	body, err := io.ReadAll(in)
	if err != nil {
		return nil, err
	}
	return body, os.WriteFile(dst, body, 0755)
}
