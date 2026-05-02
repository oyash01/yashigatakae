package kintsugi

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"
)

// Manifest is the JSON header inside each kintsugi tarball. Restored on the
// other side to know which sessionId, cwd, and machine the bundle came from.
type Manifest struct {
	Version       string    `json:"version"`         // "kintsugi-1"
	SessionID     string    `json:"session_id"`      // Claude Code session UUID
	SourceMachine string    `json:"source_machine"`  // hostname
	SourceCWD     string    `json:"source_cwd"`      // /Users/rohitkumar/Desktop/ghostnode
	CreatedAt     time.Time `json:"created_at"`
	Note          string    `json:"note,omitempty"`
	HasWorktree   bool      `json:"has_worktree"`
	HasMemory     bool      `json:"has_memory"`
}

const manifestName = "manifest.json"
const sessionTranscriptName = "session.jsonl"

// PackOptions captures what to include in a handoff bundle.
type PackOptions struct {
	SessionID      string // required
	TranscriptFile string // path to <uuid>.jsonl
	SourceCWD      string
	Note           string
	MemoryDir      string // optional; ~/.claude/projects/<encoded-cwd>/memory
	WorktreeDir    string // optional; the active project's working tree (uncommitted changes only — handled separately)
}

// Pack builds an in-memory tarball from PackOptions and returns the raw bytes
// PLUS a Manifest. The caller is responsible for encryption.
func Pack(o PackOptions) ([]byte, Manifest, error) {
	if o.SessionID == "" {
		return nil, Manifest{}, errors.New("session_id required")
	}
	if o.TranscriptFile == "" {
		return nil, Manifest{}, errors.New("transcript_file required")
	}

	host, _ := os.Hostname()
	mf := Manifest{
		Version:       "kintsugi-1",
		SessionID:     o.SessionID,
		SourceMachine: host,
		SourceCWD:     o.SourceCWD,
		CreatedAt:     time.Now().UTC(),
		Note:          o.Note,
		HasMemory:     o.MemoryDir != "",
		HasWorktree:   o.WorktreeDir != "",
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	// 1. manifest
	mfBytes, _ := json.MarshalIndent(mf, "", "  ")
	if err := tw.WriteHeader(&tar.Header{
		Name:    manifestName,
		Mode:    0o644,
		Size:    int64(len(mfBytes)),
		ModTime: mf.CreatedAt,
	}); err != nil {
		return nil, mf, err
	}
	if _, err := tw.Write(mfBytes); err != nil {
		return nil, mf, err
	}

	// 2. session transcript
	if err := addFile(tw, sessionTranscriptName, o.TranscriptFile); err != nil {
		return nil, mf, err
	}

	// 3. memory dir (recursive)
	if o.MemoryDir != "" {
		if err := addDir(tw, "memory/", o.MemoryDir); err != nil {
			return nil, mf, fmt.Errorf("memory dir: %w", err)
		}
	}

	// 4. worktree (uncommitted changes only — handled by caller via Worktree() below)
	// (The caller hands us a pre-built tar fragment if they want worktree. v0.3.0-rc2
	// keeps worktree out of scope; rc3 brings it back via the watcher path.)

	if err := tw.Close(); err != nil {
		return nil, mf, err
	}
	if err := gz.Close(); err != nil {
		return nil, mf, err
	}
	return buf.Bytes(), mf, nil
}

func addFile(tw *tar.Writer, dstName, srcPath string) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", srcPath, err)
	}
	info, err := os.Stat(srcPath)
	if err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{
		Name:    dstName,
		Mode:    0o644,
		Size:    int64(len(data)),
		ModTime: info.ModTime(),
	}); err != nil {
		return err
	}
	_, err = tw.Write(data)
	return err
}

func addDir(tw *tar.Writer, prefix, dir string) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		if rel == "." {
			return nil
		}
		// Tar uses forward slashes regardless of host.
		dstName := prefix + filepath.ToSlash(rel)
		if d.IsDir() {
			return tw.WriteHeader(&tar.Header{
				Name:    dstName + "/",
				Mode:    0o755,
				ModTime: time.Now(),
				Typeflag: tar.TypeDir,
			})
		}
		return addFile(tw, dstName, path)
	})
}

// Encrypt wraps `body` in an age envelope using a 32-byte hex passphrase
// (the KINTSUGI_KEY from secrets.env). Returns ciphertext.
func Encrypt(body []byte, hexKey string) ([]byte, error) {
	if hexKey == "" {
		return nil, errors.New("KINTSUGI_KEY is empty (set it in ~/.yashigatakae/secrets.env)")
	}
	rec, err := age.NewScryptRecipient(hexKey)
	if err != nil {
		return nil, err
	}
	rec.SetWorkFactor(15) // bump from default 18 to 15 for speed; key is high-entropy already
	var out bytes.Buffer
	w, err := age.Encrypt(&out, rec)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(body); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// Decrypt reverses Encrypt with the same hex passphrase.
func Decrypt(ciphertext []byte, hexKey string) ([]byte, error) {
	if hexKey == "" {
		return nil, errors.New("KINTSUGI_KEY is empty")
	}
	id, err := age.NewScryptIdentity(hexKey)
	if err != nil {
		return nil, err
	}
	r, err := age.Decrypt(bytes.NewReader(ciphertext), id)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

// Unpack reverses Pack. dstRoot is the directory where files are extracted
// (e.g. a temporary scratch dir; the caller moves files into ~/.claude/ etc).
func Unpack(body []byte, dstRoot string) (Manifest, error) {
	gz, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return Manifest{}, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var mf Manifest
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return mf, err
		}
		// Reject path traversal.
		if strings.Contains(hdr.Name, "..") {
			return mf, fmt.Errorf("rejected entry with parent traversal: %s", hdr.Name)
		}
		dst := filepath.Join(dstRoot, filepath.FromSlash(hdr.Name))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return mf, err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return mf, err
			}
			f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return mf, err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return mf, err
			}
			f.Close()
			if hdr.Name == manifestName {
				raw, _ := os.ReadFile(dst)
				_ = json.Unmarshal(raw, &mf)
			}
		}
	}
	return mf, nil
}

// Fingerprint returns a short SHA256-12 of the body, useful as a "resume code"
// printed by handoff and consumed by resume.
func Fingerprint(body []byte) string {
	h := sha256.Sum256(body)
	return fmt.Sprintf("%x", h[:6])
}
