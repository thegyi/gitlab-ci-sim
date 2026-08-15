package rules

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Knetic/govaluate"
	"github.com/thegyi/gitlab-ci-sim/pkg/parser"
	"github.com/thegyi/gitlab-ci-sim/pkg/variables"
)

// Result is the outcome of evaluating whether a job should run.
type Result struct {
	Run       bool
	When      string
	Variables map[string]string
}

// ShouldRun evaluates a job's rules/only/except/when to decide if it runs.
func ShouldRun(job *parser.Job, vars *variables.Context, allowManual bool) (*Result, error) {
	jobValues := make(map[string]string)
	for k, v := range job.Variables {
		jobValues[k] = v.Value
	}
	evalCtx := vars.With(jobValues)

	if len(job.Rules) > 0 {
		for _, r := range job.Rules {
			if r.If != "" {
				ok, err := evalIf(r.If, evalCtx)
				if err != nil {
					return nil, fmt.Errorf("job %q rule if: %w", job.Name, err)
				}
				if !ok {
					continue
				}
			}
			if len(r.Changes) > 0 && !changesMatch(r.Changes) {
				continue
			}
			if len(r.Exists) > 0 && !existsMatch(r.Exists) {
				continue
			}
			return buildResult(r.When, r.Variables, allowManual), nil
		}
		return &Result{Run: false}, nil
	}

	if job.Only != nil || job.Except != nil {
		if !evalOnlyExcept(job.Only, job.Except, vars) {
			return &Result{Run: false}, nil
		}
	}

	return buildResult(job.When, nil, allowManual), nil
}

// EvaluateWorkflow evaluates workflow-level rules.
// The first matching rule wins. If no rules match, the pipeline does not run.
func EvaluateWorkflow(workflow *parser.Workflow, vars *variables.Context) (*Result, error) {
	if workflow == nil || len(workflow.Rules) == 0 {
		return &Result{Run: true}, nil
	}
	for _, r := range workflow.Rules {
		if r.If != "" {
			ok, err := evalIf(r.If, vars)
			if err != nil {
				return nil, fmt.Errorf("workflow rule if: %w", err)
			}
			if !ok {
				continue
			}
		}
		if len(r.Changes) > 0 && !changesMatch(r.Changes) {
			continue
		}
		if len(r.Exists) > 0 && !existsMatch(r.Exists) {
			continue
		}
		when := r.When
		if when == "" {
			when = "always"
		}
		return &Result{
			Run:       when != "never",
			When:      when,
			Variables: r.Variables,
		}, nil
	}
	return &Result{Run: false}, nil
}

func buildResult(when string, ruleVars map[string]string, allowManual bool) *Result {
	if when == "" {
		when = "on_success"
	}
	run := true
	if when == "never" {
		run = false
	}
	if when == "manual" && !allowManual {
		run = false
	}
	return &Result{
		Run:       run,
		When:      when,
		Variables: ruleVars,
	}
}

var regexLiteralRe = regexp.MustCompile(`(=~|!~)\s*/([^/]*)/`)
var varRe = regexp.MustCompile(`\$\{[^\}]+\}|\$[A-Za-z_][A-Za-z0-9_]*`)

func evalIf(expr string, vars *variables.Context) (bool, error) {
	// Convert GitLab /pattern/ regex literals to govaluate strings.
	expr = regexLiteralRe.ReplaceAllStringFunc(expr, func(match string) string {
		parts := regexLiteralRe.FindStringSubmatch(match)
		op := parts[1]
		pattern := parts[2]
		return op + " " + quotePattern(pattern)
	})

	// Strip $ from variables so CI_COMMIT_BRANCH becomes a valid identifier.
	expr = varRe.ReplaceAllStringFunc(expr, func(match string) string {
		if strings.HasPrefix(match, "${") {
			return match[2 : len(match)-1]
		}
		return match[1:]
	})

	// GitLab's null is an empty string in a govaluate expression.
	expr = strings.ReplaceAll(expr, "null", `""`)

	params := make(map[string]interface{}, len(vars.Vars))
	for k, v := range vars.Vars {
		params[k] = v
	}

	eval, err := govaluate.NewEvaluableExpression(expr)
	if err != nil {
		return false, fmt.Errorf("parsing if expression %q: %w", expr, err)
	}
	result, err := eval.Evaluate(params)
	if err != nil {
		return false, fmt.Errorf("evaluating if expression %q: %w", expr, err)
	}
	b, ok := result.(bool)
	if !ok {
		return false, fmt.Errorf("if expression %q did not return a boolean", expr)
	}
	return b, nil
}

