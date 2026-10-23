package storage

// Groups: every rule about who may see, join, change or fill a group lives
// here, so the pages and endpoints only ever ask (Part 2, spec "Groups").

import (
	"crypto/rand"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/forceu/gokapi/internal/activity"
	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/helper"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/webserver/api/mutex/apimutex"
)

var (
	ErrGroupNotFound       = errors.New("group not found")
	ErrGroupForbidden      = errors.New("you may not do that in this group")
	ErrGroupsUnsupported   = errors.New("groups need the SQLite database")
	ErrGroupExpiryChoice   = errors.New("pick 30, 60 or 90 days, or never")
	ErrGroupNotLater       = errors.New("the new closing date must be later than the current one")
	ErrGroupName           = errors.New("a group needs a name")
	ErrCreatorCannotLeave  = errors.New("the creator cannot leave; delete the group instead")
	ErrCannotRemoveCreator = errors.New("the creator cannot be removed")
	ErrInvalidJoin         = errors.New("no group with that code or link")
	ErrGroupNothingAdded   = errors.New("None of these files could go into a group")
	ErrNotACopy            = errors.New("the photo to keep is not in this group, or is not a near-copy of the one removed")
)

// GroupExpiryDays are the choices on every create, extend and turn-into form; 0 = never
var GroupExpiryDays = []int{30, 60, 90, 0}

// GroupDefaultExpiryDays is the suggested choice
const GroupDefaultExpiryDays = 60

const (
	groupCodeAlphabet   = "23456789ABCDEFGHJKMNPQRSTVWXYZ" // no 0/O, 1/I/L or U: nothing to misread
	groupNameMaxRunes   = 200
	groupInviteTokenLen = 32
)

// ExpiryFromChoice turns a day count from GroupExpiryDays into a closing
// timestamp counted from now; 0 (never) stays 0
func ExpiryFromChoice(days int, now int64) (int64, error) {
	switch days {
	case 0:
		return 0, nil
	case 30, 60, 90:
		return now + int64(days)*24*3600, nil
	}
	return 0, ErrGroupExpiryChoice
}

// GroupCode is the code members type under "Join" (D1). It is stored as is,
// like the invite token next to it: both are shown to members again, so a
// hash would protect nothing. Guessing is stopped by the rate limiter and the
// code space (30^6 ≈ 729 million), and a leak is undone with "Reset code".
func GroupCode(g models.Group) string { return g.JoinCode }

// newGroupCode draws six characters from the alphabet with crypto/rand,
// rejection-sampled so every character is equally likely
func newGroupCode() string {
	var b strings.Builder
	for b.Len() < 7 {
		if b.Len() == 3 {
			b.WriteByte('-')
			continue
		}
		var one [1]byte
		if _, err := rand.Read(one[:]); err != nil {
			panic(err)
		}
		if int(one[0]) >= 256-256%len(groupCodeAlphabet) {
			continue
		}
		b.WriteByte(groupCodeAlphabet[int(one[0])%len(groupCodeAlphabet)])
	}
	return b.String()
}

