//go:build test

package storage

import (
	"errors"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
)

// cgCollection saves a collection of fresh files owned by the user
func cgCollection(id string, owner int) (models.Collection, []models.File) {
	a := groupFile(id+"a", owner, nil)
	b := groupFile(id+"b", owner, nil)
	c := models.Collection{Id: id, Name: "Collection " + id, UserId: owner,
		CreationDate: time.Now().Unix(), FileIds: []string{a.Id, b.Id}}
	database.SaveCollection(c)
	return c, []models.File{a, b}
}

// cgUsers saves the people of these tests: 71 owns, 72 and 73 opened the
// link signed in, 74 is the admin, 75 a plain member added later
func cgUsers() (owner, ann, ben, admin, member models.User) {
	owner = models.User{Id: 71, Name: "olga@example.com", UserLevel: models.UserLevelUser}
	ann = models.User{Id: 72, Name: "ann@example.com", UserLevel: models.UserLevelUser}
	ben = models.User{Id: 73, Name: "ben@example.com", UserLevel: models.UserLevelUser}
	admin = models.User{Id: 74, Name: "boss@example.com", UserLevel: models.UserLevelSuperAdmin, Permissions: models.UserPermissionAll}
	member = models.User{Id: 75, Name: "max@example.com", UserLevel: models.UserLevelUser}
	for _, u := range []models.User{owner, ann, ben, admin, member} {
		database.SaveUser(u, false)
	}
	return
}

func memberIds(g models.Group) map[int]string {
	result := map[int]string{}
	for _, m := range database.GetGroupMembers(g.Id) {
		result[m.UserId] = m.Role
	}
	return result
}

func TestGroupFromCollectionAddsViewers(t *testing.T) {
	owner, ann, ben, admin, _ := cgUsers()
	collection, files := cgCollection("cgviewers", owner.Id)
	defer dropGroupFiles(files...)
	defer database.DeleteCollection(collection.Id)
	// Who opened it signed in: ann first, then ben, plus someone whose
	// account is gone and (defensively) the owner, who is never recorded
	database.SaveCollectionSeen(models.CollectionSeen{UserId: ben.Id, CollectionId: collection.Id, FirstOpened: 20})
	database.SaveCollectionSeen(models.CollectionSeen{UserId: ann.Id, CollectionId: collection.Id, FirstOpened: 10})
	database.SaveCollectionSeen(models.CollectionSeen{UserId: 79, CollectionId: collection.Id, FirstOpened: 30})
	database.SaveCollectionSeen(models.CollectionSeen{UserId: owner.Id, CollectionId: collection.Id, FirstOpened: 5})

	viewers := CollectionViewers(collection, owner)
	test.IsEqualInt(t, len(viewers), 2)
	test.IsEqualInt(t, viewers[0].Id, ann.Id)
	test.IsEqualInt(t, viewers[1].Id, ben.Id)
	// The admin converting leaves themself out as well
	database.SaveCollectionSeen(models.CollectionSeen{UserId: admin.Id, CollectionId: collection.Id, FirstOpened: 40})
	test.IsEqualInt(t, len(CollectionViewers(collection, admin)), 2)
	test.IsEqualInt(t, len(CollectionViewers(collection, owner)), 3)

	// A link mode that is neither is refused before anything is made
	before := len(GetAllGroups())
	_, _, err := GroupFromCollection(collection.Id, 60, FromCollectionOptions{LinkMode: "public"}, owner)
	test.IsEqualBool(t, errors.Is(err, ErrGroupLinkMode), true)
	test.IsEqualInt(t, len(GetAllGroups()), before)

	g, res, err := GroupFromCollection(collection.Id, 60, FromCollectionOptions{AddViewers: true}, owner)
	test.IsNil(t, err)
	defer DeleteGroup(g, owner)
	test.IsEqualInt(t, len(res.Added), 2)
	test.IsEqualInt(t, res.MembersAdded, 3) // ann, ben, admin
	members := memberIds(g)
	test.IsEqualInt(t, len(members), 4)
	test.IsEqualString(t, members[owner.Id], models.GroupRoleCreator)
	test.IsEqualString(t, members[ann.Id], models.GroupRoleMember)
	test.IsEqualString(t, members[ben.Id], models.GroupRoleMember)
	test.IsEqualString(t, members[admin.Id], models.GroupRoleMember)
	_, gone := members[79]
	test.IsEqualBool(t, gone, false)

	// The link is recorded, invite by default
	link, linked := CollectionLinkOf(collection.Id)
	test.IsEqualBool(t, linked, true)
	test.IsEqualBool(t, link.Live, true)
	test.IsEqualString(t, link.Group.Id, g.Id)
	test.IsEqualString(t, link.Link.LinkMode, models.CollectionLinkInvite)
	test.IsEqualInt(t, len(GroupLinks(g)), 1)

	// A collection is turned into a group once
	_, _, err = GroupFromCollection(collection.Id, 60, FromCollectionOptions{}, owner)
	test.IsEqualBool(t, errors.Is(err, ErrCollectionAlreadyGroup), true)
	test.IsEqualInt(t, len(GetAllGroups()), before+1)
}

