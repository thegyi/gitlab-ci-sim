package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := `
# Comment line

KEY=value
SPACED = spaced value
EMPTY=
# Another comment
  INDENTED=indented
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	vars, err := loadEnvFile(path)
	if err != nil {
		t.Fatalf("loadEnvFile failed: %v", err)
	}

	want := []string{
		"KEY=value",
		"SPACED=spaced value",
		"EMPTY=",
		"INDENTED=indented",
	}
	if !reflect.DeepEqual(vars, want) {
		t.Fatalf("expected %v, got %v", want, vars)
	}
}

func TestLoadEnvFileMissing(t *testing.T) {
	_, err := loadEnvFile("/non/existent/.env")
	if err == nil {
		t.Fatal("expected error for missing env file")
	}
}
