// Package osdetect detects the host operating system and returns the
// canonical paths yashigatakae uses on that platform.
package osdetect

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

type OS string

const (
	OSMac     OS = "mac"
	OSLinux   OS = "linux"
	OSWindows OS = "windows"
	OSUnknown OS = "unknown"
)

// Detect returns the canonical OS identifier.
func Detect() OS {
	switch runtime.GOOS {
	case "darwin":
		return OSMac
	case "linux":
		return OSLinux
	case "windows":
		return OSWindows
	default:
		return OSUnknown
	}
}

// HomeDir returns the user's home directory across platforms.
func HomeDir() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user home: %w", err)
	}
	return h, nil
}

// ClaudeDir returns ~/.claude (or %USERPROFILE%\.claude on Windows).
func ClaudeDir() (string, error) {
	h, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".claude"), nil
}

// YashigatakaeDir returns ~/.yashigatakae.
func YashigatakaeDir() (string, error) {
	h, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".yashigatakae"), nil
}

// LocalBinDir returns the install path for the binary.
// /usr/local/bin (admin) or ~/.local/bin (user) on unix; %LOCALAPPDATA%\yashigatakae on Windows.
func LocalBinDir() (string, error) {
	h, err := HomeDir()
	if err != nil {
		return "", err
	}
	switch Detect() {
	case OSWindows:
		return filepath.Join(h, "AppData", "Local", "yashigatakae"), nil
	default:
		return filepath.Join(h, ".local", "bin"), nil
	}
}

// String for printing.
func (o OS) String() string { return string(o) }
