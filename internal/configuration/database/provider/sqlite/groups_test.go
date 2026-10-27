//go:build test

package sqlite

import (
	"path/filepath"
	"testing"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
)

func TestGroupsRoundTrip(t *testing.T) {
	instance, err := New(config)
	test.IsNil(t, err)
	defer instance.Close()
	test.IsEqualBool(t, instance.SupportsGroups(), true)

	g := models.Group{Id: "g1", Name: "Iceland trip", CreatorId: 7, Created: 100, Expires: 5000,
		JoinCode: "AAA-AA1", InviteToken: "token1"}
	instance.SaveGroup(g)
	got, ok := instance.GetGroup("g1")
	test.IsEqualBool(t, ok, true)
	test.IsEqualBool(t, got == g, true)
	got, ok = instance.GetGroupByToken("token1")
	test.IsEqualBool(t, ok && got.Id == "g1", true)
	got, ok = instance.GetGroupByCode("AAA-AA1")
	test.IsEqualBool(t, ok && got.Id == "g1", true)
	_, ok = instance.GetGroupByToken("")
	test.IsEqualBool(t, ok, false)
	instance.SaveGroup(models.Group{Id: "g2", Name: "Wedding", CreatorId: 8, Created: 200, JoinCode: "AAA-AA2", InviteToken: "token2"})
	all := instance.GetAllGroups()
	test.IsEqualInt(t, len(all), 2)
	test.IsEqualString(t, all[0].Id, "g2") // newest first

	instance.SaveGroupMember(models.GroupMember{GroupId: "g1", UserId: 7, Joined: 100, Role: models.GroupRoleCreator})
	instance.SaveGroupMember(models.GroupMember{GroupId: "g1", UserId: 8, Joined: 150, Role: models.GroupRoleMember})
	instance.SaveGroupMember(models.GroupMember{GroupId: "g2", UserId: 8, Joined: 200, Role: models.GroupRoleCreator})
	members := instance.GetGroupMembers("g1")
	test.IsEqualInt(t, len(members), 2)
	test.IsEqualInt(t, members[0].UserId, 7)
	test.IsEqualInt(t, len(instance.GetMembershipsOfUser(8)), 2)
	instance.DeleteGroupMember("g1", 8)
	test.IsEqualInt(t, len(instance.GetMembershipsOfUser(8)), 1)

	instance.AddGroupFile("g1", "fA")
	instance.AddGroupFile("g1", "fA") // idempotent
	instance.AddGroupFile("g1", "fB")
	instance.AddGroupFile("g2", "fA")
	test.IsEqualInt(t, len(instance.GetGroupFileIds("g1")), 2)
	test.IsEqualInt(t, len(instance.GetGroupIdsOfFile("fA")), 2)
	instance.RemoveGroupFile("g1", "fB")
	test.IsEqualInt(t, len(instance.GetGroupFileIds("g1")), 1)
	instance.RemoveFileFromGroups("fA")
	test.IsEqualInt(t, len(instance.GetGroupIdsOfFile("fA")), 0)

	instance.SaveNearCopyKept("g1", "fC")
	instance.SaveNearCopyKept("g1", "fC")
	test.IsEqualBool(t, instance.GetNearCopiesKept("g1")["fC"], true)

	instance.AddGroupFile("g1", "fD")
	instance.SaveGroupMember(models.GroupMember{GroupId: "g1", UserId: 9, Joined: 1, Role: models.GroupRoleMember})
	instance.DeleteGroup("g1")
	_, ok = instance.GetGroup("g1")
	test.IsEqualBool(t, ok, false)
	test.IsEqualInt(t, len(instance.GetGroupMembers("g1")), 0)
	test.IsEqualInt(t, len(instance.GetGroupFileIds("g1")), 0)
	test.IsEqualInt(t, len(instance.GetNearCopiesKept("g1")), 0)
	// the other group is untouched
	_, ok = instance.GetGroup("g2")
	test.IsEqualBool(t, ok, true)
	test.IsEqualInt(t, len(instance.GetGroupMembers("g2")), 1)
	instance.DeleteGroup("g2")
}

