package artifacts

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreSaveAndRestore(t *testing.T) {
	storeDir := filepath.Join(t.TempDir(), "store")
	store, err := NewStore(storeDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "build.log"), []byte("build output"), 0644); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(srcDir, "bin"), 0755); err != nil {
		t.Fatalf("make bin dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "bin", "app"), []byte("binary"), 0755); err != nil {
		t.Fatalf("write source binary: %v", err)
	}

	if err := store.Save("build_job", srcDir, []string{"build.log", "bin/*"}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	dstDir := t.TempDir()
	if err := store.Restore("build_job", dstDir); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}

	want := []string{"build.log", filepath.Join("bin", "app")}
	for _, w := range want {
		path := filepath.Join(dstDir, w)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected file %s to exist: %v", w, err)
		}
	}
}

func TestStoreRestoreMissing(t *testing.T) {
	storeDir := filepath.Join(t.TempDir(), "store")
	store, err := NewStore(storeDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	dstDir := t.TempDir()
	if err := store.Restore("missing_job", dstDir); err != nil {
		t.Fatalf("Restore of missing job should not error: %v", err)
	}
}

func TestCopyFileErrors(t *testing.T) {
	if err := copyFile("/nonexistent/path", "/tmp/dest"); err == nil {
		t.Error("expected error for missing source")
	}

	readOnly := t.TempDir()
	if err := os.Chmod(readOnly, 0000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(readOnly, 0755) })

	src := filepath.Join(t.TempDir(), "src")
	if err := os.WriteFile(src, []byte("x"), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := copyFile(src, filepath.Join(readOnly, "dst")); err == nil {
		t.Error("expected error for read-only destination")
	}
}

func TestNewStoreOnFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "storefile")
	if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	_, err := NewStore(path)
	if err == nil {
		t.Fatal("expected error creating store on a file")
	}
}

func TestSaveNoMatches(t *testing.T) {
	storeDir := filepath.Join(t.TempDir(), "store")
	store, err := NewStore(storeDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	srcDir := t.TempDir()
	if err := store.Save("job", srcDir, []string{"*.nomatch"}); err != nil {
		t.Fatalf("Save with no matches should not error: %v", err)
	}
}

func TestRestoreOnFile(t *testing.T) {
	storeDir := filepath.Join(t.TempDir(), "store")
	store, err := NewStore(storeDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := os.WriteFile(filepath.Join(storeDir, "job"), []byte("x"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := store.Restore("job", t.TempDir()); err == nil {
		t.Fatal("expected error when stored artifact is a file, not a directory")
	}
}

func TestStoreClean(t *testing.T) {
	storeDir := filepath.Join(t.TempDir(), "store")
	store, err := NewStore(storeDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "file.txt"), []byte("data"), 0644); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	if err := store.Save("job", srcDir, []string{"file.txt"}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	if err := store.Clean(); err != nil {
		t.Fatalf("Clean failed: %v", err)
	}
	if _, err := os.Stat(storeDir); !os.IsNotExist(err) {
		t.Fatalf("expected store dir to be removed, got %v", err)
	}
}
