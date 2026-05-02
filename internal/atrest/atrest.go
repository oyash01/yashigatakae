// Package atrest gives the bifrost / hermes / mempalace daemons a one-line
// way to encrypt their sqlite databases when stopped and decrypt them on
// start. Defends against a snapshot-the-disk attacker on the VPS.
//
// Wire it via systemd:
//
//   ExecStartPre=/usr/local/bin/yashigatakae db unlock /var/lib/yashigatakae/mempalace.db
//   ExecStopPost=/usr/local/bin/yashigatakae db lock   /var/lib/yashigatakae/mempalace.db
//
// Key derivation: HKDF(SHA256, secret=KINTSUGI_KEY, salt="yashigatakae-atrest-v1")
// → 32 bytes → hex → age scrypt passphrase. Same KEY everywhere so a single
// `secrets rotate` invalidates ALL the at-rest blobs in lockstep.
package atrest

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"filippo.io/age"
	"golang.org/x/crypto/hkdf"
)

const hkdfSalt = "yashigatakae-atrest-v1"

// passphraseFromKintsugiKey derives a stable 32-byte hex passphrase for age
// from KINTSUGI_KEY (which is itself a 32-byte hex random). Different salt
// from kintsugi blob encryption so rotating one doesn't invalidate the other.
func passphraseFromKintsugiKey() (string, error) {
	kk := os.Getenv("KINTSUGI_KEY")
	if kk == "" {
		return "", errors.New("KINTSUGI_KEY not set — at-rest encryption requires it")
	}
	r := hkdf.New(sha256.New, []byte(kk), []byte(hkdfSalt), []byte("at-rest db key"))
	out := make([]byte, 32)
	if _, err := io.ReadFull(r, out); err != nil {
		return "", err
	}
	return hex.EncodeToString(out), nil
}

// Lock encrypts `path` to `path + ".age"` using the derived passphrase, then
// removes the plaintext. Idempotent: if `<path>` doesn't exist but `<path>.age`
// does, returns nil (already locked).
func Lock(path string) error {
	pass, err := passphraseFromKintsugiKey()
	if err != nil {
		return err
	}
	cipherPath := path + ".age"
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if _, err2 := os.Stat(cipherPath); err2 == nil {
			return nil
		}
		return fmt.Errorf("nothing at %s to lock", path)
	}
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()
	rec, err := age.NewScryptRecipient(pass)
	if err != nil {
		return err
	}
	rec.SetWorkFactor(15)

	tmpPath := cipherPath + ".tmp"
	out, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	w, err := age.Encrypt(out, rec)
	if err != nil {
		out.Close()
		return err
	}
	if _, err := io.Copy(w, in); err != nil {
		w.Close()
		out.Close()
		return err
	}
	if err := w.Close(); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, cipherPath); err != nil {
		return err
	}
	// Plaintext removed last so a crash mid-encrypt leaves both files
	// (recoverable) instead of nothing.
	return os.Remove(path)
}

// Unlock decrypts `path + ".age"` to `path` and removes the ciphertext.
// Idempotent: if `<path>` already exists and `<path>.age` doesn't, returns nil.
func Unlock(path string) error {
	pass, err := passphraseFromKintsugiKey()
	if err != nil {
		return err
	}
	cipherPath := path + ".age"
	if _, err := os.Stat(cipherPath); os.IsNotExist(err) {
		if _, err2 := os.Stat(path); err2 == nil {
			return nil
		}
		return fmt.Errorf("neither %s nor %s exists", path, cipherPath)
	}
	in, err := os.Open(cipherPath)
	if err != nil {
		return err
	}
	defer in.Close()
	id, err := age.NewScryptIdentity(pass)
	if err != nil {
		return err
	}
	r, err := age.Decrypt(in, id)
	if err != nil {
		return err
	}
	tmpPath := path + ".tmp"
	out, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return os.Remove(cipherPath)
}

// LockAll locks every path in `paths`, continuing past individual failures.
// Returns the first error if any. Used by `yashigatakae db lock-all` as a
// convenience for the systemd ExecStopPost.
func LockAll(paths []string) error {
	var first error
	for _, p := range paths {
		if err := Lock(p); err != nil {
			if first == nil {
				first = err
			}
			fmt.Fprintf(os.Stderr, "atrest lock %s: %v\n", filepath.Base(p), err)
		}
	}
	return first
}

// UnlockAll mirrors LockAll for ExecStartPre.
func UnlockAll(paths []string) error {
	var first error
	for _, p := range paths {
		if err := Unlock(p); err != nil {
			if first == nil {
				first = err
			}
			fmt.Fprintf(os.Stderr, "atrest unlock %s: %v\n", filepath.Base(p), err)
		}
	}
	return first
}
