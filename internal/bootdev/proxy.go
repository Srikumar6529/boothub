package bootdev

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
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

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &stdoutBuf)
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)

	err := cmd.Run()
	result.Stdout = stdoutBuf.String()
	result.Stderr = stderrBuf.String()

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
			if a == "-s" || a == "--submit" {
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

func extractLessonID(args []string) string {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		if a == "submit" || a == "run" {
			continue
		}
		// UUID-ish or any non-flag token after the command.
		return a
	}
	return ""
}

// Passed prefers false negatives over false positives.
func Passed(r RunResult) bool {
	if r.ExitCode != 0 {
		return false
	}
	combined := strings.ToLower(r.Stdout + "\n" + r.Stderr)
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
