package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
)

// cardSource is one directory on a card worth importing, with the rules for
// grouping, transferring and cleaning it up.
type cardSource struct {
	name        string
	dir         string                            // relative to the card root
	group       func(string) ([]dateGroup, error) // group source files by date
	flatCleanup bool                              // remove loose files rather than whole date directories
	rsyncOnly   bool                              // require rsync (no Immich upload, no library scan)
}

// cardLayout describes how one camera arranges its card and how to recognise it.
type cardLayout struct {
	name           string
	detect         func(root string) bool
	sources        []cardSource
	clearSonyIndex bool // clear Sony's card index after cleanup so the camera does not complain
}

var cardLayouts = []cardLayout{
	{
		name: "sony",
		// Sony cameras keep their ownership index and clip metadata under PRIVATE
		// and AVF_INFO even when the card holds no media.
		detect: func(root string) bool {
			return cardHas(root, "PRIVATE/SONY", "PRIVATE/M4ROOT", "AVF_INFO")
		},
		sources: []cardSource{
			{name: "sony photos", dir: "DCIM", group: groupSonyByDate},
			{name: "sony videos", dir: "PRIVATE/M4ROOT/CLIP", group: groupSonyVideosByDate, flatCleanup: true, rsyncOnly: true},
		},
		clearSonyIndex: true,
	},
	{
		name:    "dji",
		detect:  func(root string) bool { return cardHas(root, "DCIM/DJI_001") },
		sources: []cardSource{{name: "dji", dir: "DCIM/DJI_001", group: groupDJIByDate, flatCleanup: true}},
	},
	{
		name:    "canon",
		detect:  func(root string) bool { return cardHas(root, "DCIM/CANONMSC", "DCIM/*CANON") },
		sources: []cardSource{{name: "canon", dir: "DCIM", group: groupCanonByDate}},
	},
	{
		name: "charmera",
		// The Charmera has no DCIM tree: it drops JPEGs and AVIs at the card root.
		detect: func(root string) bool {
			return !cardHas(root, "DCIM") && hasLooseMedia(root)
		},
		sources: []cardSource{{name: "charmera", dir: ".", group: groupCharmeraByDate, flatCleanup: true}},
	},
}

func layoutNames() string {
	names := make([]string, 0, len(cardLayouts))
	for _, layout := range cardLayouts {
		names = append(names, layout.name)
	}
	return strings.Join(names, ", ")
}

// pickLayout returns the layout named by override, or detects one from the card.
func pickLayout(root, override string) (cardLayout, error) {
	if override != "" {
		return layoutByName(override)
	}
	return detectLayout(root)
}

func layoutByName(name string) (cardLayout, error) {
	for _, layout := range cardLayouts {
		if layout.name == name {
			return layout, nil
		}
	}
	return cardLayout{}, fmt.Errorf("unknown camera %q; expected one of %s", name, layoutNames())
}

// detectLayout recognises the card at root. Exactly one layout must match:
// silence on a card nobody recognises, or a guess between two, would either do
// nothing or do the wrong thing without saying so.
func detectLayout(root string) (cardLayout, error) {
	var matches []cardLayout
	for _, layout := range cardLayouts {
		if layout.detect(root) {
			matches = append(matches, layout)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return cardLayout{}, fmt.Errorf("could not recognise the card at %s; pass --camera with one of %s", root, layoutNames())
	default:
		names := make([]string, 0, len(matches))
		for _, layout := range matches {
			names = append(names, layout.name)
		}
		return cardLayout{}, fmt.Errorf("card at %s looks like %s; pass --camera to choose", root, strings.Join(names, " and "))
	}
}

// activeSource is a cardSource bound to a directory that exists on the card.
type activeSource struct {
	cardSource
	path string
}

// resolveSources binds the layout's sources to directories under root, dropping
// any that are absent (a Sony card with no clips has no CLIP folder). An
// override replaces one source's directory and disables the rest: the source
// whose folder name matches the override's, else the primary one, so pointing
// at a CLIP folder gets the video rules.
func resolveSources(layout cardLayout, root, override string) []activeSource {
	if override != "" {
		chosen := layout.sources[0]
		for _, source := range layout.sources {
			if filepath.Base(source.dir) == filepath.Base(override) {
				chosen = source
			}
		}
		return []activeSource{{cardSource: chosen, path: override}}
	}
	var active []activeSource
	for _, source := range layout.sources {
		path := filepath.Join(root, source.dir)
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			log.Debug().Str("source", source.name).Str("path", path).Msg("source directory absent, skipping")
			continue
		}
		active = append(active, activeSource{cardSource: source, path: path})
	}
	return active
}

// cardHas reports whether any of the patterns (literal paths or globs, relative
// to root) names something on the card.
func cardHas(root string, patterns ...string) bool {
	for _, pattern := range patterns {
		if matches, _ := filepath.Glob(filepath.Join(root, pattern)); len(matches) > 0 {
			return true
		}
	}
	return false
}

func hasLooseMedia(root string) bool {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() || isSystemFile(entry.Name()) {
			continue
		}
		switch strings.ToLower(filepath.Ext(entry.Name())) {
		case ".jpg", ".jpeg", ".avi":
			return true
		}
	}
	return false
}
