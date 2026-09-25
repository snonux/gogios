package internal

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// writeFileAtomic replaces path with data in one rename. The data goes to a
// temporary file unique to this call (not a fixed "<path>.tmp"), so two
// Gogios runs writing the same file at once cannot interleave their bytes or
// rename each other's half-written file into place; readers (httpd, the
// peer, the next run) see either the old or the new content, never a mix.
func writeFileAtomic(path string, data []byte, perm fs.FileMode) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}

	f, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
		}
	}()

	if _, err = f.Write(data); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	// CreateTemp creates the file 0600; the reports must stay readable by
	// the web server, so set the requested mode explicitly.
	if err = f.Chmod(perm); err != nil {
		return fmt.Errorf("chmod %s: %w", tmp, err)
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmp, err)
	}
	if err = os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmp, path, err)
	}
	return nil
}
