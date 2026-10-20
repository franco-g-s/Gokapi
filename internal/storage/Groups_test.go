//go:build test

package storage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
)

// groupFile saves a file and writes its blob. The blob matters: DeleteFile
// starts a CleanUp in the background, which would otherwise drop every
// blobless file a later assertion still reads.
func groupFile(id string, owner int, mutate func(*models.File)) models.File {
	f := models.File{Id: id, Name: id + ".jpg", SHA1: "sha-" + id, UserId: owner, SizeBytes: 1,
		ExpireAt: time.Now().Add(24 * time.Hour).Unix(), DownloadsRemaining: 1, ContentType: "image/jpeg"}
	if mutate != nil {
		mutate(&f)
	}
	database.SaveMetaData(f)
	if err := os.WriteFile(filepath.Join(configuration.Get().DataDir, f.SHA1), []byte("x"), 0600); err != nil {
		panic(err)
	}
	return f
}

// dropGroupFiles removes the files and their blobs again
func dropGroupFiles(files ...models.File) {
	for _, f := range files {
		database.DeleteMetaData(f.Id)
		database.RemoveFileFromGroups(f.Id)
		_ = os.Remove(filepath.Join(configuration.Get().DataDir, f.SHA1))
	}
}

func TestGroupCodes(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		code := newGroupCode()
		test.IsEqualInt(t, len(code), 7)
		test.IsEqualString(t, code[3:4], "-")
		test.IsEqualString(t, NormaliseGroupCode(code), code)
		seen[code] = true
	}
	test.IsEqualBool(t, len(seen) > 190, true) // random, not derived
	test.IsEqualString(t, NormaliseGroupCode("k7p 4qx"), "K7P-4QX")
	test.IsEqualString(t, NormaliseGroupCode("K7P-4Q"), "")
	test.IsEqualString(t, NormaliseGroupCode("O1I-L0U"), "") // look-alikes are not in the alphabet

	expiry, err := ExpiryFromChoice(30, 1000)
	test.IsNil(t, err)
	test.IsEqualInt64(t, expiry, 1000+30*24*3600)
	expiry, err = ExpiryFromChoice(0, 1000)
	test.IsNil(t, err)
	test.IsEqualInt64(t, expiry, 0)
	_, err = ExpiryFromChoice(45, 1000)
	test.IsEqualBool(t, errors.Is(err, ErrGroupExpiryChoice), true)
}

