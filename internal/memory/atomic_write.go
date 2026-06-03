package memory

import (
	"fmt"
	"os"
	"path/filepath"
)

// writeFileAtomic writes data to path crash-safely: it writes to a temp file in
// the same directory, fsyncs it, then renames it over the destination. This
// mirrors KnowledgeGraph.saveUnlocked so a crash mid-write cannot leave a
// partially written JSON file. The destination is chmod'd to 0644 before the
// rename to preserve the create-mode of the os.WriteFile(path, data, 0644)
// calls this helper replaced.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpName, 0644); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("chmod temp file: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename temp to %s: %w", path, err)
	}

	return nil
}
