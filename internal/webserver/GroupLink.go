package webserver

// A collection turned into a group (Part 2, R4): who opened its link, what
// the old link does on /c, its two modes, and stopping it. Every rule lives
// in storage/CollectionGroups.go.

import (
	"net/http"
	"net/url"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/webserver/authentication"
)

type collectionViewer struct {
	Id   int    `json:"id"`
	Name string `json:"name"`
}

// groupFromCollectionPreview answers GET /groups/fromcollection/preview?collectionId=:
// who "Turn into a group" would add, by display name. For the collection's
// owner and the admin only.
func groupFromCollectionPreview(w http.ResponseWriter, r *http.Request) {
	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		writeJsonError(w, http.StatusUnauthorized, err)
		return
	}
	if !database.SupportsGroups() {
		writeJsonError(w, http.StatusNotImplemented, storage.ErrGroupsUnsupported)
		return
	}
	collection, ok := storage.GetCollection(r.URL.Query().Get("collectionId"))
	if !ok {
		writeJsonError(w, http.StatusNotFound, storage.ErrorCollectionNoFiles)
		return
	}
	if collection.UserId != user.Id && !(user.IsAdmin() && user.Id != 0) {
		writeJsonError(w, http.StatusForbidden, storage.ErrGroupForbidden)
		return
	}
	viewers := make([]collectionViewer, 0)
	for _, u := range storage.CollectionViewers(collection, user) {
		viewers = append(viewers, collectionViewer{Id: u.Id, Name: displayName(u)})
	}
	writeJson(w, struct {
		Viewers []collectionViewer `json:"viewers"`
	}{viewers})
}

// setGroupLinkMode answers POST /groups/linkmode {id, mode}: "invite" or "require"
func setGroupLinkMode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		groupIdRequest
		Mode string `json:"mode"`
	}
	user, ok := groupRequest(w, r, &req)
	if !ok {
		return
	}
	g, ok := visibleGroup(w, req.Id, user)
	if !ok {
		return
	}
	if err := storage.SetGroupLinkMode(g, req.Mode, user); err != nil {
		writeGroupError(w, err)
		return
	}
	writeJson(w, struct {
		Result string
		Mode   string
	}{"OK", req.Mode})
}

// stopGroupLink answers POST /groups/stoplink {id}: the collection behind the
// old link is deleted, so the link stops working
func stopGroupLink(w http.ResponseWriter, r *http.Request) {
	var req groupIdRequest
	user, ok := groupRequest(w, r, &req)
	if !ok {
		return
	}
	g, ok := visibleGroup(w, req.Id, user)
	if !ok {
		return
	}
	if err := storage.StopGroupLink(g, user); err != nil {
		writeGroupError(w, err)
		return
	}
	writeJson(w, answerOK)
}

// collectionGroupBanner is the invite-mode banner of a collection turned into
// a group: Join for a signed-in non-member, "Sign in to join" for anyone
// else, and a way to the group for those who can see it
type collectionGroupBanner struct {
	Token    string // the group's current invite token, read per request
	SignedIn bool
	CanSee   bool
	GroupUrl string
}

// seeOther redirects with 303, so a password form's POST is not carried on
func seeOther(w http.ResponseWriter, target string) {
	addNoCacheHeader(w)
	w.Header().Set("Location", target)
	w.WriteHeader(http.StatusSeeOther)
}

// collectionGroupGate is what /c does for a collection turned into a group,
// before its password gate. A link whose group has gone answers like a
// deleted collection. In require mode, whoever can see the group goes there.
// It returns the link, whether the collection has one, and whether it has
// answered the request.
func collectionGroupGate(w http.ResponseWriter, r *http.Request, collection models.Collection) (storage.CollectionLink, bool, bool) {
	link, linked := storage.CollectionLinkOf(collection.Id)
	if !linked {
		return link, false, false
	}
	if !link.Live {
		redirectOnIncorrectId(w, r, "error")
		return link, true, true
	}
	if link.Link.LinkMode == models.CollectionLinkRequire {
		if visitor, signedIn := authentication.SessionIdentity(r); signedIn && storage.CanSeeGroup(link.Group, visitor) {
			seeOther(w, groupUrl(link.Group))
			return link, true, true
		}
	}
	return link, true, false
}

// collectionGroupJoin is what /c does past the password gate: require mode
// sends everyone who cannot see the group to its join page, which signs in
// first and comes back. Invite mode returns the page's banner instead.
func collectionGroupJoin(w http.ResponseWriter, r *http.Request, link storage.CollectionLink) (*collectionGroupBanner, bool) {
	token := link.Group.InviteToken
	if link.Link.LinkMode == models.CollectionLinkRequire {
		seeOther(w, "join?t="+url.QueryEscape(token))
		return nil, true
	}
	visitor, signedIn := authentication.SessionIdentity(r)
	return &collectionGroupBanner{
		Token:    token,
		SignedIn: signedIn,
		CanSee:   signedIn && storage.CanSeeGroup(link.Group, visitor),
		GroupUrl: groupUrl(link.Group),
	}, false
}

// collectionClosed reports whether /cd and /cz must refuse this visitor: the
// collection's link requires joining a group they cannot see, or its group has gone
func collectionClosed(r *http.Request, collection models.Collection) bool {
	visitor, _ := authentication.SessionIdentity(r)
	return storage.CollectionClosedTo(collection.Id, visitor)
}

// groupLinkView is "Also shared as a link" on the group page, for its creator and the admin
type groupLinkView struct {
	Url     string
	Require bool
}

// groupLinkOf returns the group page's link panel, or nil
func groupLinkOf(g models.Group, serverUrl string) *groupLinkView {
	links := storage.GroupLinks(g)
	if len(links) == 0 {
		return nil
	}
	return &groupLinkView{Url: serverUrl + "c?id=" + links[0].CollectionId,
		Require: links[0].LinkMode == models.CollectionLinkRequire}
}
