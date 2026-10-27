//go:build test

package viewas

import (
	"testing"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
)

func viewAsRows(adminId int) []models.Activity {
	var result []models.Activity
	for _, a := range database.GetActivitySince(0, 100000) {
		if (a.Kind == models.ActivityViewAsStart || a.Kind == models.ActivityViewAsEnd) && a.ActorId == adminId {
			result = append(result, a)
		}
	}
	return result
}

// Start and End write feed events next to their [auth] log lines (Part 4, Q13)
func TestViewAsRecordsActivity(t *testing.T) {
	admin, a, b := makePeople(t, 8190, "activity")
	test.IsNil(t, Start(admin, a.Id))
	test.IsNil(t, Start(admin, b.Id)) // ends a with "switched"
	End(admin.Id, "exit")
	End(admin.Id, "exit") // nothing active: nothing recorded
	rows := viewAsRows(admin.Id)
	test.IsEqualInt(t, len(rows), 4)
	want := []struct{ kind, detail string }{
		{models.ActivityViewAsStart, "target=" + itoa(a.Id)},
		{models.ActivityViewAsEnd, "target=" + itoa(a.Id) + " reason=switched"},
		{models.ActivityViewAsStart, "target=" + itoa(b.Id)},
		{models.ActivityViewAsEnd, "target=" + itoa(b.Id) + " reason=exit"},
	}
	for i, w := range want {
		test.IsEqualString(t, rows[i].Kind, w.kind)
		test.IsEqualString(t, rows[i].Detail, w.detail)
	}
}
