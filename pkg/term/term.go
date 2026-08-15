// Package term provides minimal ANSI color helpers for terminal output.
package term

const (
	reset  = "\033[0m"
	red    = "\033[31m"
	green  = "\033[32m"
	yellow = "\033[33m"
	cyan   = "\033[36m"
	bold   = "\033[1m"
)

// Green returns s wrapped in green.
func Green(s string) string { return green + s + reset }

// Red returns s wrapped in red.
func Red(s string) string { return red + s + reset }

// Yellow returns s wrapped in yellow.
func Yellow(s string) string { return yellow + s + reset }

// Cyan returns s wrapped in cyan.
func Cyan(s string) string { return cyan + s + reset }

// Bold returns s wrapped in bold.
func Bold(s string) string { return bold + s + reset }
