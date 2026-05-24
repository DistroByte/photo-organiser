package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/rwcarlsen/goexif/exif"
)

var sonyFolderNameRegex = regexp.MustCompile(`^\d{8}$`)
var djiFilenameRegex = regexp.MustCompile(`^DJI_(\d{4})(\d{2})(\d{2})\d{6}_\d+_\w\..+$`)
var isoDateRegex = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// dateGroup holds the rsync source and file list for one date's worth of photos.
type dateGroup struct {
	sourceDir string   // absolute path used as the rsync source root
	files     []string // paths relative to sourceDir; nil means sync the whole directory
	date      string   // "YYYY-MM-DD"
}

func groupSonyByDate(sourceDir string) ([]dateGroup, error) {
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return nil, err
	}
	var groups []dateGroup
	for _, entry := range entries {
		if !entry.IsDir() || !sonyFolderNameRegex.MatchString(entry.Name()) {
			continue
		}
		date, err := calculateDestinationDir(entry.Name())
		if err != nil {
			log.Warn().Str("dir", entry.Name()).Err(err).Msg("skipping directory with unparseable name")
			continue
		}
		groups = append(groups, dateGroup{
			sourceDir: filepath.Join(sourceDir, entry.Name()),
			date:      date,
		})
	}
	return groups, nil
}

func groupDJIByDate(sourceDir string) ([]dateGroup, error) {
	byDate := make(map[string][]string)
	err := filepath.WalkDir(sourceDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		base := filepath.Base(path)
		matches := djiFilenameRegex.FindStringSubmatch(base)
		if matches == nil {
			log.Debug().Str("file", base).Msg("skipping non-DJI file")
			return nil
		}
		date := fmt.Sprintf("%s-%s-%s", matches[1], matches[2], matches[3])
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		byDate[date] = append(byDate[date], rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return dateGroupsFromMap(sourceDir, byDate), nil
}

func groupCanonByDate(sourceDir string) ([]dateGroup, error) {
	type key struct{ dir, date string }
	byDirDate := make(map[key][]string)

	err := filepath.WalkDir(sourceDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == sourceDir {
			return err
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}

		if d.IsDir() && isoDateRegex.MatchString(filepath.Base(rel)) {
			return fs.SkipDir
		}
		if d.IsDir() {
			return nil
		}

		for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
			if strings.EqualFold(part, "CANONMSC") {
				return nil
			}
		}

		if isoDateRegex.MatchString(filepath.Base(filepath.Dir(rel))) {
			return nil
		}

		taken, err := photoDate(path)
		if err != nil {
			log.Warn().Str("file", path).Err(err).Msg("skipping file: cannot determine date")
			return nil
		}

		parentDir := filepath.Dir(path)
		k := key{dir: parentDir, date: taken.Format("2006-01-02")}
		byDirDate[k] = append(byDirDate[k], filepath.Base(path))
		return nil
	})
	if err != nil {
		return nil, err
	}

	groups := make([]dateGroup, 0, len(byDirDate))
	for k, files := range byDirDate {
		groups = append(groups, dateGroup{
			sourceDir: k.dir,
			files:     files,
			date:      k.date,
		})
	}
	return groups, nil
}

func groupCharmeraByDate(sourceDir string) ([]dateGroup, error) {
	byDate := make(map[string][]string)
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".jpg" && ext != ".jpeg" && ext != ".avi" {
			continue
		}

		var taken time.Time
		if ext == ".avi" {
			info, err := entry.Info()
			if err != nil {
				return nil, err
			}
			taken = info.ModTime()
		} else {
			taken, err = photoDate(filepath.Join(sourceDir, name))
			if err != nil {
				log.Warn().Str("file", name).Err(err).Msg("falling back to mtime for date")
				info, statErr := entry.Info()
				if statErr != nil {
					return nil, statErr
				}
				taken = info.ModTime()
			}
		}

		date := taken.Format("2006-01-02")
		byDate[date] = append(byDate[date], name)
	}
	return dateGroupsFromMap(sourceDir, byDate), nil
}

// photoDate returns the date a photo was taken, preferring EXIF over mtime.
func photoDate(path string) (time.Time, error) {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, err
	}
	defer f.Close()

	x, err := exif.Decode(f)
	if err == nil {
		if tag, tagErr := x.Get(exif.DateTimeOriginal); tagErr == nil {
			raw := strings.Trim(tag.String(), `"`)
			if t, parseErr := parseEXIFDate(raw); parseErr == nil {
				return t, nil
			}
		}
		if t, dtErr := x.DateTime(); dtErr == nil {
			return t, nil
		}
	}

	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime(), nil
}

// parseEXIFDate handles both the standard EXIF format ("2006:01:02 15:04:05") and
// the all-colon variant some cameras write ("2006:01:02:15:04:05").
func parseEXIFDate(raw string) (time.Time, error) {
	if t, err := time.Parse("2006:01:02 15:04:05", raw); err == nil {
		return t, nil
	}
	return time.Parse("2006:01:02:15:04:05", raw)
}

func dateGroupsFromMap(sourceDir string, byDate map[string][]string) []dateGroup {
	groups := make([]dateGroup, 0, len(byDate))
	for date, files := range byDate {
		groups = append(groups, dateGroup{
			sourceDir: sourceDir,
			files:     files,
			date:      date,
		})
	}
	return groups
}

func calculateDestinationDir(dirName string) (string, error) {
	if len(dirName) < 8 {
		return "", fmt.Errorf("directory name %s is too short to determine date", dirName)
	}
	currentYearStr := fmt.Sprintf("%d", time.Now().Year())
	year := currentYearStr[:3] + dirName[3:4]
	month := dirName[4:6]
	day := dirName[6:8]
	return fmt.Sprintf("%s-%s-%s", year, month, day), nil
}