func TestCreateJoinRolesAndReset(t *testing.T) {
	creator := models.User{Id: 7, UserLevel: models.UserLevelUser}
	member := models.User{Id: 8, UserLevel: models.UserLevelUser}
	stranger := models.User{Id: 9, UserLevel: models.UserLevelUser}
	admin := models.User{Id: 5, UserLevel: models.UserLevelSuperAdmin, Permissions: models.UserPermissionAll}

	_, err := CreateGroup("  ", 60, creator)
	test.IsEqualBool(t, errors.Is(err, ErrGroupName), true)
	_, err = CreateGroup("Trip", 45, creator)
	test.IsEqualBool(t, errors.Is(err, ErrGroupExpiryChoice), true)
	g, err := CreateGroup("Trip", 60, creator)
	test.IsNil(t, err)
	defer database.DeleteGroup(g.Id)
	role, ok := GroupRole(g, 7)
	test.IsEqualBool(t, ok && role == models.GroupRoleCreator, true)
	test.IsEqualBool(t, strings.HasSuffix(GroupInviteUrl(g), "g/join?t="+g.InviteToken), true)
	test.IsEqualInt(t, len(g.InviteToken), 32)

	// Names are trimmed and capped at 200 characters, not bytes
	long, err := CreateGroup(" "+strings.Repeat("ü", 250)+" ", 0, creator)
	test.IsNil(t, err)
	defer database.DeleteGroup(long.Id)
	test.IsEqualInt(t, len([]rune(long.Name)), 200)
	test.IsEqualBool(t, long.NeverExpires(), true)

	joined, err := JoinGroupByCode(strings.ToLower(GroupCode(g)), member)
	test.IsNil(t, err)
	test.IsEqualString(t, joined.Id, g.Id)
	_, err = JoinGroupByToken(g.InviteToken, member) // idempotent
	test.IsNil(t, err)
	test.IsEqualInt(t, len(database.GetGroupMembers(g.Id)), 2)
	role, ok = GroupRole(g, 8)
	test.IsEqualBool(t, ok && role == models.GroupRoleMember, true)
	_, err = JoinGroupByCode("ZZZ-ZZZ", stranger)
	test.IsEqualBool(t, errors.Is(err, ErrInvalidJoin), true)
	_, err = JoinGroupByCode("", stranger)
	test.IsEqualBool(t, errors.Is(err, ErrInvalidJoin), true)
	_, err = JoinGroupByToken("", stranger)
	test.IsEqualBool(t, errors.Is(err, ErrInvalidJoin), true)

	test.IsEqualBool(t, CanManageGroup(g, creator), true)
	test.IsEqualBool(t, CanManageGroup(g, member), false)
	test.IsEqualBool(t, CanManageGroup(g, admin), true)
	test.IsEqualBool(t, CanSeeGroup(g, member), true)
	test.IsEqualBool(t, CanSeeGroup(g, stranger), false)
	test.IsEqualBool(t, CanSeeGroup(g, admin), true)
	// A zero-value User has the super admin's level 0; without an id it is nobody
	test.IsEqualBool(t, CanSeeGroup(g, models.User{}), false)
	test.IsEqualBool(t, CanManageGroup(g, models.User{}), false)
	_, err = RenameGroup(g, "Other", member)
	test.IsEqualBool(t, errors.Is(err, ErrGroupForbidden), true)
	_, err = RenameGroup(g, "   ", creator)
	test.IsEqualBool(t, errors.Is(err, ErrGroupName), true)
	renamed, err := RenameGroup(g, " Iceland ", creator)
	test.IsNil(t, err)
	test.IsEqualString(t, renamed.Name, "Iceland")
	test.IsEqualBool(t, errors.Is(LeaveGroup(g, creator), ErrCreatorCannotLeave), true)
	test.IsEqualBool(t, errors.Is(RemoveGroupMember(g, 7, admin), ErrCannotRemoveCreator), true)
	test.IsEqualBool(t, errors.Is(RemoveGroupMember(g, 8, member), ErrGroupForbidden), true)

	// The groups of a user, and of everyone
	test.IsEqualBool(t, containsGroup(GroupsOfUser(8), g.Id), true)
	test.IsEqualBool(t, containsGroup(GroupsOfUser(9), g.Id), false)
	test.IsEqualBool(t, containsGroup(GetAllGroups(), g.Id), true)

	// Leaving, and being removed
	test.IsNil(t, LeaveGroup(g, member))
	test.IsEqualBool(t, IsGroupMember(g, 8), false)
	_, err = JoinGroupByToken(g.InviteToken, member)
	test.IsNil(t, err)
	test.IsNil(t, RemoveGroupMember(g, 8, creator))
	test.IsEqualBool(t, IsGroupMember(g, 8), false)

	// Reset: the old code and link stop working, the new ones work
	oldCode, oldToken := GroupCode(g), g.InviteToken
	_, err = ResetGroupCode(g, member)
	test.IsEqualBool(t, errors.Is(err, ErrGroupForbidden), true)
	g2, err := ResetGroupCode(g, creator)
	test.IsNil(t, err)
	test.IsNotEqualString(t, g2.InviteToken, oldToken)
	test.IsEqualString(t, g2.Name, "Iceland") // a stale model does not undo the rename
	_, err = JoinGroupByCode(oldCode, stranger)
	test.IsEqualBool(t, errors.Is(err, ErrInvalidJoin), true)
	_, err = JoinGroupByToken(oldToken, stranger)
	test.IsEqualBool(t, errors.Is(err, ErrInvalidJoin), true)
	_, err = JoinGroupByCode(GroupCode(g2), stranger)
	test.IsNil(t, err)

	// An expired group is gone for every purpose
	g2.Expires = time.Now().Unix() - 1
	database.SaveGroup(g2)
	_, ok = GetGroup(g2.Id)
	test.IsEqualBool(t, ok, false)
	_, err = JoinGroupByToken(g2.InviteToken, models.User{Id: 10})
	test.IsEqualBool(t, errors.Is(err, ErrInvalidJoin), true)
	_, err = RenameGroup(g2, "Again", creator)
	test.IsEqualBool(t, errors.Is(err, ErrGroupNotFound), true)
	test.IsEqualBool(t, CanSeeGroup(g2, admin), false)
	test.IsEqualBool(t, containsGroup(GroupsOfUser(9), g2.Id), false)
	test.IsEqualBool(t, containsGroup(GetAllGroups(), g2.Id), false)

	// Deleting: only the creator or the admin
	g3, err := CreateGroup("Deleted", 30, creator)
	test.IsNil(t, err)
	defer database.DeleteGroup(g3.Id)
	test.IsEqualBool(t, errors.Is(DeleteGroup(g3, member), ErrGroupForbidden), true)
	test.IsNil(t, DeleteGroup(g3, admin))
	_, ok = database.GetGroup(g3.Id)
	test.IsEqualBool(t, ok, false)
}