func TestDownloadsAndCollectionsSeen(t *testing.T) {
	instance, err := New(config)
	test.IsNil(t, err)
	defer instance.Close()
	instance.SaveDownload(models.Download{FileId: "d1", UserId: 7, At: 1, Via: models.DownloadViaGroup})
	instance.SaveDownload(models.Download{FileId: "d1", UserId: 7, At: 2, Via: models.DownloadViaZip})
	instance.SaveDownload(models.Download{FileId: "d2", UserId: 0, At: 3, Via: models.DownloadViaFile}) // anonymous
	got := instance.GetDownloadedFileIds(7)
	test.IsEqualInt(t, len(got), 1)
	test.IsEqualBool(t, got["d1"], true)
	test.IsEqualInt(t, len(instance.GetDownloadedFileIds(0)), 0) // anonymous rows belong to nobody
	instance.DeleteDownloadsOfFile("d1")
	test.IsEqualInt(t, len(instance.GetDownloadedFileIds(7)), 0)

	// CopyDownloads: signed-in users' rows follow, once each, never twice
	instance.SaveDownload(models.Download{FileId: "c1", UserId: 7, At: 5, Via: models.DownloadViaGroup})
	instance.SaveDownload(models.Download{FileId: "c1", UserId: 7, At: 6, Via: models.DownloadViaZip})
	instance.SaveDownload(models.Download{FileId: "c1", UserId: 8, At: 7, Via: models.DownloadViaGroup})
	instance.SaveDownload(models.Download{FileId: "c1", UserId: 0, At: 8, Via: models.DownloadViaFile}) // anonymous
	instance.SaveDownload(models.Download{FileId: "c2", UserId: 8, At: 9, Via: models.DownloadViaGroup})
	instance.CopyDownloads("c1", "c2")
	instance.CopyDownloads("c1", "c2") // twice changes nothing
	instance.CopyDownloads("c1", "c1")
	instance.CopyDownloads("", "c2")
	var rows int
	test.IsNil(t, instance.sqliteDb.QueryRow(`SELECT COUNT(*) FROM "Downloads" WHERE FileId = 'c2'`).Scan(&rows))
	test.IsEqualInt(t, rows, 2) // user 8's own, and one for user 7
	var at int64
	var via string
	test.IsNil(t, instance.sqliteDb.QueryRow(`SELECT At, Via FROM "Downloads" WHERE FileId = 'c2' AND UserId = 7`).Scan(&at, &via))
	test.IsEqualInt64(t, at, 5) // the first time, by its route
	test.IsEqualString(t, via, models.DownloadViaGroup)
	test.IsEqualBool(t, instance.GetDownloadedFileIds(7)["c2"], true)
	test.IsEqualBool(t, instance.GetDownloadedFileIds(8)["c2"], true)
	test.IsEqualInt(t, len(instance.GetDownloadedFileIds(0)), 0)
	test.IsNil(t, instance.sqliteDb.QueryRow(`SELECT COUNT(*) FROM "Downloads" WHERE FileId = 'c1'`).Scan(&rows))
	test.IsEqualInt(t, rows, 4) // the source keeps its rows
	instance.DeleteDownloadsOfFile("c1")
	instance.DeleteDownloadsOfFile("c2")

	instance.SaveCollectionSeen(models.CollectionSeen{UserId: 7, CollectionId: "c1", FirstOpened: 10})
	instance.SaveCollectionSeen(models.CollectionSeen{UserId: 7, CollectionId: "c1", FirstOpened: 99}) // keeps 10
	instance.SaveCollectionSeen(models.CollectionSeen{UserId: 7, CollectionId: "c2", FirstOpened: 20})
	seen := instance.GetCollectionsSeen(7)
	test.IsEqualInt(t, len(seen), 2)
	test.IsEqualString(t, seen[0].CollectionId, "c2")
	test.IsEqualInt64(t, seen[1].FirstOpened, 10)
	instance.SaveCollection(models.Collection{Id: "c1", Name: "x", UserId: 7, CreationDate: 1})
	instance.DeleteCollection("c1")
	test.IsEqualInt(t, len(instance.GetCollectionsSeen(7)), 1)
	instance.DeleteCollection("c2")
}

