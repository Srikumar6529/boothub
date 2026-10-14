package detect

import (
	"testing"
	"time"
)

func TestTimezoneValid(t *testing.T) {
	tz := Timezone()
	if tz == "" {
		t.Fatal("empty timezone")
	}
	if _, err := time.LoadLocation(tz); err != nil {
		t.Fatalf("invalid timezone %q: %v", tz, err)
	}
}