func containsGroup(groups []models.Group, id string) bool {
	for _, g := range groups {
		if g.Id == id {
			return true
		}
	}
	return false
}

func TestAddFilesTermsAndDuplicates(t *testing.T) {
	owner := models.User{Id: 7, UserLevel: models.UserLevelUser}
	other := models.User{Id: 8, UserLevel: models.UserLevelUser}
	admin := models.User{Id: 5, UserLevel: models.UserLevelSuperAdmin, Permissions: models.UserPermissionAll}
	g, err := CreateGroup("Terms", 30, owner)
	test.IsNil(t, err)
	defer database.DeleteGroup(g.Id)
	_, _ = JoinGroupByToken(g.InviteToken, other)

	short := groupFile("grpshort", 7, nil) // expires in 1 day, 1 download left
	long := groupFile("grplong", 7, func(f *models.File) { f.ExpireAt = time.Now().Add(400 * 24 * time.Hour).Unix() })
	forever := groupFile("grpforever", 7, func(f *models.File) { f.UnlimitedTime = true })
	twin := groupFile("grptwin", 8, func(f *models.File) { f.SHA1 = "sha-grpshort" })
	e2e := groupFile("grpe2e", 7, func(f *models.File) { f.Encryption.IsEndToEndEncrypted = true; f.SHA1 = "e2e-x" })
	req := groupFile("grpreq", 7, func(f *models.File) { f.UploadRequestId = "r1" })
	others := groupFile("grpothers", 8, func(f *models.File) { f.SHA1 = "sha-unique-others" })
	defer dropGroupFiles(short, long, forever, twin, e2e, req, others)

	res, err := AddFilesToGroup(g, []string{short.Id, long.Id, forever.Id, e2e.Id, req.Id, others.Id, "nope", short.Id}, owner)
	test.IsNil(t, err)
	test.IsEqualInt(t, len(res.Added), 3)
	test.IsEqualInt(t, len(res.Refused), 4) // e2e, request, someone else's, unknown
	reasons := map[string]string{}
	for _, r := range res.Refused {
		reasons[r.FileId] = r.Reason
	}
	test.IsEqualString(t, reasons[e2e.Id], "end-to-end encrypted")
	test.IsEqualString(t, reasons[req.Id], "file request upload")
	test.IsEqualString(t, reasons[others.Id], "not yours")
	test.IsEqualString(t, reasons["nope"], "not found")
	test.IsEqualBool(t, res.Added[0].UnlimitedDownloads, true) // Added holds the re-read file
	got, _ := database.GetMetaDataById(short.Id)
	test.IsEqualBool(t, got.UnlimitedDownloads, true) // limit lifted
	test.IsEqualInt64(t, got.ExpireAt, g.Expires)     // extended to the group's
	got, _ = database.GetMetaDataById(long.Id)
	test.IsEqualInt64(t, got.ExpireAt, long.ExpireAt) // never shortened
	got, _ = database.GetMetaDataById(forever.Id)
	test.IsEqualBool(t, got.UnlimitedTime, true)
	test.IsEqualInt(t, len(GroupFiles(g)), 3)

	// Adding again changes nothing
	res, _ = AddFilesToGroup(g, []string{short.Id}, owner)
	test.IsEqualInt(t, len(res.Added)+len(res.Duplicates)+len(res.Refused), 0)

	// Exact duplicate by fingerprint, from another member
	res, _ = AddFilesToGroup(g, []string{twin.Id}, other)
	test.IsEqualInt(t, len(res.Added), 0)
	test.IsEqualInt(t, len(res.Duplicates), 1)
	test.IsEqualString(t, res.Duplicates[0].FileId, twin.Id)
	test.IsEqualString(t, res.Duplicates[0].ExistingId, short.Id)
	test.IsEqualInt(t, res.Duplicates[0].UploaderId, 7)
	found, ok := FindInGroup(g, "sha-grpshort")
	test.IsEqualBool(t, ok && found.Id == short.Id, true)
	_, ok = FindInGroup(g, "sha-nothing")
	test.IsEqualBool(t, ok, false)

	// The admin may add anyone's file; strangers may add nothing
	res, _ = AddFilesToGroup(g, []string{others.Id}, admin)
	test.IsEqualInt(t, len(res.Added), 1)
	_, err = AddFilesToGroup(g, []string{short.Id}, models.User{Id: 9, UserLevel: models.UserLevelUser})
	test.IsEqualBool(t, errors.Is(err, ErrGroupForbidden), true)

	// Removing never shortens; members remove only their own
	n, _ := RemoveFilesFromGroup(g, []string{short.Id}, other)
	test.IsEqualInt(t, n, 0)
	n, _ = RemoveFilesFromGroup(g, []string{others.Id}, other)
	test.IsEqualInt(t, n, 1)
	got, _ = database.GetMetaDataById(others.Id)
	test.IsEqualInt64(t, got.ExpireAt, g.Expires)
	_, err = RemoveFilesFromGroup(g, []string{short.Id}, models.User{Id: 9, UserLevel: models.UserLevelUser})
	test.IsEqualBool(t, errors.Is(err, ErrGroupForbidden), true)
	n, _ = RemoveFilesFromGroup(g, []string{short.Id, "nope"}, owner) // the creator removes anything
	test.IsEqualInt(t, n, 1)

	// The lock follows the latest-closing live group, and "never" is the latest
	name, locked := GroupLock(long.Id)
	test.IsEqualBool(t, locked && name == "Terms", true)
	never, err := CreateGroup("Forever", 0, owner)
	test.IsNil(t, err)
	defer database.DeleteGroup(never.Id)
	database.AddGroupFile(never.Id, long.Id)
	name, _ = GroupLock(long.Id)
	test.IsEqualString(t, name, "Forever")
	never.Expires = time.Now().Unix() - 1
	database.SaveGroup(never)
	name, _ = GroupLock(long.Id)
	test.IsEqualString(t, name, "Terms")
	_, locked = GroupLock(short.Id)
	test.IsEqualBool(t, locked, false)

	// Membership grants view of the files in the group, to members only
	test.IsEqualBool(t, UserSharesGroupWithFile(8, long.Id), true)
	test.IsEqualBool(t, UserSharesGroupWithFile(9, long.Id), false)
	test.IsEqualBool(t, UserSharesGroupWithFile(0, long.Id), false)
	test.IsEqualBool(t, UserSharesGroupWithFile(8, short.Id), false)
}

