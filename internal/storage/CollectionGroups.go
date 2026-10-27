package storage

// A collection turned into a group (Part 2, R4): its old link lives on as
// the group's, in one of two modes, and goes when the group goes.

import (
	"errors"
	"fmt"
	"time"

	"github.com/forceu/gokapi/internal/activity"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
)

var (
	ErrCollectionAlreadyGroup = errors.New("this collection is already a group")
	ErrGroupLinkMode          = errors.New("the old link either invites to join or requires joining")
	ErrNoGroupLink            = errors.New("this group is not shared as a link")
)

// FromCollectionOptions are the choices of the "Turn into a group" dialog
type FromCollectionOptions struct {
	// AddViewers makes everyone who opened the link while signed in a member
	AddViewers bool
	// LinkMode is how the old link behaves: models.CollectionLinkInvite
	// (also when empty) or models.CollectionLinkRequire
	LinkMode string
	// ViewerIds limits AddViewers to the people the dialog showed; nil adds
	// every current viewer
	ViewerIds []int
}

// FromCollectionResult is what GroupFromCollection did
type FromCollectionResult struct {
	GroupAddResult
	MembersAdded int
}

// CollectionLink is a collection's group link with its group. Live is false
// once the group has closed or is gone: the link then works no more.
type CollectionLink struct {
	Link  models.CollectionGroup
	Group models.Group
	Live  bool
}

// GroupFromCollection turns a collection into a group with its name and its
// live files ("Turn into a group"). Its people who opened the link while
// signed in become members if asked. The collection itself is not modified:
// it is linked to the group, which hides it from the Collections list, and
// its link lives on in the chosen mode. A collection becomes a group once.
// The collection's owner is the group's creator, also when the admin turns
// it into one: hiding it from their list must not cut them off, and the
// admin sees and manages every group without being a member. The
// group_created activity names the person converting as its actor (M7).
func GroupFromCollection(collectionId string, days int, opts FromCollectionOptions, actor models.User) (models.Group, FromCollectionResult, error) {
	if !database.SupportsGroups() {
		return models.Group{}, FromCollectionResult{}, ErrGroupsUnsupported
	}
	mode, err := linkMode(opts.LinkMode, true)
	if err != nil {
		return models.Group{}, FromCollectionResult{}, err
	}
	collection, ok := GetCollection(collectionId)
	if !ok {
		return models.Group{}, FromCollectionResult{}, ErrorCollectionNoFiles
	}
	if collection.UserId != actor.Id && !isGroupAdmin(actor) {
		return models.Group{}, FromCollectionResult{}, ErrGroupForbidden
	}
	if _, linked := database.GetCollectionGroup(collection.Id); linked {
		return models.Group{}, FromCollectionResult{}, ErrCollectionAlreadyGroup
	}
	creator := actor
	if collection.UserId != actor.Id {
		if owner, exists := database.GetUser(collection.UserId); exists {
			creator = owner
		}
	}
	g, err := createGroup(collection.Name, days, creator) // recorded once it survives
	if err != nil {
		return models.Group{}, FromCollectionResult{}, err
	}
	ids := make([]string, 0, len(collection.Files))
	for _, f := range collection.Files {
		ids = append(ids, f.Id)
	}
	added, err := AddFilesToGroup(g, ids, actor)
	result := FromCollectionResult{GroupAddResult: added}
	if err != nil {
		return g, result, err
	}
	if len(added.Added) == 0 {
		// Every file was refused (end-to-end encrypted, a file request
		// upload, someone else's, no longer live): no empty group is left
		// behind for the user to land in (M4)
		database.DeleteGroup(g.Id)
		return models.Group{}, result, ErrGroupNothingAdded
	}
	activity.GroupCreated(g, actor) // the person converting, not the owner (M7)
	if opts.AddViewers {
		shown := make(map[int]bool, len(opts.ViewerIds))
		for _, id := range opts.ViewerIds {
			shown[id] = true
		}
		now := time.Now().Unix()
		for _, viewer := range CollectionViewers(collection, actor) {
			if (opts.ViewerIds != nil && !shown[viewer.Id]) || IsGroupMember(g, viewer.Id) {
				continue
			}
			database.SaveGroupMember(models.GroupMember{GroupId: g.Id, UserId: viewer.Id, Joined: now, Role: models.GroupRoleMember})
			result.MembersAdded++
		}
	}
	database.SaveCollectionGroup(models.CollectionGroup{CollectionId: collection.Id, GroupId: g.Id, LinkMode: mode})
	return g, result, nil
}