func TestGroupFromCollectionWithoutViewers(t *testing.T) {
	owner, ann, _, _, _ := cgUsers()
	collection, files := cgCollection("cgnoviewers", owner.Id)
	defer dropGroupFiles(files...)
	defer database.DeleteCollection(collection.Id)
	database.SaveCollectionSeen(models.CollectionSeen{UserId: ann.Id, CollectionId: collection.Id, FirstOpened: 10})

	g, res, err := GroupFromCollection(collection.Id, 30,
		FromCollectionOptions{AddViewers: false, LinkMode: models.CollectionLinkRequire}, owner)
	test.IsNil(t, err)
	defer DeleteGroup(g, owner)
	test.IsEqualInt(t, res.MembersAdded, 0)
	test.IsEqualInt(t, len(memberIds(g)), 1)
	link, _ := CollectionLinkOf(collection.Id)
	test.IsEqualString(t, link.Link.LinkMode, models.CollectionLinkRequire)
}

func TestCollectionClosedTo(t *testing.T) {
	owner, ann, _, admin, member := cgUsers()
	collection, files := cgCollection("cgclosed", owner.Id)
	defer dropGroupFiles(files...)
	defer database.DeleteCollection(collection.Id)
	plain, plainFiles := cgCollection("cgplain", owner.Id)
	defer dropGroupFiles(plainFiles...)
	defer database.DeleteCollection(plain.Id)

	g, _, err := GroupFromCollection(collection.Id, 30, FromCollectionOptions{}, owner)
	test.IsNil(t, err)
	defer database.DeleteGroup(g.Id) // it closes below, so storage would not find it
	_, err = JoinGroupByToken(g.InviteToken, member)
	test.IsNil(t, err)

	// Invite: open to everyone, as before
	for _, u := range []models.User{{}, ann, member, owner} {
		test.IsEqualBool(t, CollectionClosedTo(collection.Id, u), false)
	}
	// Require: only those who can see the group
	test.IsNil(t, SetGroupLinkMode(g, models.CollectionLinkRequire, owner))
	test.IsEqualBool(t, CollectionClosedTo(collection.Id, models.User{}), true)
	test.IsEqualBool(t, CollectionClosedTo(collection.Id, ann), true)
	test.IsEqualBool(t, CollectionClosedTo(collection.Id, member), false)
	test.IsEqualBool(t, CollectionClosedTo(collection.Id, owner), false)
	test.IsEqualBool(t, CollectionClosedTo(collection.Id, admin), false)
	// A collection that never was a group is never closed
	test.IsEqualBool(t, CollectionClosedTo(plain.Id, models.User{}), false)
	// A link whose group has closed is closed to everyone
	g.Expires = 1
	database.SaveGroup(g)
	link, linked := CollectionLinkOf(collection.Id)
	test.IsEqualBool(t, linked && !link.Live, true)
	test.IsEqualBool(t, CollectionClosedTo(collection.Id, owner), true)
}

