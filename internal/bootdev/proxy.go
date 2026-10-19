package bootdev

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

type RunRequest struct {
	Binary string
	Args   []string
}

type RunResult struct {
	ExitCode  int
	Stdout    string
	Stderr    string
	LessonID  string
	Submitted bool
	Command   string // "submit" or "run"
}

// captureLimit is how much of the child's output we keep in memory for
// pass/fail detection. A TUI can redraw constantly; there is no reason to keep
// the whole stream.
const captureLimit = 256 << 10

type tailWriter struct {
	buf []byte
	max int
}

func (t *tailWriter) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(p), nil
}

func (t *tailWriter) String() string { return string(t.buf) }

func Proxy(req RunRequest) (RunResult, error) {
	bin := req.Binary
	if bin == "" {
		path, err := exec.LookPath("bootdev")
		if err != nil {
			return RunResult{}, fmt.Errorf("bootdev not found on PATH; install it with: go install github.com/bootdotdev/bootdev@latest")
		}
		bin = path
	}

	result := RunResult{
		LessonID:  extractLessonID(req.Args),
		Submitted: isSubmitInvocation(req.Args),
		Command:   commandName(req.Args),
	}

	cmd := exec.Command(bin, req.Args...)
	cmd.Stdin = os.Stdin

	stdout := &tailWriter{max: captureLimit}
	stderr := &tailWriter{max: captureLimit}
	cmd.Stdout = io.MultiWriter(os.Stdout, stdout)
	cmd.Stderr = io.MultiWriter(os.Stderr, stderr)

	err := cmd.Run()
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()

	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			result.ExitCode = ee.ExitCode()
			return result, nil
		}
		return result, err
	}
	result.ExitCode = 0
	return result, nil
}

func isSubmitInvocation(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if args[0] == "submit" {
		return true
	}
	if args[0] == "run" {
		for _, a := range args[1:] {
			if a == "-s" || a == "--submit" || a == "--submit=true" {
				return true
			}
		}
	}
	return false
}

func commandName(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// extractLessonID prefers a UUID-looking positional argument; otherwise it
// falls back to the first non-flag token. This avoids mistaking the value of a
// flag such as `--course 1` for the lesson id.
func extractLessonID(args []string) string {
	var fallback string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		if a == "submit" || a == "run" {
			continue
		}
		if uuidRe.MatchString(a) {
			return a
		}
		if fallback == "" {
			fallback = a
		}
	}
	return fallback
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// stripANSI removes terminal escape sequences so pass/fail needles are not
// broken up by color codes (e.g. "Try \x1b[31magain\x1b[0m").
func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

// Passed prefers false negatives over false positives.
func Passed(r RunResult) bool {
	if r.ExitCode != 0 {
		return false
	}
	combined := strings.ToLower(stripANSI(r.Stdout + "\n" + r.Stderr))
	failureNeedles := []string{
		"submission failed",
		"failed to submit",
		"lesson failed",
		"tests failed",
		"incorrect",
		"not quite",
		"try again",
	}
	for _, n := range failureNeedles {
		if strings.Contains(combined, n) {
			return false
		}
	}
	return true
}