// NormaliseGroupCode turns what someone typed ("k7p 4qx") into the stored
// form ("K7P-4QX"), or "" if it is not six characters of the alphabet
func NormaliseGroupCode(input string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(input) {
		if strings.ContainsRune(groupCodeAlphabet, r) {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if len(s) != 6 {
		return ""
	}
	return s[:3] + "-" + s[3:]
}

// GroupInviteUrl is the link that joins the group after signing in
func GroupInviteUrl(g models.Group) string {
	return configuration.Get().ServerUrl + "g/join?t=" + g.InviteToken
}

// withFreshInvite gives the group a new invite token and a new code that no
// other group uses. SaveGroup is INSERT OR REPLACE on unique columns, so a
// value another group holds would silently evict that group. A clash is
// rare; five tries is plenty.
func withFreshInvite(g models.Group) models.Group {
	token := ""
	for i := 0; i < 5 && token == ""; i++ {
		candidate := helper.GenerateRandomString(groupInviteTokenLen)
		if other, taken := database.GetGroupByToken(candidate); !taken || other.Id == g.Id {
			token = candidate
		}
	}
	if token == "" {
		panic("could not find a free group invite token")
	}
	g.InviteToken = token
	for i := 0; i < 5; i++ {
		g.JoinCode = newGroupCode()
		if other, taken := database.GetGroupByCode(g.JoinCode); !taken || other.Id == g.Id {
			return g
		}
	}
	panic("could not find a free group code")
}

// newGroupId returns an id no group has yet, for the same reason
func newGroupId() string {
	for i := 0; i < 5; i++ {
		id := createNewId()
		if _, taken := database.GetGroup(id); !taken {
			return id
		}
	}
	panic("could not find a free group id")
}

// cleanGroupName trims a name and caps it at 200 characters. Only an empty
// name is refused, so a long collection name still converts.
func cleanGroupName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if runes := []rune(name); len(runes) > groupNameMaxRunes {
		name = strings.TrimSpace(string(runes[:groupNameMaxRunes]))
	}
	if name == "" {
		return "", ErrGroupName
	}
	return name, nil
}

// liveGroup re-reads a group by id, so a stale model can neither undo a
// change nor act on a group that has closed since it was read
func liveGroup(g models.Group) (models.Group, error) {
	if !database.SupportsGroups() {
		return models.Group{}, ErrGroupsUnsupported
	}
	current, ok := GetGroup(g.Id)
	if !ok {
		return models.Group{}, ErrGroupNotFound
	}
	return current, nil
}

// CreateGroup creates a group with the creator as its first member
func CreateGroup(name string, days int, creator models.User) (models.Group, error) {
	if !database.SupportsGroups() {
		return models.Group{}, ErrGroupsUnsupported
	}
	if creator.Id == 0 {
		return models.Group{}, ErrGroupForbidden
	}
	name, err := cleanGroupName(name)
	if err != nil {
		return models.Group{}, err
	}
	now := time.Now().Unix()
	expires, err := ExpiryFromChoice(days, now)
	if err != nil {
		return models.Group{}, err
	}
	g := withFreshInvite(models.Group{
		Id:        newGroupId(),
		Name:      name,
		CreatorId: creator.Id,
		Created:   now,
		Expires:   expires,
	})
	database.SaveGroup(g)
	database.SaveGroupMember(models.GroupMember{GroupId: g.Id, UserId: creator.Id, Joined: now, Role: models.GroupRoleCreator})
	activity.GroupCreated(g, creator)
	return g, nil
}

// GetGroup returns the group, or false when it is unknown or has closed
func GetGroup(id string) (models.Group, bool) {
	g, ok := database.GetGroup(id)
	if !ok || g.IsExpired(time.Now().Unix()) {
		return models.Group{}, false
	}
	return g, true
}

// GetAllGroups returns every live group, newest first
func GetAllGroups() []models.Group {
	now := time.Now().Unix()
	result := make([]models.Group, 0)
	for _, g := range database.GetAllGroups() {
		if !g.IsExpired(now) {
			result = append(result, g)
		}
	}
	return result
}

// GroupsOfUser returns the live groups the user is a member of, newest first
func GroupsOfUser(userId int) []models.Group {
	result := make([]models.Group, 0)
	if userId == 0 {
		return result
	}
	for _, m := range database.GetMembershipsOfUser(userId) {
		if g, ok := GetGroup(m.GroupId); ok {
			result = append(result, g)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Created != result[j].Created {
			return result[i].Created > result[j].Created
		}
		return result[i].Name < result[j].Name
	})
	return result
}

// GroupRole returns the user's role in the group, and whether they are a member
func GroupRole(g models.Group, userId int) (role string, member bool) {
	if userId == 0 {
		return "", false
	}
	for _, m := range database.GetGroupMembers(g.Id) {
		if m.UserId == userId {
			return m.Role, true
		}
	}
	return "", false
}

// IsGroupMember reports whether the user is in the group
func IsGroupMember(g models.Group, userId int) bool {
	_, member := GroupRole(g, userId)
	return member
}

// isGroupAdmin is the admin of the group rules: user.IsAdmin() (D17). A
// zero-value User has UserLevel 0, which is the super admin, so a user
// without an id never counts.
func isGroupAdmin(user models.User) bool {
	return user.Id != 0 && user.IsAdmin()
}

// CanSeeGroup is true for members and the admin (D17), while the group is open
func CanSeeGroup(g models.Group, user models.User) bool {
	if g.IsExpired(time.Now().Unix()) {
		return false
	}
	return isGroupAdmin(user) || IsGroupMember(g, user.Id)
}

// CanManageGroup is true for the creator and the admin (D17), while the group is open
func CanManageGroup(g models.Group, user models.User) bool {
	if g.IsExpired(time.Now().Unix()) {
		return false
	}
	return isGroupAdmin(user) || (user.Id != 0 && g.CreatorId == user.Id)
}

// GroupFiles returns the files in the group that are still live: not
// expired and not pending deletion
func GroupFiles(g models.Group) []models.File {
	now := time.Now().Unix()
	result := make([]models.File, 0)
	for _, id := range database.GetGroupFileIds(g.Id) {
		f, ok := database.GetMetaDataById(id)
		if !ok || f.IsPendingForDeletion() || IsExpiredFile(f, now) {
			continue
		}
		result = append(result, f)
	}
	return result
}

// join adds the user to a group found by its link or code. Joining twice
// changes nothing.
func join(g models.Group, found bool, user models.User) (models.Group, error) {
	if !found || g.IsExpired(time.Now().Unix()) {
		return models.Group{}, ErrInvalidJoin
	}
	if user.Id == 0 {
		return models.Group{}, ErrGroupForbidden
	}
	if !IsGroupMember(g, user.Id) {
		database.SaveGroupMember(models.GroupMember{GroupId: g.Id, UserId: user.Id,
			Joined: time.Now().Unix(), Role: models.GroupRoleMember})
		activity.GroupJoined(g, user)
	}
	return g, nil
}

// JoinGroupByToken joins the group of an invite link. Idempotent.
func JoinGroupByToken(token string, user models.User) (models.Group, error) {
	if !database.SupportsGroups() {
		return models.Group{}, ErrGroupsUnsupported
	}
	if token == "" {
		return models.Group{}, ErrInvalidJoin
	}
	g, ok := database.GetGroupByToken(token)
	return join(g, ok, user)
}

// JoinGroupByCode joins the group of a typed code, in any case and spacing. Idempotent.
func JoinGroupByCode(code string, user models.User) (models.Group, error) {
	if !database.SupportsGroups() {
		return models.Group{}, ErrGroupsUnsupported
	}
	normalised := NormaliseGroupCode(code)
	if normalised == "" {
		return models.Group{}, ErrInvalidJoin
	}
	g, ok := database.GetGroupByCode(normalised)
	return join(g, ok, user)
}

// LeaveGroup takes a member out of a group. The creator cannot leave (D8).
// The files they put in stay.
func LeaveGroup(g models.Group, user models.User) error {
	current, err := liveGroup(g)
	if err != nil {
		return err
	}
	role, member := GroupRole(current, user.Id)
	if !member {
		return ErrGroupForbidden
	}
	if role == models.GroupRoleCreator || current.CreatorId == user.Id {
		return ErrCreatorCannotLeave
	}
	database.DeleteGroupMember(current.Id, user.Id)
	return nil
}

// RemoveGroupMember lets the creator or the admin take someone out of a group
func RemoveGroupMember(g models.Group, memberId int, actor models.User) error {
	current, err := liveGroup(g)
	if err != nil {
		return err
	}
	if !CanManageGroup(current, actor) {
		return ErrGroupForbidden
	}
	if memberId == current.CreatorId {
		return ErrCannotRemoveCreator
	}
	database.DeleteGroupMember(current.Id, memberId)
	return nil
}

// RenameGroup gives the group a new name
func RenameGroup(g models.Group, name string, actor models.User) (models.Group, error) {
	current, err := liveGroup(g)
	if err != nil {
		return models.Group{}, err
	}
	if !CanManageGroup(current, actor) {
		return models.Group{}, ErrGroupForbidden
	}
	name, err = cleanGroupName(name)
	if err != nil {
		return models.Group{}, err
	}
	current.Name = name
	database.SaveGroup(current)
	return current, nil
}

// ExtendGroup sets a new closing date, 30/60/90 days from today or never
// (D18). It must be later than the current one. Every live file in the group
// then gets the group's terms; expired and pending-deletion files are skipped.
func ExtendGroup(g models.Group, days int, actor models.User) (models.Group, error) {
	current, err := liveGroup(g)
	if err != nil {
		return models.Group{}, err
	}
	if !CanManageGroup(current, actor) {
		return models.Group{}, ErrGroupForbidden
	}
	newExpiry, err := ExpiryFromChoice(days, time.Now().Unix())
	if err != nil {
		return models.Group{}, err
	}
	if current.NeverExpires() || (newExpiry != 0 && newExpiry <= current.Expires) {
		return models.Group{}, ErrGroupNotLater
	}
	current.Expires = newExpiry
	database.SaveGroup(current)
	for _, id := range database.GetGroupFileIds(current.Id) {
		updateFileTerms(id, current)
	}
	return current, nil
}

// ResetGroupCode replaces the code and the invite link; the old ones stop working
func ResetGroupCode(g models.Group, actor models.User) (models.Group, error) {
	current, err := liveGroup(g)
	if err != nil {
		return models.Group{}, err
	}
	if !CanManageGroup(current, actor) {
		return models.Group{}, ErrGroupForbidden
	}
	current = withFreshInvite(current)
	database.SaveGroup(current)
	return current, nil
}

// DeleteGroup removes the group, and the collection behind its old link if
// it was made from one (R4). Its files stay with their uploaders and keep
// their current expiry (D7).
func DeleteGroup(g models.Group, actor models.User) error {
	current, err := liveGroup(g)
	if err != nil {
		return err
	}
	if !CanManageGroup(current, actor) {
		return ErrGroupForbidden
	}
	deleteGroupAndLinks(current.Id)
	return nil
}

// GroupDuplicate is a file that was not added because a byte-identical copy
// is already in the group
type GroupDuplicate struct {
	FileId     string // the file that was not added
	ExistingId string // the copy already in the group
	UploaderId int    // who put the existing copy there
}

// GroupRefusal is a file that may not go into a group
type GroupRefusal struct {
	FileId string
	Reason string // "not found", "not yours", "end-to-end encrypted", "file request upload"
}

// GroupAddResult reports what AddFilesToGroup did with each file
type GroupAddResult struct {
	Added      []models.File
	Duplicates []GroupDuplicate
	Refused    []GroupRefusal
}

// applyGroupTerms gives a file the terms of a group it is in: unlimited
// downloads, and an expiry no earlier than the group's. Never shortens.
func applyGroupTerms(f *models.File, g models.Group) bool {
	changed := false
	if !f.UnlimitedDownloads {
		f.UnlimitedDownloads = true
		changed = true
	}
	if f.UnlimitedTime {
		return changed
	}
	if g.NeverExpires() {
		f.UnlimitedTime = true
		return true
	}
	if f.ExpireAt < g.Expires {
		f.ExpireAt = g.Expires
		changed = true
	}
	return changed
}

// updateFileTerms re-reads the file under the metadata lock, so a concurrent
// download or edit is not overwritten, and skips files that are expired or
// pending deletion (their 10 s undo window must not be cancelled)
func updateFileTerms(fileId string, g models.Group) {
	apimutex.Lock(apimutex.TypeMetaData, fileId)
	defer apimutex.Unlock(apimutex.TypeMetaData, fileId)
	f, ok := database.GetMetaDataById(fileId)
	if !ok || f.IsPendingForDeletion() || IsExpiredFile(f, time.Now().Unix()) {
		return
	}
	if applyGroupTerms(&f, g) {
		database.SaveMetaData(f)
	}
}

// isFingerprint reports whether a stored SHA1 can be compared across files.
// End-to-end encrypted files carry a random "e2e-" placeholder instead.
func isFingerprint(sha1 string) bool {
	return sha1 != "" && !strings.HasPrefix(sha1, "e2e-")
}

// AddFilesToGroup puts existing files into a group. Members add their own
// files; the admin may add anyone's. A file whose exact copy is already in
// the group is reported as a duplicate and not added.
func AddFilesToGroup(g models.Group, fileIds []string, actor models.User) (GroupAddResult, error) {
	result := GroupAddResult{Added: []models.File{}, Duplicates: []GroupDuplicate{}, Refused: []GroupRefusal{}}
	current, err := liveGroup(g)
	if err != nil {
		return result, err
	}
	if !CanSeeGroup(current, actor) {
		return result, ErrGroupForbidden
	}
	bySha := make(map[string]models.File)
	for _, f := range GroupFiles(current) {
		if isFingerprint(f.SHA1) {
			bySha[f.SHA1] = f
		}
	}
	inGroup := make(map[string]bool)
	for _, id := range database.GetGroupFileIds(current.Id) {
		inGroup[id] = true
	}
	now := time.Now().Unix()
	seen := make(map[string]bool)
	for _, id := range fileIds {
		if seen[id] {
			continue
		}
		seen[id] = true
		f, ok := database.GetMetaDataById(id)
		if !ok || f.IsPendingForDeletion() || IsExpiredFile(f, now) {
			result.Refused = append(result.Refused, GroupRefusal{FileId: id, Reason: "not found"})
			continue
		}
		if f.UserId != actor.Id && !isGroupAdmin(actor) {
			result.Refused = append(result.Refused, GroupRefusal{FileId: id, Reason: "not yours"})
			continue
		}
		if f.Encryption.IsEndToEndEncrypted {
			result.Refused = append(result.Refused, GroupRefusal{FileId: id, Reason: "end-to-end encrypted"})
			continue
		}
		if f.UploadRequestId != "" {
			result.Refused = append(result.Refused, GroupRefusal{FileId: id, Reason: "file request upload"})
			continue
		}
		if inGroup[f.Id] {
			continue
		}
		if existing, dup := bySha[f.SHA1]; dup && isFingerprint(f.SHA1) {
			result.Duplicates = append(result.Duplicates, GroupDuplicate{FileId: f.Id, ExistingId: existing.Id, UploaderId: existing.UserId})
			continue
		}
		database.AddGroupFile(current.Id, f.Id)
		updateFileTerms(f.Id, current)
		inGroup[f.Id] = true
		if updated, ok := database.GetMetaDataById(f.Id); ok {
			f = updated
		}
		if isFingerprint(f.SHA1) {
			bySha[f.SHA1] = f
		}
		result.Added = append(result.Added, f)
	}
	return result, nil
}

// AttachUploadToGroup is AddFilesToGroup for a file uploaded into the group
// just now. A byte-identical copy already in the group wins: the new file is
// deleted and its duplicate returned. The caller has checked that the
// uploader may upload here; a file that may not go into a group (end-to-end
// encrypted, a file request upload) or a group the uploader cannot see is
// left alone, and the upload stays a private file.
func AttachUploadToGroup(g models.Group, file models.File, uploader models.User) (GroupDuplicate, bool) {
	current, err := liveGroup(g)
	if err != nil || !CanSeeGroup(current, uploader) {
		return GroupDuplicate{}, false
	}
	if file.Encryption.IsEndToEndEncrypted || file.UploadRequestId != "" {
		return GroupDuplicate{}, false
	}
	if existing, found := FindInGroup(current, file.SHA1); found && existing.Id != file.Id {
		DeleteFile(file.Id, true)
		fmt.Println("groups: duplicate upload " + file.Id + " of " + existing.Id + " in " + current.Id)
		return GroupDuplicate{FileId: file.Id, ExistingId: existing.Id, UploaderId: existing.UserId}, true
	}
	database.AddGroupFile(current.Id, file.Id)
	updateFileTerms(file.Id, current)
	return GroupDuplicate{}, false
}

// FindInGroup returns the live file in the group with this stored SHA1
func FindInGroup(g models.Group, sha1 string) (models.File, bool) {
	if !isFingerprint(sha1) {
		return models.File{}, false
	}
	for _, f := range GroupFiles(g) {
		if f.SHA1 == sha1 {
			return f, true
		}
	}
	return models.File{}, false
}

// StoredSha1IsPlain is false when new uploads are stored encrypted, because
// their SHA1 is then salted and a browser cannot compute it (D3)
func StoredSha1IsPlain() bool {
	return !isEncryptionRequested()
}

// RemoveFilesFromGroup takes files out of a group. The creator and the admin
// may remove any; a member only their own. Others are skipped, not errors.
// The files keep their current expiry: it is never shortened.
func RemoveFilesFromGroup(g models.Group, fileIds []string, actor models.User) (removed int, err error) {
	ids, err := removeFilesFromGroup(g, fileIds, actor)
	return len(ids), err
}

// RemoveNearCopies is RemoveFilesFromGroup for the duplicate view: the files
// go out because the group keeps keptId, which looks like each of them. What
// each of them was marked as downloaded for follows to keptId (the tick, or
// "new to me"), for every user. keptId must be a live file of the group and a
// near-copy of every file named; otherwise nothing is removed or copied.
func RemoveNearCopies(g models.Group, fileIds []string, keptId string, actor models.User) (removed int, err error) {
	current, err := liveGroup(g)
	if err != nil {
		return 0, err
	}
	if !CanSeeGroup(current, actor) {
		return 0, ErrGroupForbidden
	}
	inGroup := make(map[string]bool)
	for _, id := range database.GetGroupFileIds(current.Id) {
		inGroup[id] = true
	}
	kept, ok := database.GetMetaDataById(keptId)
	if !ok || !inGroup[keptId] || kept.IsPendingForDeletion() || IsExpiredFile(kept, time.Now().Unix()) {
		return 0, ErrNotACopy
	}
	for _, id := range fileIds {
		if !inGroup[id] {
			continue // skipped by the removal below, as without keptId
		}
		f, ok := database.GetMetaDataById(id)
		if !ok || !IsNearCopyPair(f, kept) {
			return 0, ErrNotACopy
		}
	}
	ids, err := removeFilesFromGroup(current, fileIds, actor)
	for _, id := range ids {
		database.CopyDownloads(id, keptId)
	}
	return len(ids), err
}

// removeFilesFromGroup removes what actor may and returns their ids
func removeFilesFromGroup(g models.Group, fileIds []string, actor models.User) (removed []string, err error) {
	current, err := liveGroup(g)
	if err != nil {
		return nil, err
	}
	if !CanSeeGroup(current, actor) {
		return nil, ErrGroupForbidden
	}
	inGroup := make(map[string]bool)
	for _, id := range database.GetGroupFileIds(current.Id) {
		inGroup[id] = true
	}
	manager := CanManageGroup(current, actor)
	for _, id := range fileIds {
		if !inGroup[id] {
			continue
		}
		if !manager {
			f, ok := database.GetMetaDataById(id)
			if !ok || f.UserId != actor.Id {
				continue
			}
		}
		database.RemoveGroupFile(current.Id, id)
		delete(inGroup, id)
		removed = append(removed, id)
	}
	return removed, nil
}

// UserSharesGroupWithFile reports whether the user is a member of a live
// group the file is in, for access.CanView
func UserSharesGroupWithFile(userId int, fileId string) bool {
	if userId == 0 || fileId == "" {
		return false
	}
	for _, groupId := range database.GetGroupIdsOfFile(fileId) {
		if g, ok := GetGroup(groupId); ok && IsGroupMember(g, userId) {
			return true
		}
	}
	return false
}

// GroupLock reports whether a file is in a live group, and the name of the
// latest-closing one: its expiry, download limit and password then follow the
// group and cannot be edited (D4)
func GroupLock(fileId string) (groupName string, locked bool) {
	var latest models.Group
	for _, groupId := range database.GetGroupIdsOfFile(fileId) {
		g, ok := GetGroup(groupId)
		if !ok {
			continue
		}
		if !locked || closesLater(g, latest) {
			latest = g
			locked = true
		}
	}
	return latest.Name, locked
}

// closesLater reports whether a closes after b; never closing is the latest
func closesLater(a, b models.Group) bool {
	if b.NeverExpires() {
		return false
	}
	return a.NeverExpires() || a.Expires > b.Expires
}

// cleanUpGroups deletes every group that has closed, with the collection
// behind its old link. Its files stay.
func cleanUpGroups(timeNow int64) {
	if !database.SupportsGroups() {
		return
	}
	for _, g := range database.GetAllGroups() {
		if g.IsExpired(timeNow) {
			deleteGroupAndLinks(g.Id)
		}
	}
}
