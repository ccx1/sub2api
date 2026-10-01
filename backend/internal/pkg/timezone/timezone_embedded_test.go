package timezone

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestTimezoneDatabaseWithoutGoInstallation(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows standalone timezone regression")
	}
	if os.Getenv("SUB2API_TIMEZONE_TEST_HELPER") == "1" {
		assertEmbeddedTimezoneOffsets(t)
		return
	}
	t.Setenv("GOROOT", t.TempDir())
	t.Setenv("ZONEINFO", filepath.Join(t.TempDir(), "missing-zoneinfo.zip"))
	t.Setenv("SUB2API_TIMEZONE_TEST_HELPER", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestTimezoneDatabaseWithoutGoInstallation$")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("standalone timezone loading: %v\n%s", err, output)
	}
}

func assertEmbeddedTimezoneOffsets(t *testing.T) {
	t.Helper()
	originalLocal := time.Local
	for _, tt := range []struct {
		zone   string
		month  time.Month
		offset int
	}{
		{"Asia/Shanghai", time.January, 8 * 3600},
		{"America/New_York", time.January, -5 * 3600},
		{"America/New_York", time.July, -4 * 3600},
	} {
		loc, err := time.LoadLocation(tt.zone)
		if err != nil {
			t.Fatalf("load %s without external tzdata: %v", tt.zone, err)
		}
		_, offset := time.Date(2026, tt.month, 15, 12, 0, 0, 0, loc).Zone()
		if offset != tt.offset {
			t.Errorf("%s month %d offset = %d, want %d", tt.zone, tt.month, offset, tt.offset)
		}
	}
	if time.Local != originalLocal {
		t.Fatal("loading timezone data changed the configured local timezone")
	}
}
