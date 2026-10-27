package redis

import "github.com/forceu/gokapi/internal/models"

// The activity feed needs the SQLite database, like groups (Part 4, A). The
// activity package checks SupportsGroups() before writing, so these are
// never reached in practice; they must not panic.

func (p DatabaseProvider) SaveActivity(a models.Activity) {}
func (p DatabaseProvider) GetActivitySince(since int64, limit int) []models.Activity {
	return []models.Activity{}
}
func (p DatabaseProvider) ActivityBounds() (oldest, newest int64) { return 0, 0 }
func (p DatabaseProvider) PruneActivity(before int64)             {}
