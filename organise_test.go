package main

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// noonUTC returns a timestamp at noon so formatting it in any local time zone
// still yields the same calendar date.
func noonUTC(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 12, 0, 0, 0, time.UTC)
}

func writeFile(t *testing.T, path, content string, modTime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if !modTime.IsZero() {
		if err := os.Chtimes(path, modTime, modTime); err != nil {
			t.Fatal(err)
		}
	}
}

// groupsByDate flattens a []dateGroup into date → sorted file names for
// order-independent comparison. Groups that sync a whole directory (files == nil)
// record a single "*" marker.
func groupsByDate(groups []dateGroup) map[string][]string {
	out := make(map[string][]string)
	for _, g := range groups {
		if g.files == nil {
			out[g.date] = append(out[g.date], "*")
			continue
		}
		out[g.date] = append(out[g.date], g.files...)
	}
	for _, files := range out {
		sort.Strings(files)
	}
	return out
}

func TestParseEXIFDate(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string // formatted YYYY-MM-DD, empty means expect error
		wantErr bool
	}{
		{"standard space format", "2023:05:14 09:30:00", "2023-05-14", false},
		{"all-colon variant", "2023:05:14:09:30:00", "2023-05-14", false},
		{"garbage", "not a date", "", true},
		{"empty", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseEXIFDate(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got %v", tt.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Format("2006-01-02") != tt.want {
				t.Errorf("parseEXIFDate(%q) = %s, want %s", tt.raw, got.Format("2006-01-02"), tt.want)
			}
		})
	}
}

func TestParseSonyXMLDate(t *testing.T) {
	dir := t.TempDir()

	valid := filepath.Join(dir, "C0001M01.XML")
	writeFile(t, valid, `<?xml version="1.0"?>
<NonRealTimeMeta><CreationDate value="2026-06-07T10:35:22+02:00"/></NonRealTimeMeta>`, time.Time{})
	if got, err := parseSonyXMLDate(valid); err != nil || got != "2026-06-07" {
		t.Errorf("parseSonyXMLDate(valid) = (%q, %v), want (2026-06-07, nil)", got, err)
	}

	missing := filepath.Join(dir, "C0002M01.XML")
	writeFile(t, missing, `<NonRealTimeMeta></NonRealTimeMeta>`, time.Time{})
	if _, err := parseSonyXMLDate(missing); err == nil {
		t.Error("expected error when CreationDate is absent")
	}

	if _, err := parseSonyXMLDate(filepath.Join(dir, "nope.XML")); err == nil {
		t.Error("expected error for a nonexistent file")
	}
}

func TestSonyVideoSidecarRegex(t *testing.T) {
	tests := []struct {
		name     string
		wantBase string // empty means no match
	}{
		{"C0023M01.XML", "C0023"},
		{"C0023M01.xml", "C0023"}, // case-insensitive extension
		{"C1234M02.XML", "C1234"},
		{"C0023.MP4", ""},
		{"MEDIAPRO.XML", ""},
		{"C0023T01.JPG", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := sonyVideoSidecarRegex.FindStringSubmatch(tt.name)
			if tt.wantBase == "" {
				if m != nil {
					t.Errorf("%q unexpectedly matched: %v", tt.name, m)
				}
				return
			}
			if m == nil {
				t.Fatalf("%q did not match", tt.name)
			}
			if m[1] != tt.wantBase {
				t.Errorf("%q base = %q, want %q", tt.name, m[1], tt.wantBase)
			}
		})
	}
}

func TestSonyVideoDate(t *testing.T) {
	dir := t.TempDir()

	// Clip with a sidecar: date comes from the XML CreationDate, not mtime.
	writeFile(t, filepath.Join(dir, "C0001.MP4"), "video", noonUTC(2020, time.January, 1))
	writeFile(t, filepath.Join(dir, "C0001M01.XML"),
		`<CreationDate value="2023-05-14T09:30:00+00:00"/>`, time.Time{})

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var mp4 os.DirEntry
	for _, e := range entries {
		if e.Name() == "C0001.MP4" {
			mp4 = e
		}
	}
	if got := sonyVideoDate(dir, mp4); got != "2023-05-14" {
		t.Errorf("sonyVideoDate with sidecar = %s, want 2023-05-14", got)
	}

	// Clip without a sidecar: fall back to mtime.
	dir2 := t.TempDir()
	writeFile(t, filepath.Join(dir2, "C0002.MP4"), "video", noonUTC(2024, time.August, 9))
	entries2, _ := os.ReadDir(dir2)
	if got := sonyVideoDate(dir2, entries2[0]); got != "2024-08-09" {
		t.Errorf("sonyVideoDate without sidecar = %s, want 2024-08-09", got)
	}
}

