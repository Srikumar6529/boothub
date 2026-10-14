package bootdev

import "testing"

func TestIsSubmitInvocation(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"submit", "uuid"}, true},
		{[]string{"run", "-s", "uuid"}, true},
		{[]string{"run", "--submit"}, true},
		{[]string{"run", "uuid"}, false},
		{[]string{"login"}, false},
	}
	for _, tc := range cases {
		if got := isSubmitInvocation(tc.args); got != tc.want {
			t.Fatalf("args=%v got=%v want=%v", tc.args, got, tc.want)
		}
	}
}

func TestPassed(t *testing.T) {
	if !Passed(RunResult{ExitCode: 0, Stdout: "nice work"}) {
		t.Fatal("expected pass")
	}
	if Passed(RunResult{ExitCode: 1}) {
		t.Fatal("expected fail on exit")
	}
	if Passed(RunResult{ExitCode: 0, Stdout: "Submission failed"}) {
		t.Fatal("expected fail on needle")
	}
}

func TestExtractLessonID(t *testing.T) {
	id := extractLessonID([]string{"submit", "abc-123"})
	if id != "abc-123" {
		t.Fatalf("got %s", id)
	}
	id = extractLessonID([]string{"run", "-s", "xyz"})
	if id != "xyz" {
		t.Fatalf("got %s", id)
	}
}
