package detect

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Timezone returns the best-effort IANA timezone for this machine.
func Timezone() string {
	if tz := strings.TrimSpace(os.Getenv("TZ")); tz != "" && tz != ":" {
		if _, err := time.LoadLocation(tz); err == nil {
			return tz
		}
	}

	if tz := fromLocaltimeSymlink(); tz != "" {
		return tz
	}

	if name := time.Local.String(); name != "" && name != "Local" {
		if _, err := time.LoadLocation(name); err == nil {
			return name
		}
	}

	return "UTC"
}

func fromLocaltimeSymlink() string {
	// macOS/Linux: /etc/localtime -> .../zoneinfo/Area/City
	target, err := filepath.EvalSymlinks("/etc/localtime")
	if err != nil {
		return ""
	}
	target = filepath.ToSlash(target)
	for _, marker := range []string{"/zoneinfo/", "/tzdata/"} {
		if i := strings.Index(target, marker); i >= 0 {
			tz := target[i+len(marker):]
			if _, err := time.LoadLocation(tz); err == nil {
				return tz
			}
		}
	}
	// Some systems store under /usr/share/zoneinfo/
	if i := strings.Index(target, "zoneinfo/"); i >= 0 {
		tz := target[i+len("zoneinfo/"):]
		if _, err := time.LoadLocation(tz); err == nil {
			return tz
		}
	}
	_ = runtime.GOOS
	return ""
}