func TestExtendPropagates(t *testing.T) {
	owner := models.User{Id: 7, UserLevel: models.UserLevelUser}
	g, _ := CreateGroup("Extend", 30, owner)
	defer database.DeleteGroup(g.Id)
	f := groupFile("grpextend", 7, nil)
	pending := groupFile("grpextpending", 7, func(f *models.File) {
		f.SHA1 = "sha-pending"
		f.PendingDeletion = time.Now().Add(9 * time.Second).Unix()
	})
	defer dropGroupFiles(f, pending)
	_, _ = AddFilesToGroup(g, []string{f.Id}, owner)
	database.AddGroupFile(g.Id, pending.Id)

	// Deterministic: a second ticking between create and extend must not
	// make "30 days from today" later than the current closing date
	g.Expires = time.Now().Add(31 * 24 * time.Hour).Unix()
	database.SaveGroup(g)
	_, err := ExtendGroup(g, 30, owner) // same day count from today is not later... (≈ equal)
	test.IsEqualBool(t, errors.Is(err, ErrGroupNotLater), true)
	_, err = ExtendGroup(g, 90, models.User{Id: 8, UserLevel: models.UserLevelUser})
	test.IsEqualBool(t, errors.Is(err, ErrGroupForbidden), true)
	_, err = ExtendGroup(g, 45, owner)
	test.IsEqualBool(t, errors.Is(err, ErrGroupExpiryChoice), true)
	g2, err := ExtendGroup(g, 90, owner)
	test.IsNil(t, err)
	got, _ := database.GetMetaDataById(f.Id)
	test.IsEqualInt64(t, got.ExpireAt, g2.Expires)
	got, _ = database.GetMetaDataById(pending.Id)
	test.IsEqualInt64(t, got.ExpireAt, pending.ExpireAt) // undo window untouched
	g3, _ := ExtendGroup(g2, 0, owner)
	got, _ = database.GetMetaDataById(f.Id)
	test.IsEqualBool(t, got.UnlimitedTime && g3.NeverExpires(), true)
	_, err = ExtendGroup(g3, 90, owner)
	test.IsEqualBool(t, errors.Is(err, ErrGroupNotLater), true)
}

