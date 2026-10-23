//go:build test

package sqlite

import (
	"fmt"
	"testing"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
)

func TestActivityCursor(t *testing.T) {
	instance, err := New(config)
	test.IsNil(t, err)
	defer instance.Close()
	for i := 0; i < 5; i++ {
		instance.SaveActivity(models.Activity{At: int64(100 + i), Kind: "upload", ActorId: 7, FileId: fmt.Sprintf("f%d", i)})
	}
	oldest, newest := instance.ActivityBounds()
	test.IsEqualBool(t, newest-oldest == 4, true)
	page := instance.GetActivitySince(oldest+1, 2)
	test.IsEqualInt(t, len(page), 2)
	test.IsEqualBool(t, page[0].Id == oldest+2 && page[1].Id == oldest+3, true)
	test.IsEqualString(t, page[0].FileId, "f2")
	test.IsEqualInt(t, page[0].ActorId, 7)
	instance.PruneActivity(103)
	oldest2, _ := instance.ActivityBounds()
	test.IsEqualBool(t, oldest2 == oldest+3, true)

	// A full prune keeps the newest id: AUTOINCREMENT never reuses ids, so a
	// cursor stays meaningful, and an empty table reports oldest 0
	instance.PruneActivity(1 << 40)
	oldest3, newest3 := instance.ActivityBounds()
	test.IsEqualBool(t, oldest3 == 0 && newest3 == newest, true)
	test.IsEqualInt(t, len(instance.GetActivitySince(0, 10)), 0)
	instance.SaveActivity(models.Activity{At: 200, Kind: "download", FileName: "a.jpg", FileSize: 12, Via: "zip", Detail: "x"})
	page = instance.GetActivitySince(newest, 10)
	test.IsEqualInt(t, len(page), 1)
	test.IsEqualBool(t, page[0].Id == newest+1, true)
	test.IsEqualBool(t, page[0] == models.Activity{Id: newest + 1, At: 200, Kind: "download", FileName: "a.jpg", FileSize: 12, Via: "zip", Detail: "x"}, true)
}
