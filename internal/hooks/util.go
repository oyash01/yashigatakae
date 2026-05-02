package hooks

import (
	"io"
	"os"
	"path/filepath"
)

// io_discard returns io.Discard; named separately so callers don't need to
// import io directly.
func io_discard() io.Writer { return io.Discard }

// copyTreeOverwrite is a minimal cross-platform fallback for rsync used by
// the autocommit hook on systems (mostly Windows) without rsync. It copies
// every file in src into dst, overwriting matching paths but NOT deleting
// removed-on-source files.
func copyTreeOverwrite(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		info, err := in.Stat()
		if err != nil {
			return err
		}
		w, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer w.Close()
		_, err = io.Copy(w, in)
		return err
	})
}
