package artifacts

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Store manages artifact storage between jobs.
type Store struct {
	baseDir string
}

// NewStore creates an artifact store in the given directory.
func NewStore(baseDir string) (*Store, error) {
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating artifact store: %w", err)
	}
	return &Store{baseDir: baseDir}, nil
}

// Save copies artifacts from a job's workspace to the store.
func (s *Store) Save(jobName string, workDir string, paths []string) error {
	jobDir := filepath.Join(s.baseDir, jobName)
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		return err
	}

	for _, pattern := range paths {
		matches, err := filepath.Glob(filepath.Join(workDir, pattern))
		if err != nil {
			return fmt.Errorf("globbing %s: %w", pattern, err)
		}
		for _, match := range matches {
			rel, _ := filepath.Rel(workDir, match)
			dest := filepath.Join(jobDir, rel)
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
			if err := copyFile(match, dest); err != nil {
				return fmt.Errorf("copying %s: %w", match, err)
			}
		}
	}
	return nil
}

// Restore copies artifacts from the store into a job's workspace.
func (s *Store) Restore(fromJob string, workDir string) error {
	jobDir := filepath.Join(s.baseDir, fromJob)
	if _, err := os.Stat(jobDir); os.IsNotExist(err) {
		return nil // no artifacts to restore
	}

	return filepath.Walk(jobDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(jobDir, path)
		dest := filepath.Join(workDir, rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		return copyFile(path, dest)
	})
}

// Clean removes all stored artifacts.
func (s *Store) Clean() error {
	return os.RemoveAll(s.baseDir)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