// linkMode checks a mode from a request; an empty one is the default
// (invite) where allowed
func linkMode(mode string, emptyIsInvite bool) (string, error) {
	switch {
	case mode == models.CollectionLinkInvite || mode == models.CollectionLinkRequire:
		return mode, nil
	case mode == "" && emptyIsInvite:
		return models.CollectionLinkInvite, nil
	}
	return "", ErrGroupLinkMode
}

// CollectionViewers is who opened the collection's link while signed in and
// still exists, first opened first, leaving out its owner and the actor
// (who becomes the creator)
func CollectionViewers(collection models.Collection, actor models.User) []models.User {
	result := make([]models.User, 0)
	for _, seen := range database.GetCollectionViewers(collection.Id) {
		if seen.UserId == 0 || seen.UserId == collection.UserId || seen.UserId == actor.Id {
			continue
		}
		if u, ok := database.GetUser(seen.UserId); ok {
			result = append(result, u)
		}
	}
	return result
}

// CollectionLinkOf returns the group link of a collection, if it has one
func CollectionLinkOf(collectionId string) (CollectionLink, bool) {
	if collectionId == "" || !database.SupportsGroups() {
		return CollectionLink{}, false
	}
	link, ok := database.GetCollectionGroup(collectionId)
	if !ok {
		return CollectionLink{}, false
	}
	g, live := GetGroup(link.GroupId)
	return CollectionLink{Link: link, Group: g, Live: live}, true
}

// CollectionClosedTo reports whether a collection's own routes are closed to
// this person: its link was turned into a group that has gone, or that
// requires joining and they cannot see. Pass the zero User when signed out.
func CollectionClosedTo(collectionId string, user models.User) bool {
	link, linked := CollectionLinkOf(collectionId)
	if !linked {
		return false
	}
	if !link.Live {
		return true
	}
	return link.Link.LinkMode == models.CollectionLinkRequire && !CanSeeGroup(link.Group, user)
}

// GroupLinks returns the group's links whose collection still exists
func GroupLinks(g models.Group) []models.CollectionGroup {
	result := make([]models.CollectionGroup, 0)
	if !database.SupportsGroups() {
		return result
	}
	for _, link := range database.GetCollectionGroupsOfGroup(g.Id) {
		if _, ok := database.GetCollection(link.CollectionId); ok {
			result = append(result, link)
		}
	}
	return result
}

// managedLinks is the start of both link changes: the creator or the admin,
// a live group, and at least one link
func managedLinks(g models.Group, actor models.User) ([]models.CollectionGroup, error) {
	current, err := liveGroup(g)
	if err != nil {
		return nil, err
	}
	if !CanManageGroup(current, actor) {
		return nil, ErrGroupForbidden
	}
	links := GroupLinks(current)
	if len(links) == 0 {
		return nil, ErrNoGroupLink
	}
	return links, nil
}

// SetGroupLinkMode switches how the group's old link behaves. The creator
// and the admin only.
func SetGroupLinkMode(g models.Group, mode string, actor models.User) error {
	mode, err := linkMode(mode, false)
	if err != nil {
		return err
	}
	links, err := managedLinks(g, actor)
	if err != nil {
		return err
	}
	for _, link := range links {
		link.LinkMode = mode
		database.SaveCollectionGroup(link)
	}
	return nil
}

// StopGroupLink deletes the collection behind the group's old link, so the
// link stops working. The group and its files stay. The creator and the
// admin only.
func StopGroupLink(g models.Group, actor models.User) error {
	links, err := managedLinks(g, actor)
	if err != nil {
		return err
	}
	for _, link := range links {
		deleteLinkedCollection(link.CollectionId)
	}
	fmt.Println("groups: stopped the link of " + g.Id)
	return nil
}

// deleteLinkedCollection deletes a collection whose link was the group's,
// and the link with it. Callers log the group id: the collection id is the
// share link's secret.
func deleteLinkedCollection(collectionId string) {
	database.DeleteCollection(collectionId)
	database.DeleteCollectionGroup(collectionId)
}

// deleteGroupAndLinks deletes a group and the collections behind its old
// links: those links were only living on as the group's. The links are read
// first, because deleting the group drops them.
func deleteGroupAndLinks(groupId string) {
	links := database.GetCollectionGroupsOfGroup(groupId)
	for _, link := range links {
		deleteLinkedCollection(link.CollectionId)
	}
	if len(links) > 0 {
		fmt.Println("groups: deleted the linked collection of " + groupId)
	}
	database.DeleteGroup(groupId)
}