// Like Previews (Ruling 20): the tables appear on a start with no schema bump,
// and a second start with them present is harmless
func TestGroupTablesIdempotentWithoutSchemaBump(t *testing.T) {
	cfg := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "gokapi.sqlite"), Type: 0}
	for i := 0; i < 2; i++ {
		instance, err := New(cfg)
		test.IsNil(t, err)
		test.IsEqualInt(t, instance.GetDbVersion(), 17)
		instance.SaveGroup(models.Group{Id: "x", Name: "x", JoinCode: "h", InviteToken: "t"})
		// R4's CollectionGroups table is there as well
		instance.SaveCollectionGroup(models.CollectionGroup{CollectionId: "c", GroupId: "x", LinkMode: models.CollectionLinkInvite})
		_, linked := instance.GetCollectionGroup("c")
		test.IsEqualBool(t, linked, true)
		instance.Close()
	}
}

// R4: a collection turned into a group, the people who opened it, and the
// link going away with the collection or the group
func TestCollectionGroupsAndViewers(t *testing.T) {
	instance, err := New(config)
	test.IsNil(t, err)
	defer instance.Close()

	instance.SaveCollectionSeen(models.CollectionSeen{UserId: 21, CollectionId: "cg1", FirstOpened: 30})
	instance.SaveCollectionSeen(models.CollectionSeen{UserId: 22, CollectionId: "cg1", FirstOpened: 10})
	instance.SaveCollectionSeen(models.CollectionSeen{UserId: 21, CollectionId: "cg2", FirstOpened: 5})
	viewers := instance.GetCollectionViewers("cg1")
	test.IsEqualInt(t, len(viewers), 2)
	test.IsEqualInt(t, viewers[0].UserId, 22) // first opened first
	test.IsEqualInt(t, viewers[1].UserId, 21)
	test.IsEqualString(t, viewers[1].CollectionId, "cg1")
	test.IsEqualInt(t, len(instance.GetCollectionViewers("nosuch")), 0)

	_, ok := instance.GetCollectionGroup("cg1")
	test.IsEqualBool(t, ok, false)
	link := models.CollectionGroup{CollectionId: "cg1", GroupId: "gg1", LinkMode: models.CollectionLinkInvite}
	instance.SaveCollectionGroup(link)
	got, ok := instance.GetCollectionGroup("cg1")
	test.IsEqualBool(t, ok, true)
	test.IsEqualBool(t, got == link, true)
	link.LinkMode = models.CollectionLinkRequire
	instance.SaveCollectionGroup(link) // replaces
	got, _ = instance.GetCollectionGroup("cg1")
	test.IsEqualString(t, got.LinkMode, models.CollectionLinkRequire)
	instance.SaveCollectionGroup(models.CollectionGroup{CollectionId: "cg2", GroupId: "gg1", LinkMode: models.CollectionLinkInvite})
	instance.SaveCollectionGroup(models.CollectionGroup{CollectionId: "cg3", GroupId: "gg2", LinkMode: models.CollectionLinkInvite})
	ofGroup := instance.GetCollectionGroupsOfGroup("gg1")
	test.IsEqualInt(t, len(ofGroup), 2)
	test.IsEqualString(t, ofGroup[0].CollectionId, "cg1")

	// Deleting a collection drops its link (and who opened it)
	instance.SaveCollection(models.Collection{Id: "cg1", Name: "x", UserId: 7, CreationDate: 1})
	instance.DeleteCollection("cg1")
	_, ok = instance.GetCollectionGroup("cg1")
	test.IsEqualBool(t, ok, false)
	test.IsEqualInt(t, len(instance.GetCollectionViewers("cg1")), 0)
	// DeleteCollectionGroup drops one link
	instance.DeleteCollectionGroup("cg3")
	_, ok = instance.GetCollectionGroup("cg3")
	test.IsEqualBool(t, ok, false)
	// Deleting the group drops its links; the collections themselves are
	// storage's to delete
	instance.SaveGroup(models.Group{Id: "gg1", Name: "x", JoinCode: "CGG-AA1", InviteToken: "cgtoken1"})
	instance.DeleteGroup("gg1")
	test.IsEqualInt(t, len(instance.GetCollectionGroupsOfGroup("gg1")), 0)
	instance.DeleteCollection("cg2")
}
