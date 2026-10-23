//go:build test

package sqlite

import (
	"testing"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
)

func TestBlocklistRoundTripAndFailsClosed(t *testing.T) {
	instance, err := New(config)
	test.IsNil(t, err)
	instance.SaveBlock(models.BlockEntry{Entry: "@bl.example", Added: 1, AddedBy: 2})
	instance.SaveBlock(models.BlockEntry{Entry: "@bl.example", Added: 9, AddedBy: 3})
	test.IsEqualBool(t, instance.IsBlocked("@bl.example"), true)
	test.IsEqualBool(t, instance.IsBlocked("@other.example"), false)
	all := instance.GetAllBlocks()
	test.IsEqualInt(t, len(all), 1)
	test.IsEqualInt(t, all[0].AddedBy, 2)
	instance.DeleteBlock("@bl.example")
	test.IsEqualBool(t, instance.IsBlocked("@bl.example"), false)

	// a database that cannot be read must not answer "not blocked"
	instance.Close()
	func() {
		defer test.ExpectPanic(t)
		instance.IsBlocked("@bl.example")
	}()
}
