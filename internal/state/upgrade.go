package state

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// UpgradeOptions captures CLI flags for `yashigatakae upgrade`.
type UpgradeOptions struct {
	Repo         string // default oyash01/yashigatakae
	TargetTag    string // pin a specific version; default = latest
	IncludeState bool   // also `git pull` the state-repo
}

// UpgradeResult is what Upgrade returns.
type UpgradeResult struct {
	OldVersion  string
	NewVersion  string
	BinaryPath  string
	StateBumped bool
}

// Upgrade replaces the running binary with the latest GitHub release.
// On Windows this only WRITES the new binary alongside the old (foo.exe.new)
// because Windows doesn't allow renaming a running .exe; the user runs
// `yashigatakae upgrade --finalize` after restart. On Unix we rename in place.
func Upgrade(opts UpgradeOptions) (UpgradeResult, error) {
	if opts.Repo == "" {
		opts.Repo = "oyash01/yashigatakae"
	}
	current, _ := os.Executable()

	// 1. Resolve target version.
	tag := opts.TargetTag
	var err error
	if tag == "" {
		tag, err = latestRelease(opts.Repo)
		if err != nil {
			return UpgradeResult{}, err
		}
	}
	res := UpgradeResult{
		OldVersion: readEmbeddedVersion(),
		NewVersion: tag,
		BinaryPath: current,
	}
	if res.OldVersion == tag {
		fmt.Printf("✓ already on %s\n", tag)
		return res, nil
	}

	// 2. Pick asset for this OS/arch.
	asset := assetName(runtime.GOOS, runtime.GOARCH)
	if asset == "" {
		return res, fmt.Errorf("unsupported platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	url := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", opts.Repo, tag, asset)
	fmt.Printf("  · downloading %s\n", url)

	tmpDir, err := os.MkdirTemp("", "yashi-upgrade-*")
	if err != nil {
		return res, err
	}
	defer os.RemoveAll(tmpDir)
	archivePath := filepath.Join(tmpDir, asset)
	if err := downloadFile(url, archivePath); err != nil {
		return res, err
	}

	// 3. Extract.
	binname := "yashigatakae"
	if runtime.GOOS == "windows" {
		binname = "yashigatakae.exe"
	}
	extracted := filepath.Join(tmpDir, binname)
	if err := extractBinary(archivePath, extracted); err != nil {
		return res, err
	}
	if err := os.Chmod(extracted, 0o755); err != nil {
		return res, err
	}

	// 4. Replace.
	if runtime.GOOS == "windows" {
		newPath := current + ".new"
		if err := os.Rename(extracted, newPath); err != nil {
			return res, err
		}
		fmt.Printf("  · staged at %s — close any running yashigatakae and rename to %s\n", newPath, current)
	} else {
		// Atomic rename on the same filesystem.
		if err := os.Rename(extracted, current); err != nil {
			return res, fmt.Errorf("replace %s: %w", current, err)
		}
		fmt.Printf("  · replaced %s\n", current)
	}

	// 5. Optionally bump state-repo too.
	if opts.IncludeState {
		if err := Pull(); err == nil {
			res.StateBumped = true
		}
	}
	return res, nil
}

// latestRelease returns the GitHub "latest" tag (v.x.y).
func latestRelease(repo string) (string, error) {
	api := "https://api.github.com/repos/" + repo + "/releases/latest"
	resp, err := http.Get(api)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("releases/latest: HTTP %d", resp.StatusCode)
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}
	if rel.TagName == "" {
		return "", fmt.Errorf("releases/latest returned empty tag")
	}
	return rel.TagName, nil
}

func assetName(goos, goarch string) string {
	switch {
	case goos == "darwin" && goarch == "arm64":
		return "yashigatakae-darwin-arm64.tar.gz"
	case goos == "darwin" && goarch == "amd64":
		return "yashigatakae-darwin-amd64.tar.gz"
	case goos == "linux" && goarch == "amd64":
		return "yashigatakae-linux-amd64.tar.gz"
	case goos == "linux" && goarch == "arm64":
		return "yashigatakae-linux-arm64.tar.gz"
	case goos == "windows" && goarch == "amd64":
		return "yashigatakae-windows-amd64.zip"
	}
	return ""
}

func downloadFile(url, dst string) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("download: HTTP %d: %s", resp.StatusCode, string(body))
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

func extractBinary(archive, dst string) error {
	if strings.HasSuffix(archive, ".tar.gz") {
		f, err := os.Open(archive)
		if err != nil {
			return err
		}
		defer f.Close()
		gz, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer gz.Close()
		tr := tar.NewReader(gz)
		for {
			h, err := tr.Next()
			if err == io.EOF {
				return fmt.Errorf("yashigatakae binary not found in tarball")
			}
			if err != nil {
				return err
			}
			if h.Typeflag == tar.TypeReg && (filepath.Base(h.Name) == "yashigatakae") {
				out, err := os.Create(dst)
				if err != nil {
					return err
				}
				defer out.Close()
				_, err = io.Copy(out, tr)
				return err
			}
		}
	}
	if strings.HasSuffix(archive, ".zip") {
		zr, err := zip.OpenReader(archive)
		if err != nil {
			return err
		}
		defer zr.Close()
		for _, f := range zr.File {
			if filepath.Base(f.Name) == "yashigatakae.exe" {
				rc, err := f.Open()
				if err != nil {
					return err
				}
				defer rc.Close()
				out, err := os.Create(dst)
				if err != nil {
					return err
				}
				defer out.Close()
				_, err = io.Copy(out, rc)
				return err
			}
		}
		return fmt.Errorf("yashigatakae.exe not found in zip")
	}
	return fmt.Errorf("unsupported archive %s", archive)
}

// readEmbeddedVersion is best-effort — we set it via -ldflags at build time.
// Reading the running binary's debug info is more involved; the upgrade
// path doesn't strictly need it (we're going to swap regardless).
func readEmbeddedVersion() string {
	return "(running)"
}
