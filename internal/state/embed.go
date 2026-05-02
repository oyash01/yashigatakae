package state

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// embedded carries the default templates + caveman hooks + bundled skills
// INSIDE the binary. This decouples the public yashigatakae tool from any
// user's private state repo: a fresh `init` works without ever touching
// GitHub for skills/hooks.
//
//go:embed embedded/templates/*.tmpl embedded/templates/*.env embedded/hooks/* embedded/skills/wiki/SKILL.md
var embedded embed.FS

// extractEmbeddedTemplates renders every embedded *.tmpl file into claudeDir,
// honoring the "preserve user data" rule: skip if target exists with content.
// .env templates render verbatim (no template parsing).
//
// Returns the list of paths actually written for the doctor report.
func extractEmbeddedTemplates(claudeDir, home, user string) ([]string, error) {
	data := map[string]string{"HOME": home, "USER": user}
	var written []string
	entries, err := embedded.ReadDir("embedded/templates")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		src := "embedded/templates/" + e.Name()
		raw, err := embedded.ReadFile(src)
		if err != nil {
			return written, err
		}
		dstName := e.Name()
		// Only .tmpl files get the .tmpl suffix stripped; .env stays as-is
		// (and lands as the "example" file at ~/.yashigatakae/secrets.example.env
		// — handled below, not in claudeDir).
		if strings.HasSuffix(dstName, ".tmpl") {
			dstName = strings.TrimSuffix(dstName, ".tmpl")
			dst := filepath.Join(claudeDir, dstName)
			if info, err := os.Stat(dst); err == nil && info.Size() > 0 {
				continue // user data, preserve
			}
			t, err := template.New(e.Name()).Parse(string(raw))
			if err != nil {
				return written, err
			}
			f, err := os.Create(dst)
			if err != nil {
				return written, err
			}
			if err := t.Execute(f, data); err != nil {
				f.Close()
				return written, err
			}
			f.Close()
			written = append(written, dst)
		} else if strings.HasSuffix(dstName, ".env") {
			// secrets.example.env → ~/.yashigatakae/secrets.example.env
			yashDir := filepath.Join(filepath.Dir(claudeDir), ".yashigatakae")
			_ = os.MkdirAll(yashDir, 0o755)
			dst := filepath.Join(yashDir, dstName)
			if _, err := os.Stat(dst); err == nil {
				continue
			}
			_ = os.WriteFile(dst, raw, 0o644)
			written = append(written, dst)
		}
	}
	return written, nil
}

// extractEmbeddedSkills copies every directory under embedded/skills/ into
// claudeDir/skills/<name>/. Existing files are overwritten — the bundled
// skills are tool-managed. User-authored skills live alongside untouched.
func extractEmbeddedSkills(claudeDir string) ([]string, error) {
	skillsDst := filepath.Join(claudeDir, "skills")
	if err := os.MkdirAll(skillsDst, 0o755); err != nil {
		return nil, err
	}
	var written []string
	err := fs.WalkDir(embedded, "embedded/skills", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		raw, err := embedded.ReadFile(p)
		if err != nil {
			return err
		}
		// Mirror the relative tree under embedded/skills/ into claudeDir/skills/.
		rel := strings.TrimPrefix(p, "embedded/skills/")
		dst := filepath.Join(skillsDst, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, raw, 0o644); err != nil {
			return err
		}
		written = append(written, dst)
		return nil
	})
	return written, err
}

// extractEmbeddedHooks copies every file in embedded/hooks/ into
// claudeDir/hooks/. Existing files are overwritten (these are tool-managed,
// not user-edited). Caveman + autocommit + sweep stubs ship this way.
func extractEmbeddedHooks(claudeDir string) ([]string, error) {
	hooksDst := filepath.Join(claudeDir, "hooks")
	if err := os.MkdirAll(hooksDst, 0o755); err != nil {
		return nil, err
	}
	var written []string
	err := fs.WalkDir(embedded, "embedded/hooks", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		raw, err := embedded.ReadFile(p)
		if err != nil {
			return err
		}
		dst := filepath.Join(hooksDst, filepath.Base(p))
		// Make .sh executable; .js stays 0644 (node runs it).
		mode := os.FileMode(0o644)
		if strings.HasSuffix(dst, ".sh") {
			mode = 0o755
		}
		if err := os.WriteFile(dst, raw, mode); err != nil {
			return err
		}
		written = append(written, dst)
		return nil
	})
	return written, err
}
