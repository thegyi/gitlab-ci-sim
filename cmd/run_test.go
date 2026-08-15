package cmd

import (
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/thegyi/gitlab-ci-sim/pkg/pipeline"
)

func TestParseSelection(t *testing.T) {
	cases := []struct {
		input string
		max   int
		want  []int
	}{
		{"1,3,5", 5, []int{1, 3, 5}},
		{"1-3", 5, []int{1, 2, 3}},
		{"1-3,5", 5, []int{1, 2, 3, 5}},
		{"5-1", 5, []int{1, 2, 3, 4, 5}},
		{"1-10", 5, []int{1, 2, 3, 4, 5}},
		{"", 3, nil},
		{"2,2,2", 3, []int{2}},
	}
	for _, c := range cases {
		got := parseSelection(c.input, c.max)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("parseSelection(%q, %d) = %v, want %v", c.input, c.max, got, c.want)
		}
	}
}

func TestSelectJobs(t *testing.T) {
	pipe := &pipeline.Pipeline{
		Stages: []*pipeline.Stage{
			{Name: "build", Jobs: []*pipeline.PipelineJob{
				{Name: "job_a"},
				{Name: "job_b"},
			}},
			{Name: "test", Jobs: []*pipeline.PipelineJob{
				{Name: "job_c"},
			}},
		},
	}

	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdin = r
	fmt.Fprint(w, "1,3\n")
	w.Close()
	defer func() { os.Stdin = oldStdin }()

	got, err := selectJobs(pipe)
	if err != nil {
		t.Fatalf("selectJobs failed: %v", err)
	}
	want := []string{"job_a", "job_c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}
