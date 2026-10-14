package marker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

func DayKey(when time.Time, loc *time.Location) string {
	local := when.In(loc)
	return local.Format("2006-01-02")
}

func RelPath(dayKey, lessonID string) string {
	return filepath.Join("submissions", dayKey, lessonID+".json")
}

func BackfillRelPath(dayKey string, n int) string {
	return RelPath(dayKey, fmt.Sprintf("backfill-%d", n))
}

func AbsPath(repoPath, rel string) string {
	return filepath.Join(repoPath, rel)
}

func Encode(m Marker) ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

func Write(repoPath, rel string, m Marker) error {
	abs := AbsPath(repoPath, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	data, err := Encode(m)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(abs, data, 0o644)
}

func Exists(repoPath, rel string) bool {
	_, err := os.Stat(AbsPath(repoPath, rel))
	return err == nil
}

func Identical(repoPath, rel string, m Marker) (bool, error) {
	abs := AbsPath(repoPath, rel)
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
