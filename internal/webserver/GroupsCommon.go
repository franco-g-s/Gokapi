package webserver

// Helpers shared by every group page and endpoint (Part 2)

import (
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/people"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/webserver/authentication"
)

// groupJsonBodyLimit caps a group request body at 1 MiB
const groupJsonBodyLimit = 1 << 20

// displayName is what members see of a person (D5): User.Name is the
// lowercased Google email, so only the part before "@" is shown. A name
// without one is shown whole. Real display names come in Part 4.
func displayName(u models.User) string { return people.Name(u) }

// memberNames maps user ids to display names, for the members of a group and
// the owners of its files. People who no longer exist are left out.
func memberNames(g models.Group) map[int]string {
	return memberNamesOf(g, storage.GroupFiles(g))
}

// memberNamesOf is memberNames for a caller that already has the group's
// live files, so they are not read twice
func memberNamesOf(g models.Group, files []models.File) map[int]string {
	result := make(map[int]string)
	add := func(id int) {
		if _, done := result[id]; done {
			return
		}
		if u, ok := database.GetUser(id); ok {
			result[id] = displayName(u)
		}
	}
	for _, m := range database.GetGroupMembers(g.Id) {
		add(m.UserId)
	}
	for _, f := range files {
		add(f.UserId)
	}
	return result
}

// writeJson answers with v as uncached JSON
func writeJson(w http.ResponseWriter, v any) {
	addNoCacheHeader(w)
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	_ = json.NewEncoder(w).Encode(v)
}

// writeJsonError answers {"Result":"error","ErrorMessage":…} with the status
func writeJsonError(w http.ResponseWriter, status int, err error) {
	addNoCacheHeader(w)
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Result       string `json:"Result"`
		ErrorMessage string `json:"ErrorMessage"`
	}{"error", err.Error()})
}

// postOnly is false, with a 405 written, unless the request is a POST. A Lax
// session cookie rides on cross-site GETs, so no other method may change
// anything (D20).
func postOnly(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodPost {
		return true
	}
	w.Header().Set("Allow", http.MethodPost)
	writeJsonError(w, http.StatusMethodNotAllowed, errMethodNotAllowed)
	return false
}

var errMethodNotAllowed = errors.New("method not allowed")

// isJsonRequest reports whether the body is declared as JSON
func isJsonRequest(r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mediaType == "application/json"
}

// jsonOnly is false, with a 415 written, unless the request declares a JSON
// body. The Lax session cookie stops cross-site POSTs, but not same-site
// ones: a page on a sibling subdomain could post an HTML form whose
// text/plain body happens to parse as JSON. A form can never send
// application/json, and a script on another origin cannot either without a
// CORS preflight this server never answers.
func jsonOnly(w http.ResponseWriter, r *http.Request) bool {
	if isJsonRequest(r) {
		return true
	}
	writeJsonError(w, http.StatusUnsupportedMediaType, errJsonRequired)
	return false
}

var errJsonRequired = errors.New("the request body must be application/json")

// isSameOriginForm reports whether a form POST may come from this server's
// own pages: refused when Origin is present and is not the server's origin
// (a literal "null" included), or, without an Origin, when a Referer is
// present and points elsewhere. A request with neither is let through: some
// privacy settings strip both, and the Lax cookie still keeps out
// cross-site posts.
func isSameOriginForm(r *http.Request) bool {
	own := originOf(configuration.Get().ServerUrl)
	if own == "" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		return originOf(origin) == own
	}
	if referer := r.Header.Get("Referer"); referer != "" {
		return originOf(referer) == own
	}
	return true
}

// originOf is scheme://host[:port] of a URL, lower case; "" if it has none
func originOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return strings.ToLower(u.Scheme + "://" + u.Host)
}

var errForeignOrigin = errors.New("this form must be sent from this server's own pages")

// readJsonBody decodes a JSON request body of at most 1 MiB into v
func readJsonBody(r *http.Request, v any) error {
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, groupJsonBodyLimit)).Decode(v)
}

// groupListItem is one row of /groups/list
type groupListItem struct {
	Id           string `json:"id"`
	Name         string `json:"name"`
	Expires      int64  `json:"expires"`
	NeverExpires bool   `json:"neverExpires"`
	Files        int    `json:"files"`
	Members      int    `json:"members"`
	Role         string `json:"role"` // "creator", "member", or "" (admin, not a member)
}

// listGroups answers GET /groups/list: the caller's live groups. ?all=1 is
// every live group, for the admin only (D17); anyone else gets their own.
func listGroups(w http.ResponseWriter, r *http.Request) {
	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		writeJsonError(w, http.StatusUnauthorized, err)
		return
	}
	if !database.SupportsGroups() {
		writeJsonError(w, http.StatusNotImplemented, storage.ErrGroupsUnsupported)
		return
	}
	groups := storage.GroupsOfUser(user.Id)
	if r.URL.Query().Get("all") == "1" && user.IsAdmin() {
		groups = storage.GetAllGroups()
	}
	result := make([]groupListItem, 0, len(groups))
	for _, g := range groups {
		role, _ := storage.GroupRole(g, user.Id)
		result = append(result, groupListItem{
			Id:           g.Id,
			Name:         g.Name,
			Expires:      g.Expires,
			NeverExpires: g.NeverExpires(),
			Files:        len(storage.GroupFiles(g)),
			Members:      len(database.GetGroupMembers(g.Id)),
			Role:         role,
		})
	}
	writeJson(w, result)
}
