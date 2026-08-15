package term

import (
	"strings"
	"testing"
)

func TestColorHelpers(t *testing.T) {
	cases := []struct {
		fn     func(string) string
		in     string
		prefix string
	}{
		{Green, "ok", "\033[32m"},
		{Red, "fail", "\033[31m"},
		{Yellow, "warn", "\033[33m"},
		{Cyan, "info", "\033[36m"},
		{Bold, "bold", "\033[1m"},
	}
	for _, c := range cases {
		got := c.fn(c.in)
		if !strings.HasPrefix(got, c.prefix) || !strings.HasSuffix(got, reset) {
			t.Errorf("unexpected output for %q: %q", c.in, got)
		}
	}
}
