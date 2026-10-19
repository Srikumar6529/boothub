package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// captureLimit bounds how much child output we keep for pass/fail detection.
const captureLimit = 256 << 10

type proxyResult struct {
	ExitCode  int
	Stdout    string
	Stderr    string
	LessonID  string
	Submitted bool
	Command   string
}

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

// runProxy runs the bootdev CLI as a child, streaming its TUI to the terminal.
func runProxy(binary string, args []string) (proxyResult, error) {
	bin := binary
	if bin == "" {
		path, err := exec.LookPath("bootdev")
		if err != nil {
			return proxyResult{}, fmt.Errorf("bootdev not found on PATH; install it with: go install github.com/bootdotdev/bootdev@latest")
		}
		bin = path
	}

	result := proxyResult{
		LessonID:  extractLessonID(args),
		Submitted: isSubmitInvocation(args),
		Command:   commandName(args),
	}

	cmd := exec.Command(bin, args...)
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
	return result, nil
}

func isSubmitInvocation(args []string) bool {
	if len(args) == 0 {
		return false
	}
	// `--help`/`--version` exit 0 without submitting anything; never sync for
	// those, or a plain `boothub submit --help` would create a bogus commit.
	for _, a := range args {
		switch a {
		case "-h", "--help", "--version":
			return false
		}
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

var uuidSearchRe = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// lessonIDFromOutput finds the first lesson UUID in the bootdev output, used as
// a fallback when the id is not a command-line argument (e.g. `bootdev submit`
// with no UUID submits the next lesson).
func lessonIDFromOutput(r proxyResult) string {
	return uuidSearchRe.FindString(stripANSI(r.Stdout + "\n" + r.Stderr))
}

// extractLessonID prefers a UUID-looking positional argument; otherwise the
// first non-flag token. This avoids mistaking a flag value (e.g. `--course 1`)
// for the lesson id.
func extractLessonID(args []string) string {
	var fallback string
	for _, a := range args {
		if strings.HasPrefix(a, "-") || a == "submit" || a == "run" {
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

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

// proxyPassed prefers false negatives over false positives.
func proxyPassed(r proxyResult) bool {
	if r.ExitCode != 0 {
		return false
	}
	combined := strings.ToLower(stripANSI(r.Stdout + "\n" + r.Stderr))
	for _, needle := range []string{
		"submission failed", "failed to submit", "lesson failed",
		"tests failed", "incorrect", "not quite", "try again",
	} {
		if strings.Contains(combined, needle) {
			return false
		}
	}
	return true
}

// cmdProxy runs the bootdev CLI and syncs a marker commit when it passes.
func cmdProxy(command string, args []string) error {
	cfg, cfgErr := loadConfig() // config is only needed for syncing

	result, err := runProxy(cfg.BootdevBinary, append([]string{command}, args...))
	if err != nil {
		return err
	}
	if !result.Submitted || !proxyPassed(result) {
		os.Exit(result.ExitCode)
	}
	if cfgErr != nil {
		fmt.Fprintf(os.Stderr, "boothub: bootdev passed, but sync skipped: %v\n", cfgErr)
		os.Exit(result.ExitCode)
	}

	trigger := triggerCLISubmit
	if result.Command == "run" {
		trigger = triggerCLIRunSubmit
	}

	lessonID := result.LessonID
	if lessonID == "" {
		lessonID = lessonIDFromOutput(result)
	}

	a := app{cfg: cfg}
	res, syncErr := a.syncOne(syncRequest{
		LessonID: lessonID,
		Trigger:  trigger,
		When:     time.Now(),
	})
	switch {
	case syncErr != nil:
		fmt.Fprintf(os.Stderr, "boothub: warning: GitHub sync failed: %v\n", syncErr)
	case res.Skipped:
		fmt.Fprintf(os.Stderr, "boothub: %s\n", res.Message)
	default:
		fmt.Fprintf(os.Stderr, "boothub: synced %s\n", res.RelPath)
	}

	os.Exit(result.ExitCode)
	return nil
}
