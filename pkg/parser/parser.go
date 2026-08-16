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

// Image represents the image: configuration, which may be a string or a mapping.
type Image struct {
	Name       string   `yaml:"name"`
	Entrypoint []string `yaml:"entrypoint"`
}

// UnmarshalYAML supports both scalar strings and { name, entrypoint } mappings.
func (i *Image) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err == nil {
		i.Name = s
		return nil
	}
	type raw Image
	return unmarshal((*raw)(i))
}

// JobDefaults represents the `default:` section.
type JobDefaults struct {
	Image        Image     `yaml:"image"`
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

// Parallel represents a parallel: job configuration.
type Parallel struct {
	Scalar int                   `yaml:"scalar"`
	Matrix []map[string][]string `yaml:"matrix"`
}

// UnmarshalYAML supports both scalar counts and full mapping forms.
func (p *Parallel) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var n int
	if err := unmarshal(&n); err == nil {
		p.Scalar = n
		return nil
	}
	type raw Parallel
	return unmarshal((*raw)(p))
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

// AllowFailure supports allow_failure: true or allow_failure: { exit_codes: [...] }.
type AllowFailure struct {
	Value     bool
	ExitCodes []int
}

// UnmarshalYAML supports boolean and mapping forms.
func (a *AllowFailure) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var b bool
	if err := unmarshal(&b); err == nil {
		a.Value = b
		return nil
	}
	var raw struct {
		ExitCodes []int `yaml:"exit_codes"`
	}
	if err := unmarshal(&raw); err == nil {
		a.ExitCodes = raw.ExitCodes
		return nil
	}
	return fmt.Errorf("allow_failure must be a boolean or a mapping with exit_codes")
}

// Need represents a single dependency entry in needs:.
// It can be a plain job name or an object with options.
type Need struct {
	Job       string `yaml:"job"`
	Optional  bool   `yaml:"optional"`
	Artifacts *bool  `yaml:"artifacts"`
}

// UnmarshalYAML supports a string job name or a mapping with job/optional/artifacts.
func (n *Need) UnmarshalYAML(unmarshal func(interface{}) error) error {
	defaultArtifacts := true
	var s string
	if err := unmarshal(&s); err == nil {
		n.Job = s
		n.Artifacts = &defaultArtifacts
		return nil
	}
	type raw Need
	var r raw
	if err := unmarshal(&r); err == nil {
		*n = Need(r)
		if n.Artifacts == nil {
			n.Artifacts = &defaultArtifacts
		}
		return nil
	}
	return fmt.Errorf("needs item must be a string or mapping")
}

// Needs is a slice of Need entries.
type Needs []Need

// Names returns the job names of each Need.
func (n Needs) Names() []string {
	names := make([]string, 0, len(n))
	for _, need := range n {
		names = append(names, need.Job)
	}
	return names
}

// Job represents a single CI job.
type Job struct {
	Name         string
	Stage        string              `yaml:"stage"`
	Image        Image               `yaml:"image"`
	Script       []string            `yaml:"script"`
	BeforeScript []string            `yaml:"before_script"`
	AfterScript  []string            `yaml:"after_script"`
	Variables    map[string]Variable `yaml:"variables"`
	Rules        []Rule              `yaml:"rules"`
	Only         *OnlyExcept         `yaml:"only"`
	Except       *OnlyExcept         `yaml:"except"`
	Needs        Needs               `yaml:"needs"`
	Dependencies []string            `yaml:"dependencies"`
	Artifacts    *Artifacts          `yaml:"artifacts"`
	Cache        *Cache              `yaml:"cache"`
	Services     []Service           `yaml:"services"`
	Extends      interface{}         `yaml:"extends"`
	AllowFailure AllowFailure        `yaml:"allow_failure"`
	When         string              `yaml:"when"`
	StartIn      string              `yaml:"start_in"`
	Parallel     *Parallel           `yaml:"parallel"`
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

// CacheKey represents a cache key, which may be a scalar or a mapping.
type CacheKey struct {
	Prefix string   `yaml:"prefix"`
	Files  []string `yaml:"files"`
}

// UnmarshalYAML supports cache.key as a string or { prefix, files } mapping.
func (k *CacheKey) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err == nil {
		k.Prefix = s
		return nil
	}
	type raw CacheKey
	return unmarshal((*raw)(k))
}

// Cache represents the cache: configuration.
type Cache struct {
	Key       *CacheKey `yaml:"key"`
	Paths     []string  `yaml:"paths"`
	Policy    string    `yaml:"policy"`
	Untracked bool      `yaml:"untracked"`
	When      string    `yaml:"when"`
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
