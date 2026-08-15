package resolver

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func resolveMap(t *testing.T, data []byte, baseDir string) map[string]interface{} {
	t.Helper()
	resolved, err := Resolve(data, baseDir)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	var m map[string]interface{}
	if err := yaml.Unmarshal(resolved, &m); err != nil {
		t.Fatalf("unmarshal resolved YAML failed: %v", err)
	}
	return m
}

func TestResolveExtends(t *testing.T) {
	yml := []byte(`
stages:
  - build
  - test

.template:
  image: golang:1.21
  before_script:
    - echo setup

build_job:
  extends: .template
  stage: build
  script:
    - go build
`)

	m := resolveMap(t, yml, "")

	job, ok := m["build_job"].(map[string]interface{})
	if !ok {
		t.Fatalf("build_job not a mapping")
	}
	if job["image"] != "golang:1.21" {
		t.Errorf("expected image to be inherited, got %v", job["image"])
	}
	if _, ok := job["extends"]; ok {
		t.Errorf("extends should be removed from resolved job")
	}

	before, ok := job["before_script"].([]interface{})
	if !ok || len(before) != 1 || before[0] != "echo setup" {
		t.Errorf("expected inherited before_script, got %v", before)
	}

	script, ok := job["script"].([]interface{})
	if !ok || len(script) != 1 || script[0] != "go build" {
		t.Errorf("expected script preserved, got %v", script)
	}
}

func TestResolveReference(t *testing.T) {
	yml := []byte(`
stages:
  - build

.template:
  script:
    - echo one
    - echo two

build_job:
  stage: build
  script: !reference [.template, script]
`)

	m := resolveMap(t, yml, "")

	job := m["build_job"].(map[string]interface{})
	script, ok := job["script"].([]interface{})
	if !ok || len(script) != 2 || script[0] != "echo one" || script[1] != "echo two" {
		t.Errorf("!reference not resolved, got %v", job["script"])
	}
}

func TestResolveIncludeLocal(t *testing.T) {
	dir := t.TempDir()
	main := []byte(`
include:
  - common.yml

build_job:
  stage: build
  extends: .template
  script:
    - go build
`)
	common := []byte(`
stages:
  - build
  - test

.template:
  image: golang:1.21
`)
	if err := os.WriteFile(filepath.Join(dir, "main.yml"), main, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "common.yml"), common, 0o644); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "main.yml"))
	if err != nil {
		t.Fatal(err)
	}
	m := resolveMap(t, data, dir)

	if _, ok := m["include"]; ok {
		t.Errorf("include key should be removed")
	}

	stages, ok := m["stages"].([]interface{})
	if !ok || len(stages) != 2 {
		t.Errorf("expected merged stages, got %v", stages)
	}

	job := m["build_job"].(map[string]interface{})
	if job["image"] != "golang:1.21" {
		t.Errorf("expected image from included template, got %v", job["image"])
	}
}

func TestResolveCyclicInclude(t *testing.T) {
	dir := t.TempDir()
	a := []byte(`
include:
  - b.yml
`)
	b := []byte(`
include:
  - a.yml
`)
	if err := os.WriteFile(filepath.Join(dir, "a.yml"), a, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.yml"), b, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Resolve(a, dir)
	if err == nil {
		t.Error("expected cyclic include error")
	}
}
