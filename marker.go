package main

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
	markerSource   = "boothub"
	markerVersion  = 1
	markerPlatform = "boot.dev"

	triggerCLISubmit    = "cli_submit"
	triggerCLIRunSubmit = "cli_run_submit"
	triggerManual       = "manual"
	triggerBackfill     = "backfill"
	triggerExtension    = "extension"

	// markerPrefix is the commit subject every marker commit starts with.
	markerPrefix = "boot.dev: complete "

	submissionsPrefix = "submissions/"
)

type marker struct {
	Source        string    `json:"source"`
	Version       int       `json:"version"`
	Platform      string    `json:"platform"`
	LessonID      string    `json:"lesson_id"`
	SubmittedAt   time.Time `json:"submitted_at"`
	Trigger       string    `json:"trigger"`
	Note          string    `json:"note,omitempty"`
	ActivityCount int       `json:"activity_count,omitempty"`
}

func newMarker(lessonID, trigger string, when time.Time) marker {
	return marker{
		Source:      markerSource,
		Version:     markerVersion,
		Platform:    markerPlatform,
		LessonID:    lessonID,
		SubmittedAt: when.UTC(),
		Trigger:     trigger,
	}
}

func encodeMarker(m marker) ([]byte, error) {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// lessonIDRe matches ids we are willing to write to disk. It excludes path
// separators, leading dots and ".." so an id can never escape submissions/.
var lessonIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,180}$`)

func sanitizeLessonID(id string) (string, error) {
	id = strings.TrimPrefix(strings.TrimSpace(id), "@")
	if id == "" {
		return "", fmt.Errorf("lesson id is empty")
	}
	id = strings.ReplaceAll(id, "/", "-")
	id = strings.ReplaceAll(id, "\\", "-")
	if !lessonIDRe.MatchString(id) {
		return "", fmt.Errorf("invalid lesson id %q", id)
	}
	return id, nil
}

func dayKey(when time.Time, loc *time.Location) string {
	return when.In(loc).Format("2006-01-02")
}

var dayKeyRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func validDayKey(s string) bool { return dayKeyRe.MatchString(s) }

func markerRelPath(day, lessonID string) string {
	return path.Join("submissions", day, lessonID+".json")
}

// safeAbs resolves rel under repoPath, refusing anything that escapes it.
func safeAbs(repoPath, rel string) (string, error) {
	clean := path.Clean(rel)
	if clean == "." || clean == ".." || path.IsAbs(clean) || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("unsafe marker path %q", rel)
	}
	if !filepath.IsLocal(filepath.FromSlash(clean)) {
		return "", fmt.Errorf("unsafe marker path %q", rel)
	}
	return filepath.Join(repoPath, filepath.FromSlash(clean)), nil
}

// writeMarker writes the marker atomically (temp file + rename).
func writeMarker(repoPath, rel string, m marker) error {
	abs, err := safeAbs(repoPath, rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	data, err := encodeMarker(m)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(abs), ".marker-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		return err
	}
	return os.Rename(name, abs)
}

func markerExists(repoPath, rel string) bool {
	abs, err := safeAbs(repoPath, rel)
	if err != nil {
		return false
	}
	_, err = os.Stat(abs)
	return err == nil
}

func markerIdentical(repoPath, rel string, m marker) (bool, error) {
	abs, err := safeAbs(repoPath, rel)
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
	want, err := encodeMarker(m)
	if err != nil {
		return false, err
	}
	return string(existing) == string(want), nil
}
