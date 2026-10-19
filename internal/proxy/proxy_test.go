package proxy

import "testing"

func TestPassed(t *testing.T) {
	if !Passed(Result{ExitCode: 0, Stdout: "Lesson complete!"}) {
		t.Fatal("clean output should pass")
	}
	if Passed(Result{ExitCode: 1, Stdout: "Lesson complete!"}) {
		t.Fatal("nonzero exit must fail")
	}
	if Passed(Result{ExitCode: 0, Stdout: "Tests failed, try again"}) {
		t.Fatal("failure text must fail even with exit 0")
	}
	if !Passed(Result{ExitCode: 0, Stdout: "\x1b[32mSUCCESS\x1b[0m"}) {
		t.Fatal("ANSI-decorated success should pass")
	}
}

func TestIsSubmitInvocation(t *testing.T) {
	yes := [][]string{
		{"submit"},
		{"submit", "abc"},
		{"run", "-s"},
		{"run", "--submit", "abc"},
	}
	for _, args := range yes {
		if !isSubmitInvocation(args) {
			t.Fatalf("isSubmitInvocation(%v) = false, want true", args)
		}
	}
	no := [][]string{
		{},
		{"run"},
		{"submit", "--help"},
		{"submit", "-h"},
		{"run", "--version"},
		{"run", "--help", "-s"},
	}
	for _, args := range no {
		if isSubmitInvocation(args) {
			t.Fatalf("isSubmitInvocation(%v) = true, want false", args)
		}
	}
}

func TestLessonIDFromOutput(t *testing.T) {
	r := Result{Stdout: "\x1b[33mSubmitting lesson 0d5e6f7a-1234-4abc-9def-0123456789ab\x1b[0m"}
	if got := LessonIDFromOutput(r); got != "0d5e6f7a-1234-4abc-9def-0123456789ab" {
		t.Fatalf("got %q", got)
	}
	if got := LessonIDFromOutput(Result{Stdout: "no id here"}); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestExtractLessonID(t *testing.T) {
	if got := extractLessonID([]string{"submit", "--course", "3", "0d5e6f7a-1234-4abc-9def-0123456789ab"}); got != "0d5e6f7a-1234-4abc-9def-0123456789ab" {
		t.Fatalf("got %q", got)
	}
	if got := extractLessonID([]string{"submit", "--course", "3"}); got != "3" {
		t.Fatalf("fallback got %q", got)
	}
}