func TestGroupSonyVideosByDate(t *testing.T) {
	dir := t.TempDir()
	// Two clips on the same XML date, each with a sidecar.
	writeFile(t, filepath.Join(dir, "C0001.MP4"), "v", noonUTC(2020, time.January, 1))
	writeFile(t, filepath.Join(dir, "C0001M01.XML"), `<CreationDate value="2023-05-14T00:00:00Z"/>`, time.Time{})
	writeFile(t, filepath.Join(dir, "C0002.MP4"), "v", noonUTC(2020, time.January, 1))
	writeFile(t, filepath.Join(dir, "C0002M01.XML"), `<CreationDate value="2023-05-14T00:00:00Z"/>`, time.Time{})
	// A clip on a different date, dated by mtime (no sidecar).
	writeFile(t, filepath.Join(dir, "C0003.MP4"), "v", noonUTC(2024, time.August, 9))
	// An orphan sidecar with no matching clip must be skipped.
	writeFile(t, filepath.Join(dir, "C0099M01.XML"), `<CreationDate value="2023-05-14T00:00:00Z"/>`, time.Time{})
	// A non-video, non-sidecar file must be ignored.
	writeFile(t, filepath.Join(dir, "MEDIAPRO.XML"), "junk", time.Time{})

	groups, err := groupSonyVideosByDate(dir)
	if err != nil {
		t.Fatal(err)
	}

	got := groupsByDate(groups)
	want := map[string][]string{
		"2023-05-14": {"C0001.MP4", "C0001M01.XML", "C0002.MP4", "C0002M01.XML"},
		"2024-08-09": {"C0003.MP4"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d date groups, want %d: %v", len(got), len(want), got)
	}
	for date, files := range want {
		if !equalStrings(got[date], files) {
			t.Errorf("date %s: got %v, want %v", date, got[date], files)
		}
	}
}

func TestGroupDJIByDate(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "DJI_20230715093000_0001_D.MP4"), "v", time.Time{})
	writeFile(t, filepath.Join(dir, "DJI_20230715093500_0002_D.MP4"), "v", time.Time{})
	writeFile(t, filepath.Join(dir, "DJI_20230820120000_0003_D.JPG"), "p", time.Time{})
	writeFile(t, filepath.Join(dir, "notes.txt"), "ignored", time.Time{})

	groups, err := groupDJIByDate(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := groupsByDate(groups)
	want := map[string][]string{
		"2023-07-15": {"DJI_20230715093000_0001_D.MP4", "DJI_20230715093500_0002_D.MP4"},
		"2023-08-20": {"DJI_20230820120000_0003_D.JPG"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d groups, want %d: %v", len(got), len(want), got)
	}
	for date, files := range want {
		if !equalStrings(got[date], files) {
			t.Errorf("date %s: got %v, want %v", date, got[date], files)
		}
	}
}

func TestSonyFolderDate(t *testing.T) {
	dir := t.TempDir()
	// photoDate falls back to mtime for non-EXIF files.
	writeFile(t, filepath.Join(dir, "DSC00001.ARW"), "raw", noonUTC(2025, time.March, 3))
	if got, err := sonyFolderDate(dir); err != nil || got != "2025-03-03" {
		t.Errorf("sonyFolderDate = (%q, %v), want (2025-03-03, nil)", got, err)
	}

	empty := t.TempDir()
	if _, err := sonyFolderDate(empty); err == nil {
		t.Error("expected error for a folder with no readable files")
	}
}

func TestGroupSonyByDate(t *testing.T) {
	dir := t.TempDir()
	// A valid 8-digit Sony date folder containing one file.
	writeFile(t, filepath.Join(dir, "10160722", "DSC00001.ARW"), "raw", noonUTC(2026, time.July, 22))
	// A folder that does not match the 8-digit pattern is ignored.
	writeFile(t, filepath.Join(dir, "MISC", "readme.txt"), "x", noonUTC(2026, time.July, 22))

	groups, err := groupSonyByDate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1: %+v", len(groups), groups)
	}
	if groups[0].date != "2026-07-22" {
		t.Errorf("date = %s, want 2026-07-22", groups[0].date)
	}
	if groups[0].files != nil {
		t.Errorf("Sony folder groups should sync the whole directory (files == nil), got %v", groups[0].files)
	}
	if filepath.Base(groups[0].sourceDir) != "10160722" {
		t.Errorf("sourceDir = %s, want it to end in 10160722", groups[0].sourceDir)
	}
}

func TestGroupCharmeraByDate(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "img1.jpg"), "p", noonUTC(2025, time.February, 1))
	writeFile(t, filepath.Join(dir, "clip.avi"), "v", noonUTC(2025, time.February, 1))
	writeFile(t, filepath.Join(dir, "img2.jpeg"), "p", noonUTC(2025, time.February, 2))
	writeFile(t, filepath.Join(dir, "ignore.bin"), "x", noonUTC(2025, time.February, 2))
	// Subdirectories are skipped.
	writeFile(t, filepath.Join(dir, "sub", "img3.jpg"), "p", noonUTC(2025, time.February, 3))

	groups, err := groupCharmeraByDate(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := groupsByDate(groups)
	want := map[string][]string{
		"2025-02-01": {"clip.avi", "img1.jpg"},
		"2025-02-02": {"img2.jpeg"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d groups, want %d: %v", len(got), len(want), got)
	}
	for date, files := range want {
		if !equalStrings(got[date], files) {
			t.Errorf("date %s: got %v, want %v", date, got[date], files)
		}
	}
}

func TestGroupCanonByDate(t *testing.T) {
	dir := t.TempDir()
	// Normal Canon files in a DCIM subfolder.
	writeFile(t, filepath.Join(dir, "100CANON", "IMG_0001.JPG"), "p", noonUTC(2025, time.April, 10))
	writeFile(t, filepath.Join(dir, "100CANON", "IMG_0002.JPG"), "p", noonUTC(2025, time.April, 10))
	// CANONMSC housekeeping files must be excluded.
	writeFile(t, filepath.Join(dir, "CANONMSC", "M100.CTG"), "x", noonUTC(2025, time.April, 10))
	// An already-organised ISO-date directory must be skipped entirely.
	writeFile(t, filepath.Join(dir, "2020-01-01", "IMG_9999.JPG"), "p", noonUTC(2020, time.January, 1))

	groups, err := groupCanonByDate(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := groupsByDate(groups)
	want := map[string][]string{
		"2025-04-10": {"IMG_0001.JPG", "IMG_0002.JPG"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d groups, want %d: %v", len(got), len(want), got)
	}
	for date, files := range want {
		if !equalStrings(got[date], files) {
			t.Errorf("date %s: got %v, want %v", date, got[date], files)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
