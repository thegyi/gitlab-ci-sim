package variables

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Context holds all CI variables available during pipeline execution.
type Context struct {
	Vars     map[string]string
	Declared map[string]bool
	Masked   map[string]bool
}

// Get retrieves a variable value by name.
func (c *Context) Get(name string) string {
	return c.Vars[name]
}

// Set sets a variable value.
func (c *Context) Set(name, value string) {
	c.Vars[name] = value
}

// IsDeclared reports whether the variable is intended to be passed to jobs.
func (c *Context) IsDeclared(name string) bool {
	return c.Declared[name]
}

// IsMasked reports whether the variable is marked masked.
func (c *Context) IsMasked(name string) bool {
	return c.Masked[name]
}

var expandRe = regexp.MustCompile(`\$\{(\w+)\}|\$(\w+)`)

// Expand performs variable substitution in a string.
func (c *Context) Expand(s string) string {
	return expandRe.ReplaceAllStringFunc(s, func(match string) string {
		var name string
		if strings.HasPrefix(match, "${") {
			name = match[2 : len(match)-1]
		} else {
			name = match[1:]
		}
		if v, ok := c.Vars[name]; ok {
			return v
		}
		return match
	})
}

// With returns a new context with the given variables merged on top.
func (c *Context) With(m map[string]string) *Context {
	if m == nil || len(m) == 0 {
		return c
	}
	n := &Context{
		Vars:     make(map[string]string, len(c.Vars)+len(m)),
		Declared: make(map[string]bool, len(c.Declared)+len(m)),
		Masked:   make(map[string]bool, len(c.Masked)+len(m)),
	}
	for k, v := range c.Vars {
		n.Vars[k] = v
	}
	for k, v := range c.Declared {
		n.Declared[k] = v
	}
	for k, v := range c.Masked {
		n.Masked[k] = v
	}
	for k, v := range m {
		n.Vars[k] = v
		n.Declared[k] = true
	}
	return n
}

// MissingValues scans the given strings for $VAR or ${VAR} references and
// returns the names of variables that are not defined or are empty.
func (c *Context) MissingValues(ss ...string) []string {
	seen := make(map[string]bool)
	var missing []string
	for _, s := range ss {
		for _, m := range expandRe.FindAllStringSubmatch(s, -1) {
			name := m[1]
			if name == "" {
				name = m[2]
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			if v, ok := c.Vars[name]; !ok || v == "" {
				missing = append(missing, name)
			}
		}
	}
	return missing
}

// Build creates a variable context from the local git state, top-level CI variables, and overrides.
func Build(branch string, configVars map[string]string, configMasked map[string]bool, overrides []string) (*Context, error) {
	ctx := &Context{
		Vars:     make(map[string]string),
		Declared: make(map[string]bool),
		Masked:   make(map[string]bool),
	}

	// Git-derived variables
	if branch == "" {
		branch = gitOutput("rev-parse", "--abbrev-ref", "HEAD")
	}
	sha := gitOutput("rev-parse", "HEAD")
	shortSha := gitOutput("rev-parse", "--short", "HEAD")
	remoteName := gitOutput("config", "--get", fmt.Sprintf("branch.%s.remote", branch))
	if remoteName == "" {
		remoteName = "origin"
	}
	remoteURL := gitOutput("remote", "get-url", remoteName)
	repoDir, _ := os.Getwd()

	// Predefined GitLab CI variables
	ctx.Vars["CI"] = "true"
	ctx.Vars["CI_SERVER"] = "yes"
	ctx.Vars["CI_PIPELINE_SOURCE"] = "push"
	ctx.Vars["CI_COMMIT_BRANCH"] = branch
	ctx.Vars["CI_COMMIT_REF_NAME"] = branch
	ctx.Vars["CI_COMMIT_REF_SLUG"] = slugify(branch)
	ctx.Vars["CI_COMMIT_SHA"] = sha
	ctx.Vars["CI_COMMIT_SHORT_SHA"] = shortSha
	ctx.Vars["CI_DEFAULT_BRANCH"] = getDefaultBranch()
	ctx.Vars["CI_PROJECT_DIR"] = "/builds/project"
	ctx.Vars["CI_PROJECT_NAME"] = filepath.Base(repoDir)
	ctx.Vars["CI_PROJECT_PATH"] = extractProjectPath(remoteURL)
	ctx.Vars["CI_REPOSITORY_URL"] = remoteURL
	ctx.Vars["GITLAB_CI"] = "true"

	for k := range ctx.Vars {
		ctx.Declared[k] = true
	}

	// Apply top-level CI variables from the config (lower priority than job and CLI)
	for k, v := range configVars {
		ctx.Vars[k] = v
		ctx.Declared[k] = true
		if configMasked[k] {
			ctx.Masked[k] = true
		}
	}

	// Apply overrides
	for _, ov := range overrides {
		parts := strings.SplitN(ov, "=", 2)
		if len(parts) == 2 {
			ctx.Vars[parts[0]] = parts[1]
			ctx.Declared[parts[0]] = true
		}
	}

	return ctx, nil
}

func gitOutput(args ...string) string {
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func getDefaultBranch() string {
	// Try to determine the default branch
	branch := gitOutput("symbolic-ref", "refs/remotes/origin/HEAD")
	if branch != "" {
		parts := strings.Split(branch, "/")
		return parts[len(parts)-1]
	}
	return "main"
}

func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, "_", "-")
	return s
}

func extractProjectPath(remoteURL string) string {
	// Handle git@host:group/project.git and https://host/group/project.git
	url := remoteURL
	url = strings.TrimSuffix(url, ".git")
	if idx := strings.Index(url, ":"); idx > 0 && !strings.Contains(url[:idx], "/") {
		return url[idx+1:]
	}
	if idx := strings.Index(url, "://"); idx > 0 {
		rest := url[idx+3:]
		if slashIdx := strings.Index(rest, "/"); slashIdx > 0 {
			return rest[slashIdx+1:]
		}
	}
	return filepath.Base(url)
}
