package preview

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/logging"
)

// GeneratorVersion identifies what the generator produces (sizes, formats,
// quality). Bump it whenever the outputs change: the next Start removes every
// preview made by an older version and the backfill regenerates them.
const GeneratorVersion = 2

// generatorVersionFile sits in Dir(). A missing or unreadable marker counts as
// version 1, the generator that predates the marker.
const generatorVersionFile = ".generator-version"

func readGeneratorVersion() int {
	raw, err := os.ReadFile(filepath.Join(Dir(), generatorVersionFile))
	if err != nil {
		return 1
	}
	v, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return 1
	}
	return v
}

// regenerateOnVersionChange removes every preview row and its files when the
// stored generator version differs from GeneratorVersion, and returns how many
// rows it removed. Run it before backfill, which then rebuilds them in its
// normal order. The marker is written only after the removal finished, so a
// crash in between repeats the removal on the next start instead of leaving
// old-size previews that look current.
func regenerateOnVersionChange() int {
	if readGeneratorVersion() == GeneratorVersion {
		return 0
	}
	known := database.GetAllPreviewSha1s()
	for sha1 := range known {
		removeBlob(sha1)
	}
	marker := filepath.Join(Dir(), generatorVersionFile)
	if err := os.WriteFile(marker, []byte(strconv.Itoa(GeneratorVersion)), 0600); err != nil {
		// stdout only. Rows are already gone and backfill rebuilds them; the next
		// start finds no marker and has nothing left to remove.
		fmt.Printf("preview: cannot write %s: %v\n", marker, err)
	}
	if len(known) > 0 {
		logging.LogInfo(fmt.Sprintf("Preview generator v%d: regenerating %d preview(s)", GeneratorVersion, len(known)))
	}
	return len(known)
}
