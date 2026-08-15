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
