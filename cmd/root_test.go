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

func TestExecute(t *testing.T) {
	old := os.Args
	defer func() { os.Args = old }()
	os.Args = []string{"gitlab-ci-sim", "help"}
	if err := Execute(); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
}

func TestLoadEnvFileSkipsInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := `
VALID=ok
NO_EQUALS
=NO_KEY
# comment
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	vars, err := loadEnvFile(path)
	if err != nil {
		t.Fatalf("loadEnvFile: %v", err)
	}
	if len(vars) != 1 || vars[0] != "VALID=ok" {
		t.Errorf("expected [VALID=ok], got %v", vars)
	}
}

func TestLoadEnvFileMissing(t *testing.T) {
	_, err := loadEnvFile("/non/existent/.env")
	if err == nil {
		t.Fatal("expected error for missing env file")
	}
}