func TestGroupLinkModeAndStop(t *testing.T) {
	owner, _, _, admin, member := cgUsers()
	collection, files := cgCollection("cgstop", owner.Id)
	defer dropGroupFiles(files...)
	defer database.DeleteCollection(collection.Id)
	g, _, err := GroupFromCollection(collection.Id, 30, FromCollectionOptions{}, owner)
	test.IsNil(t, err)
	defer DeleteGroup(g, owner)
	_, err = JoinGroupByToken(g.InviteToken, member)
	test.IsNil(t, err)

	// A plain member may do neither
	test.IsEqualBool(t, errors.Is(SetGroupLinkMode(g, models.CollectionLinkRequire, member), ErrGroupForbidden), true)
	test.IsEqualBool(t, errors.Is(StopGroupLink(g, member), ErrGroupForbidden), true)
	link, _ := CollectionLinkOf(collection.Id)
	test.IsEqualString(t, link.Link.LinkMode, models.CollectionLinkInvite)
	// Only the two modes
	test.IsEqualBool(t, errors.Is(SetGroupLinkMode(g, "open", owner), ErrGroupLinkMode), true)
	// The creator and the admin may
	test.IsNil(t, SetGroupLinkMode(g, models.CollectionLinkRequire, owner))
	link, _ = CollectionLinkOf(collection.Id)
	test.IsEqualString(t, link.Link.LinkMode, models.CollectionLinkRequire)
	test.IsNil(t, SetGroupLinkMode(g, models.CollectionLinkInvite, admin))
	link, _ = CollectionLinkOf(collection.Id)
	test.IsEqualString(t, link.Link.LinkMode, models.CollectionLinkInvite)

	// Stopping deletes the collection and the link; the group and its files stay
	test.IsNil(t, StopGroupLink(g, owner))
	_, ok := database.GetCollection(collection.Id)
	test.IsEqualBool(t, ok, false)
	_, linked := CollectionLinkOf(collection.Id)
	test.IsEqualBool(t, linked, false)
	test.IsEqualInt(t, len(GroupLinks(g)), 0)
	_, ok = GetGroup(g.Id)
	test.IsEqualBool(t, ok, true)
	test.IsEqualInt(t, len(GroupFiles(g)), 2)
	// Nothing left to stop or switch
	test.IsEqualBool(t, errors.Is(StopGroupLink(g, owner), ErrNoGroupLink), true)
	test.IsEqualBool(t, errors.Is(SetGroupLinkMode(g, models.CollectionLinkRequire, owner), ErrNoGroupLink), true)
}

// Deleting the group, or its closing, takes the collection behind its link along
func TestGroupDeletionRemovesLinkedCollection(t *testing.T) {
	owner, _, _, _, _ := cgUsers()
	collection, files := cgCollection("cgdelete", owner.Id)
	defer dropGroupFiles(files...)
	defer database.DeleteCollection(collection.Id)
	g, _, err := GroupFromCollection(collection.Id, 30, FromCollectionOptions{}, owner)
	test.IsNil(t, err)
	test.IsNil(t, DeleteGroup(g, owner))
	_, ok := database.GetCollection(collection.Id)
	test.IsEqualBool(t, ok, false)
	_, linked := database.GetCollectionGroup(collection.Id)
	test.IsEqualBool(t, linked, false)
	// The files stay
	_, ok = database.GetMetaDataById(files[0].Id)
	test.IsEqualBool(t, ok, true)

	expiring, moreFiles := cgCollection("cgexpire", owner.Id)
	defer dropGroupFiles(moreFiles...)
	defer database.DeleteCollection(expiring.Id)
	g2, _, err := GroupFromCollection(expiring.Id, 30, FromCollectionOptions{}, owner)
	test.IsNil(t, err)
	g2.Expires = 1
	database.SaveGroup(g2)
	cleanUpGroups(time.Now().Unix())
	_, ok = database.GetGroup(g2.Id)
	test.IsEqualBool(t, ok, false)
	_, ok = database.GetCollection(expiring.Id)
	test.IsEqualBool(t, ok, false)
	_, linked = database.GetCollectionGroup(expiring.Id)
	test.IsEqualBool(t, linked, false)
}
