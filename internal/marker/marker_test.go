package marker

import (
	"strings"
	"testing"
	"time"
)

func TestSanitizeLessonID(t *testing.T) {
	ok := map[string]string{
		"abc-123":    "abc-123",
		"@abc-123":   "abc-123",
		"lesson_1.2": "lesson_1.2",
		"A":          "A",
		"a/b":        "a-b", // slashes are normalized, never kept
		"a\\b":       "a-b",
	}
	for in, want := range ok {
		got, err := SanitizeLessonID(in)
		if err != nil {
			t.Fatalf("SanitizeLessonID(%q) errored: %v", in, err)
		}
		if got != want {
			t.Fatalf("SanitizeLessonID(%q) = %q, want %q", in, got, want)
		}
	}
	bad := []string{"", "..", ".hidden", "a b"}
	for _, id := range bad {
		if got, err := SanitizeLessonID(id); err == nil {
			t.Fatalf("SanitizeLessonID(%q) = %q, want error", id, got)
		}
	}
}

func TestSafeAbsRejectsTraversal(t *testing.T) {
	for _, rel := range []string{"../evil.json", "/etc/passwd", "a/../../b.json"} {
		if got, err := SafeAbs("/repo", rel); err == nil {
			t.Fatalf("SafeAbs(%q) = %q, want error", rel, got)
		}
	}
	got, err := SafeAbs("/repo", "submissions/2026-05-01/x.json")
	if err != nil || !strings.HasPrefix(got, "/repo/submissions/") {
		t.Fatalf("SafeAbs valid = %q, %v", got, err)
	}
}

func TestEncodeRoundTrip(t *testing.T) {
	m := New("lesson-1", TriggerManual, time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC))
	data, err := Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "{\n") || !strings.HasSuffix(string(data), "\n") {
		t.Fatalf("unexpected encoding: %q", data)
	}
}