func TestGroupCleanUp(t *testing.T) {
	g := models.Group{Id: "grpexpired", Name: "x", CreatorId: 7, Expires: 1, JoinCode: "grpexp", InviteToken: "grpexp"}
	database.SaveGroup(g)
	database.AddGroupFile(g.Id, "grpsomefile")
	live, err := CreateGroup("Live", 30, models.User{Id: 7})
	test.IsNil(t, err)
	defer database.DeleteGroup(live.Id)
	cleanUpGroups(time.Now().Unix())
	_, ok := database.GetGroup(g.Id)
	test.IsEqualBool(t, ok, false)
	test.IsEqualInt(t, len(database.GetGroupIdsOfFile("grpsomefile")), 0)
	_, ok = database.GetGroup(live.Id)
	test.IsEqualBool(t, ok, true)
}

func TestGroupFromCollection(t *testing.T) {
	owner := models.User{Id: 7, UserLevel: models.UserLevelUser}
	a := groupFile("grpcola", 7, nil)
	b := groupFile("grpcolb", 7, nil)
	defer dropGroupFiles(a, b)
	collection := models.Collection{Id: "grpcollection", Name: "Iceland trip", UserId: 7,
		CreationDate: time.Now().Unix(), FileIds: []string{a.Id, b.Id}, PasswordHash: "somehash"}
	database.SaveCollection(collection)
	defer database.DeleteCollection(collection.Id)

	// A stranger is refused before anything is created
	before := len(GetAllGroups())
	_, _, err := GroupFromCollection(collection.Id, 60, models.User{Id: 9, UserLevel: models.UserLevelUser})
	test.IsEqualBool(t, errors.Is(err, ErrGroupForbidden), true)
	test.IsEqualInt(t, len(GetAllGroups()), before)

	g, res, err := GroupFromCollection(collection.Id, 60, owner)
	test.IsNil(t, err)
	defer database.DeleteGroup(g.Id)
	test.IsEqualString(t, g.Name, "Iceland trip")
	test.IsEqualInt(t, len(res.Added), 2)
	test.IsEqualInt(t, len(GroupFiles(g)), 2)
	role, _ := GroupRole(g, 7)
	test.IsEqualString(t, role, models.GroupRoleCreator)

	// The collection is untouched (D6)
	stored, ok := database.GetCollection(collection.Id)
	test.IsEqualBool(t, ok, true)
	test.IsEqualString(t, strings.Join(stored.FileIds, ","), a.Id+","+b.Id)
	test.IsEqualString(t, stored.PasswordHash, "somehash")
}

func TestAttachUploadDuplicate(t *testing.T) {
	owner := models.User{Id: 7, UserLevel: models.UserLevelUser}
	uploader := models.User{Id: 8, UserLevel: models.UserLevelUser}
	g, err := CreateGroup("Attach", 30, owner)
	test.IsNil(t, err)
	defer database.DeleteGroup(g.Id)
	_, _ = JoinGroupByToken(g.InviteToken, uploader)

	first := groupFile("grpattachfirst", 7, nil)
	again := groupFile("grpattachagain", 8, func(f *models.File) { f.SHA1 = "sha-grpattachfirst" })
	unique := groupFile("grpattachunique", 8, nil)
	defer dropGroupFiles(first, again, unique)

	_, dup := AttachUploadToGroup(g, first, owner)
	test.IsEqualBool(t, dup, false)

	duplicate, dup := AttachUploadToGroup(g, again, uploader)
	test.IsEqualBool(t, dup, true)
	test.IsEqualString(t, duplicate.FileId, again.Id)
	test.IsEqualString(t, duplicate.ExistingId, first.Id)
	test.IsEqualInt(t, duplicate.UploaderId, 7)
	got, _ := database.GetMetaDataById(again.Id)
	test.IsEqualInt64(t, got.ExpireAt, 0)
	// DeleteFile's background CleanUp removes it for good. Wait for that, so
	// the sweep does not run into the tests after this one.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, exists := database.GetMetaDataById(again.Id); !exists || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	_, exists := database.GetMetaDataById(again.Id)
	test.IsEqualBool(t, exists, false)
	time.Sleep(200 * time.Millisecond) // let the recursive sweep finish
	// The original and its blob survive: the blob is shared
	_, exists = database.GetMetaDataById(first.Id)
	test.IsEqualBool(t, exists, true)

	_, dup = AttachUploadToGroup(g, unique, uploader)
	test.IsEqualBool(t, dup, false)
	got, _ = database.GetMetaDataById(unique.Id)
	test.IsEqualBool(t, got.UnlimitedDownloads, true)
	test.IsEqualInt64(t, got.ExpireAt, g.Expires)
	test.IsEqualInt(t, len(database.GetGroupFileIds(g.Id)), 2)
}
