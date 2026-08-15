package resolver

import (
	"fmt"
	"net/http"
	"net/http/httptest"
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

func TestResolveNestedExtends(t *testing.T) {
	yml := []byte(`
.base:
  image: golang:1.21

.template:
  extends: .base
  before_script:
    - echo setup

build_job:
  extends: .template
  stage: build
  script:
    - go build
`)

	m := resolveMap(t, yml, "")
	job := m["build_job"].(map[string]interface{})
	if job["image"] != "golang:1.21" {
		t.Errorf("expected image from .base, got %v", job["image"])
	}
	before := job["before_script"].([]interface{})
	if len(before) != 1 || before[0] != "echo setup" {
		t.Errorf("expected before_script from .template, got %v", before)
	}
}

func TestResolveReferenceScalar(t *testing.T) {
	yml := []byte(`
stages:
  - build

.template:
  variables:
    ENV: prod

build_job:
  stage: build
  script: !reference [.template, variables, ENV]
`)

	m := resolveMap(t, yml, "")
	job := m["build_job"].(map[string]interface{})
	if job["script"] != "prod" {
		t.Errorf("expected scalar reference 'prod', got %v", job["script"])
	}
}

func TestResolveIncludeMergesVariablesAndStages(t *testing.T) {
	dir := t.TempDir()
	main := []byte(`
include:
  - common.yml

build_job:
  stage: build
  script:
    - echo $DEPLOY_ENV
`)
	common := []byte(`
stages:
  - build
  - test

variables:
  CI_REGISTRY: registry.example.com
  DEPLOY_ENV: staging
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

	stages := m["stages"].([]interface{})
	if len(stages) != 2 {
		t.Errorf("expected 2 merged stages, got %d", len(stages))
	}
	vars := m["variables"].(map[string]interface{})
	if vars["CI_REGISTRY"] != "registry.example.com" {
		t.Errorf("expected CI_REGISTRY from include, got %v", vars["CI_REGISTRY"])
	}
	if vars["DEPLOY_ENV"] != "staging" {
		t.Errorf("expected DEPLOY_ENV from include, got %v", vars["DEPLOY_ENV"])
	}
}

func TestResolveMissingInclude(t *testing.T) {
	dir := t.TempDir()
	yml := []byte(`
include:
  - missing.yml
`)
	_, err := Resolve(yml, dir)
	if err == nil {
		t.Error("expected error for missing include file")
	}
}

func TestResolveIncludeRemote(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`
stages:
  - build

.template:
  image: remote:latest
`))
	}))
	defer server.Close()

	yml := []byte(fmt.Sprintf(`
include:
  remote: %s

build_job:
  stage: build
  extends: .template
  script:
    - echo build
`, server.URL))

	m := resolveMap(t, yml, "")
	job := m["build_job"].(map[string]interface{})
	if job["image"] != "remote:latest" {
		t.Errorf("expected image from remote, got %v", job["image"])
	}
}

func TestResolveIncludeMapping(t *testing.T) {
	dir := t.TempDir()
	main := []byte(`
include:
  local: common.yml

build_job:
  stage: build
  script:
    - echo
`)
	common := []byte(`
.template:
  image: common:latest
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
		t.Error("include key should be removed")
	}
}

func TestResolveInvalidInclude(t *testing.T) {
	dir := t.TempDir()
	yml := []byte(`
include:
  - 123
`)
	_, err := Resolve(yml, dir)
	if err == nil {
		t.Error("expected error for invalid include item")
	}
}

func TestResolveReferenceInvalid(t *testing.T) {
	yml := []byte(`
stages:
  - build

build:
  script: !reference [missing, path]
`)
	_, err := Resolve(yml, "")
	if err != nil {
		t.Fatalf("Resolve should not fail on invalid !reference, got: %v", err)
	}
}

func TestProjectURL(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"group/project", "https://gitlab.com/group/project.git"},
		{"https://gitlab.example.com/group/project", "https://gitlab.example.com/group/project"},
		{"git@gitlab.com:group/project", "git@gitlab.com:group/project"},
	}
	for _, c := range cases {
		got := projectURL(c.input)
		if got != c.want {
			t.Errorf("projectURL(%q) = %q, want %q", c.input, got, c.want)
		}
	}
	os.Setenv("CI_SERVER_URL", "https://selfhosted.example.com")
	defer os.Unsetenv("CI_SERVER_URL")
	if got := projectURL("group/project"); got != "https://selfhosted.example.com/group/project.git" {
		t.Errorf("projectURL with env: %q", got)
	}
}

func TestResolveExtendsMissing(t *testing.T) {
	yml := []byte(`
stages:
  - build

build_job:
  stage: build
  extends: .missing
  script:
    - echo
`)
	m := resolveMap(t, yml, "")
	job := m["build_job"].(map[string]interface{})
	if _, ok := job["extends"]; ok {
		t.Error("extends should be removed even if source missing")
	}
}

func TestResolveIncludeScalar(t *testing.T) {
	dir := t.TempDir()
	main := []byte(`
include: common.yml

build_job:
  stage: build
  extends: .template
  script:
    - echo
`)
	common := []byte(`
stages:
  - build

.template:
  image: common:latest
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
	job := m["build_job"].(map[string]interface{})
	if job["image"] != "common:latest" {
		t.Errorf("expected image from scalar include, got %v", job["image"])
	}
}

func TestResolveRemoteIncludeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	yml := []byte(fmt.Sprintf(`
include:
  remote: %s
`, server.URL))
	_, err := Resolve(yml, "")
	if err == nil {
		t.Error("expected error for 404 remote include")
	}
}

func TestResolveScalarRoot(t *testing.T) {
	resolved, err := Resolve([]byte("hello"), "")
	if err != nil {
		t.Fatalf("scalar root: %v", err)
	}
	if string(resolved) != "hello" {
		t.Errorf("expected original data, got %q", string(resolved))
	}
}

func TestResolveInvalidYAML(t *testing.T) {
	_, err := Resolve([]byte("}{invalid"), "")
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestResolveEmptyIncludeRef(t *testing.T) {
	dir := t.TempDir()
	yml := []byte(`
include:
  random: value

build_job:
  script:
    - echo
`)
	m := resolveMap(t, yml, dir)
	if _, ok := m["build_job"]; !ok {
		t.Error("build_job should exist despite empty include ref")
	}
}

func TestResolveProjectIncludeWithoutFile(t *testing.T) {
	dir := t.TempDir()
	yml := []byte(`
include:
  project: group/project
`)
	_, err := Resolve(yml, dir)
	if err == nil {
		t.Error("expected error for project include without file")
	}
}

func TestResolveEmptyDoc(t *testing.T) {
	resolved, err := Resolve([]byte{}, "")
	if err != nil {
		t.Fatalf("empty doc: %v", err)
	}
	if len(resolved) != 0 {
		t.Errorf("expected empty, got %q", resolved)
	}
}
