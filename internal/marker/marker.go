package marker

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	Source   = "boothub"
	Version  = 1
	Platform = "boot.dev"

	TriggerCLISubmit    = "cli_submit"
	TriggerCLIRunSubmit = "cli_run_submit"
	TriggerManual       = "manual"
	TriggerBackfill     = "backfill"
	TriggerExtension    = "extension"

	// MessagePrefix is the commit subject every BootHub marker commit starts
	// with. It is used to count marker commits per day without trusting file
	// names (a marker can be rewritten, producing more commits than files).
	MessagePrefix = "boot.dev: complete "
)

type Marker struct {
	Source        string    `json:"source"`
	Version       int       `json:"version"`
	Platform      string    `json:"platform"`
	LessonID      string    `json:"lesson_id"`
	SubmittedAt   time.Time `json:"submitted_at"`
	Trigger       string    `json:"trigger"`
	Note          string    `json:"note,omitempty"`
	ActivityCount int       `json:"activity_count,omitempty"`
}

func New(lessonID, trigger string, when time.Time) Marker {
	return Marker{
		Source:      Source,
		Version:     Version,
		Platform:    Platform,
		LessonID:    lessonID,
		SubmittedAt: when.UTC(),
		Trigger:     trigger,
	}
}

// validID matches lesson ids we are willing to write to disk. It deliberately
// excludes path separators, leading dots and ".." so a lesson id can never
// escape the submissions directory.
var validID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,180}$`)

// SanitizeLessonID cleans a lesson id coming from the CLI or from bootdev's
// output and rejects anything that could be used for path traversal.
func SanitizeLessonID(id string) (string, error) {
	id = strings.TrimPrefix(strings.TrimSpace(id), "@")
	if id == "" {
		return "", fmt.Errorf("lesson id is empty")
	}
	id = strings.ReplaceAll(id, "/", "-")
	id = strings.ReplaceAll(id, "\\", "-")
	if !validID.MatchString(id) {
		return "", fmt.Errorf("invalid lesson id %q", id)
	}
	return id, nil
}

// DayKey is the calendar day of when in loc, as YYYY-MM-DD.
func DayKey(when time.Time, loc *time.Location) string {
	local := when.In(loc)
	return local.Format("2006-01-02")
}

var dayKeyRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// RelPath returns the repo-relative (slash-separated) marker path.
func RelPath(dayKey, lessonID string) string {
	return path.Join("submissions", dayKey, lessonID+".json")
}

func BackfillRelPath(dayKey string, n int) string {
	return RelPath(dayKey, fmt.Sprintf("backfill-%d", n))
}

// AbsPath returns the absolute path for a repo-relative marker path. Callers
// should prefer SafeAbsPath, which rejects paths that escape the repo.
func AbsPath(repoPath, rel string) string {
	return filepath.Join(repoPath, filepath.FromSlash(rel))
}

// SafeAbsPath resolves rel under repoPath and refuses anything that would
// escape it (e.g. "../../tmp/x").
func SafeAbsPath(repoPath, rel string) (string, error) {
	clean := path.Clean(rel)
	if clean == "." || strings.HasPrefix(clean, "../") || clean == ".." || path.IsAbs(clean) {
		return "", fmt.Errorf("unsafe marker path %q", rel)
	}
	if !filepath.IsLocal(filepath.FromSlash(clean)) {
		return "", fmt.Errorf("unsafe marker path %q", rel)
	}
	return filepath.Join(repoPath, filepath.FromSlash(clean)), nil
}

func Encode(m Marker) ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// Write writes the marker atomically (temp file + rename) so a crash cannot
// leave a half-written JSON file behind.
func Write(repoPath, rel string, m Marker) error {
	abs, err := SafeAbsPath(repoPath, rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	data, err := Encode(m)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(abs), ".marker-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpName, abs)
}

func Exists(repoPath, rel string) bool {
	abs, err := SafeAbsPath(repoPath, rel)
	if err != nil {
		return false
	}
	_, err = os.Stat(abs)
	return err == nil
}

func Identical(repoPath, rel string, m Marker) (bool, error) {
	abs, err := SafeAbsPath(repoPath, rel)
	if err != nil {
		return false, err
	}
	existing, err := os.ReadFile(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	want, err := Encode(m)
	if err != nil {
		return false, err
	}
	want = append(want, '\n')
	return string(existing) == string(want), nil
}

// ValidDayKey reports whether s looks like a calendar day.
func ValidDayKey(s string) bool { return dayKeyRe.MatchString(s) }