func quotePattern(pattern string) string {
	// Escape backslashes and quotes so the pattern survives as a string literal.
	s := strings.ReplaceAll(pattern, `\`, `\\`)
	if !strings.Contains(s, `"`) {
		return strconv.Quote(s)
	}
	if !strings.Contains(s, `'`) {
		return "'" + s + "'"
	}
	// Both quote types present: fall back to double quotes and escape them.
	s = strings.ReplaceAll(s, `"`, `\\"`)
	return strconv.Quote(s)
}

func changesMatch(patterns []string) bool {
	changed := changedFiles()
	for _, f := range changed {
		for _, p := range patterns {
			if matched, _ := filepath.Match(p, f); matched {
				return true
			}
			// filepath.Match does not support **; support prefix glob for directories.
			if strings.HasSuffix(p, "/*") && strings.HasPrefix(f, p[:len(p)-1]) {
				return true
			}
		}
	}
	return false
}

func existsMatch(patterns []string) bool {
	for _, p := range patterns {
		matches, _ := filepath.Glob(p)
		if len(matches) > 0 {
			return true
		}
	}
	return false
}

func changedFiles() []string {
	// Try to get the files changed in the current branch.
	out, err := exec.Command("git", "diff", "--name-only", "HEAD~1", "HEAD").Output()
	if err == nil && len(out) > 0 {
		return splitLines(string(out))
	}
	// Fallback to modified files.
	out, err = exec.Command("git", "diff", "--name-only").Output()
	if err == nil {
		return splitLines(string(out))
	}
	return nil
}

func splitLines(s string) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func evalOnlyExcept(only, except *parser.OnlyExcept, vars *variables.Context) bool {
	branch := vars.Get("CI_COMMIT_BRANCH")
	source := vars.Get("CI_PIPELINE_SOURCE")

	if except != nil && exceptMatches(except, branch, source, vars) {
		return false
	}
	if only != nil && !onlyMatches(only, branch, source, vars) {
		return false
	}
	return true
}

// onlyMatches returns true when every present sub-key (refs, variables, changes) matches.
// Within a sub-key the conditions are ORed (e.g. any ref or any variable matches).
func onlyMatches(oe *parser.OnlyExcept, branch, source string, vars *variables.Context) bool {
	if len(oe.Refs) > 0 && !matchRefs(oe.Refs, branch, source) {
		return false
	}
	if len(oe.Variables) > 0 {
		for _, expr := range oe.Variables {
			if !evalVariableCondition(expr, vars) {
				return false
			}
		}
	}
	if len(oe.Changes) > 0 && !changesMatch(oe.Changes) {
		return false
	}
	return true
}

// exceptMatches returns true when any present sub-key matches.
// This means if any ref, variable expression, or file change matches, the job is excluded.
func exceptMatches(oe *parser.OnlyExcept, branch, source string, vars *variables.Context) bool {
	if len(oe.Refs) > 0 && matchRefs(oe.Refs, branch, source) {
		return true
	}
	if len(oe.Variables) > 0 {
		for _, expr := range oe.Variables {
			if evalVariableCondition(expr, vars) {
				return true
			}
		}
	}
	if len(oe.Changes) > 0 && changesMatch(oe.Changes) {
		return true
	}
	return false
}

func evalVariableCondition(expr string, vars *variables.Context) bool {
	expr = strings.TrimSpace(expr)
	// A bare variable reference like VAR, $VAR or ${VAR} is truthy when non-empty.
	name := expr
	if strings.HasPrefix(name, "$") {
		if strings.HasPrefix(name, "${") && strings.HasSuffix(name, "}") {
			name = name[2 : len(name)-1]
		} else {
			name = name[1:]
		}
	}
	if !strings.ContainsAny(name, "=<>!~&|\"") {
		return vars.Get(name) != ""
	}
	ok, err := evalIf(expr, vars)
	if err != nil {
		return false
	}
	return ok
}

func matchRefs(refs []string, branch, source string) bool {
	for _, r := range refs {
		r = strings.TrimSpace(r)
		if r == branch {
			return true
		}
		if r == "merge_requests" && source == "merge_request_event" {
			return true
		}
		if r == "tags" && strings.HasPrefix(branch, "refs/tags/") {
			return true
		}
		if r == "pipelines" && source == "pipeline" {
			return true
		}
		if r == "schedules" && source == "schedule" {
			return true
		}
	}
	return false
}

// MergeVariables combines job-level and rule-level variables with rule variables winning.
func MergeVariables(jobVars, ruleVars map[string]string) map[string]string {
	m := make(map[string]string)
	for k, v := range jobVars {
		m[k] = v
	}
	for k, v := range ruleVars {
		m[k] = v
	}
	return m
}

// FormatVariables returns a deterministic, env-file style string for debugging.
func FormatVariables(vars map[string]string) string {
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s\n", k, vars[k])
	}
	return b.String()
}
