package redis

import (
	"testing"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
)

func TestGroupsUnsupported(t *testing.T) {
	instance, err := New(config)
	test.IsNil(t, err)
	defer instance.Close()
	test.IsEqualBool(t, instance.SupportsGroups(), false)

	// downloads must never break on Redis
	instance.SaveDownload(models.Download{FileId: "d1", UserId: 7, At: 1, Via: models.DownloadViaFile})
	instance.SaveCollectionSeen(models.CollectionSeen{UserId: 7, CollectionId: "c1", FirstOpened: 1})
	instance.DeleteDownloadsOfFile("d1")
	test.IsEqualInt(t, len(instance.GetDownloadedFileIds(7)), 0)
	test.IsEqualInt(t, len(instance.GetCollectionsSeen(7)), 0)
	_, ok := instance.GetGroup("g1")
	test.IsEqualBool(t, ok, false)
	test.IsEqualInt(t, len(instance.GetAllGroups()), 0)
}
