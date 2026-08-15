package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/thegyi/gitlab-ci-sim/pkg/resolver"
	"gopkg.in/yaml.v3"
)

// Config represents the fully resolved .gitlab-ci.yml configuration.
type Config struct {
	Stages    []string
	Jobs      map[string]*Job
	Default   *JobDefaults
	Workflow  *Workflow
	Variables map[string]Variable
}

// JobDefaults represents the `default:` section.
type JobDefaults struct {
	Image        string    `yaml:"image"`
	BeforeScript []string  `yaml:"before_script"`
	AfterScript  []string  `yaml:"after_script"`
	Services     []Service `yaml:"services"`
}

// Trigger represents a trigger: job configuration.
type Trigger struct {
	Project  string `yaml:"project"`
	Strategy string `yaml:"strategy"`
	Branch   string `yaml:"branch"`
}

// UnmarshalYAML supports both scalar project paths and full mapping forms.
func (t *Trigger) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err == nil {
		t.Project = s
		return nil
	}
	type raw Trigger
	return unmarshal((*raw)(t))
}

// Retry represents a retry: job configuration.
type Retry struct {
	Max  int      `yaml:"max"`
	When []string `yaml:"when"`
}

// UnmarshalYAML supports both scalar counts and full mapping forms.
func (r *Retry) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var n int
	if err := unmarshal(&n); err == nil {
		r.Max = n
		return nil
	}
	type raw Retry
	return unmarshal((*raw)(r))
}

// Job represents a single CI job.
type Job struct {
	Name         string
	Stage        string              `yaml:"stage"`
	Image        string              `yaml:"image"`
	Script       []string            `yaml:"script"`
	BeforeScript []string            `yaml:"before_script"`
	AfterScript  []string            `yaml:"after_script"`
	Variables    map[string]Variable `yaml:"variables"`
	Rules        []Rule              `yaml:"rules"`
	Only         *OnlyExcept         `yaml:"only"`
	Except       *OnlyExcept         `yaml:"except"`
	Needs        []string            `yaml:"needs"`
	Dependencies []string            `yaml:"dependencies"`
	Artifacts    *Artifacts          `yaml:"artifacts"`
	Cache        *Cache              `yaml:"cache"`
	Services     []Service           `yaml:"services"`
	Extends      interface{}         `yaml:"extends"`
	AllowFailure bool                `yaml:"allow_failure"`
	When         string              `yaml:"when"`
	Parallel     interface{}         `yaml:"parallel"`
	Tags         []string            `yaml:"tags"`
	Retry        *Retry              `yaml:"retry"`
	Trigger      *Trigger            `yaml:"trigger"`
}

// Rule represents a single entry in the rules: array.
type Rule struct {
	If        string            `yaml:"if"`
	Changes   []string          `yaml:"changes"`
	Exists    []string          `yaml:"exists"`
	When      string            `yaml:"when"`
	Variables map[string]string `yaml:"variables"`
}

// OnlyExcept represents the only:/except: configuration.
type OnlyExcept struct {
	Refs      []string `yaml:"refs"`
	Variables []string `yaml:"variables"`
	Changes   []string `yaml:"changes"`
}

// Variable represents a CI variable and its attributes.
type Variable struct {
	Value  string `yaml:"value"`
	Masked bool   `yaml:"masked"`
}

// UnmarshalYAML supports both scalar strings and mapping forms.
func (v *Variable) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err == nil {
		v.Value = s
		return nil
	}
	type raw Variable
	return unmarshal((*raw)(v))
}

// Artifacts represents the artifacts: configuration.
type Artifacts struct {
	Paths   []string               `yaml:"paths"`
	Expire  string                 `yaml:"expire_in"`
	When    string                 `yaml:"when"`
	Reports map[string]interface{} `yaml:"reports"`
}

// Cache represents the cache: configuration.
type Cache struct {
	Key    string   `yaml:"key"`
	Paths  []string `yaml:"paths"`
	Policy string   `yaml:"policy"`
}

// Service represents a Docker service (linked container).
type Service struct {
	Name    string   `yaml:"name"`
	Alias   string   `yaml:"alias"`
	Command []string `yaml:"command"`
}

// Workflow represents the workflow: section.
type Workflow struct {
	Rules []Rule `yaml:"rules"`
}

// ParseFile reads and parses a .gitlab-ci.yml file.
func ParseFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	resolved, err := resolver.Resolve(data, filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", path, err)
	}
	return Parse(resolved)
}

// Parse parses raw YAML bytes into a Config.
func Parse(data []byte) (*Config, error) {
	// First pass: unmarshal into a generic map to extract known keys
	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing YAML: %w", err)
	}

	config := &Config{
		Jobs: make(map[string]*Job),
	}

	// Extract stages
	if stages, ok := raw["stages"]; ok {
		if stageList, ok := stages.([]interface{}); ok {
			for _, s := range stageList {
				config.Stages = append(config.Stages, fmt.Sprintf("%v", s))
			}
		}
	}
	if len(config.Stages) == 0 {
		config.Stages = []string{"build", "test", "deploy"}
	}

	// Extract default: section
	if d, ok := raw["default"]; ok {
		if dm, ok := d.(map[string]interface{}); ok {
			data, _ := yaml.Marshal(dm)
			var defaults JobDefaults
			if err := yaml.Unmarshal(data, &defaults); err == nil {
				config.Default = &defaults
			}
		}
	}

	// Extract top-level variables
	if v, ok := raw["variables"]; ok {
		if vm, ok := v.(map[string]interface{}); ok {
			data, _ := yaml.Marshal(vm)
			config.Variables = make(map[string]Variable)
			_ = yaml.Unmarshal(data, &config.Variables)
		}
	}

	// Extract workflow: section
	if w, ok := raw["workflow"]; ok {
		if wm, ok := w.(map[string]interface{}); ok {
			data, _ := yaml.Marshal(wm)
			var workflow Workflow
			if err := yaml.Unmarshal(data, &workflow); err == nil {
				config.Workflow = &workflow
			}
		}
	}

	// Reserved top-level keys (not jobs)
	reserved := map[string]bool{
		"stages": true, "default": true, "variables": true,
		"include": true, "workflow": true, "image": true,
		"before_script": true, "after_script": true, "services": true,
		"cache": true,
	}

	// Extract jobs
	for name, value := range raw {
		if reserved[name] || strings.HasPrefix(name, ".") {
			continue
		}
		jobData, err := yaml.Marshal(value)
		if err != nil {
			continue
		}
		var job Job
		if err := yaml.Unmarshal(jobData, &job); err != nil {
			continue
		}
		job.Name = name
		if job.Stage == "" {
			job.Stage = "test"
		}
		config.Jobs[name] = &job
	}

	return config, nil
}
