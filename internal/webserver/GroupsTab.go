package webserver

// The Groups tab, creating and joining groups, and the endpoints that manage
// one (Part 2, G6). Every rule lives in storage/Groups.go; these handlers only
// decode, ask and answer.

import (
	"errors"
	"html"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/helper"
	"github.com/forceu/gokapi/internal/logging"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/webserver/authentication"
	"github.com/forceu/gokapi/internal/webserver/errorHandling"
	"github.com/forceu/gokapi/internal/webserver/ratelimiter"
	"github.com/forceu/gokapi/internal/webserver/viewas"
)

var errTooManyJoins = errors.New("Too many attempts — wait a minute")

const msgInvalidInvite = "This invite link is no longer valid."

// closesLabel: "closes 12 Jan", "closes 12 Jan 2027" outside the current year, "never closes"
func closesLabel(g models.Group, now time.Time) string {
	if g.NeverExpires() {
		return "never closes"
	}
	closes := time.Unix(g.Expires, 0).In(now.Location())
	if closes.Year() == now.Year() {
		return "closes " + closes.Format("2 Jan")
	}
	return "closes " + closes.Format("2 Jan 2006")
}

// groupUrl is the group's page, relative to a top-level page
func groupUrl(g models.Group) string {
	return "g?id=" + url.QueryEscape(g.Id)
}

// groupErrorStatus maps a storage error to its HTTP status
func groupErrorStatus(err error) int {
	switch {
	case errors.Is(err, storage.ErrGroupForbidden):
		return http.StatusForbidden
	case errors.Is(err, storage.ErrGroupNotFound), errors.Is(err, storage.ErrNoGroupLink):
		return http.StatusNotFound
	case errors.Is(err, storage.ErrGroupsUnsupported):
		return http.StatusNotImplemented
	}
	return http.StatusBadRequest
}

func writeGroupError(w http.ResponseWriter, err error) {
	writeJsonError(w, groupErrorStatus(err), err)
}

// groupRequest starts every group mutation: POST only, a JSON body, a
// signed-in user, and the body decoded into v
func groupRequest(w http.ResponseWriter, r *http.Request, v any) (models.User, bool) {
	if !postOnly(w, r) || !jsonOnly(w, r) {
		return models.User{}, false
	}
	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		writeJsonError(w, http.StatusUnauthorized, err)
		return models.User{}, false
	}
	if err = readJsonBody(r, v); err != nil {
		writeJsonError(w, http.StatusBadRequest, err)
		return models.User{}, false
	}
	return user, true
}

// visibleGroup returns the group if the user may see it. A group is
// unlisted, so to anyone else it does not exist: 404, not 403.
func visibleGroup(w http.ResponseWriter, id string, user models.User) (models.Group, bool) {
	if !database.SupportsGroups() {
		writeJsonError(w, http.StatusBadRequest, storage.ErrGroupsUnsupported)
		return models.Group{}, false
	}
	g, ok := storage.GetGroup(id)
	if !ok || !storage.CanSeeGroup(g, user) {
		writeJsonError(w, http.StatusNotFound, storage.ErrGroupNotFound)
		return models.Group{}, false
	}
	return g, true
}

type okAnswer struct {
	Result string
}

var answerOK = okAnswer{Result: "OK"}

// ---------------------------------------------------------------- the tab

type groupRow struct {
	Id       string
	Name     string
	Url      string
	People   int
	Files    int
	Closes   string
	Role     string // "Creator", "Member", or "" when the admin is not a member
	IsMember bool
	// InviteUrl is what "Copy join link" copies. Every row has it: the tab
	// lists only the caller's groups, or every group for the admin, who may
	// manage them all.
	InviteUrl string
}

type groupExpiryChoice struct {
	Days    int
	Label   string
	Checked bool
}

// GroupsView is the Groups tab
type GroupsView struct {
	AdminView
	Supported     bool
	Groups        []groupRow
	ExpiryChoices []groupExpiryChoice
}

// groupExpiryChoices are the radios of every create form
func groupExpiryChoices() []groupExpiryChoice {
	result := make([]groupExpiryChoice, 0, len(storage.GroupExpiryDays))
	for _, days := range storage.GroupExpiryDays {
		label := strconv.Itoa(days) + " days"
		if days == 0 {
			label = "Never"
		} else if days == storage.GroupDefaultExpiryDays {
			label += " (suggested)"
		}
		result = append(result, groupExpiryChoice{Days: days, Label: label, Checked: days == storage.GroupDefaultExpiryDays})
	}
	return result
}

// showGroups shows /groups: your groups, or every group for the admin (D17)
func showGroups(w http.ResponseWriter, r *http.Request) {
	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		redirect(w, r, "login")
		return
	}
	view := GroupsView{
		AdminView:     *(&AdminView{}).convertGlobalConfig(ViewGroups, user),
		Supported:     database.SupportsGroups(),
		Groups:        make([]groupRow, 0),
		ExpiryChoices: groupExpiryChoices(),
	}
	if view.Supported {
		groups := storage.GroupsOfUser(user.Id)
		if user.Id != 0 && user.IsAdmin() {
			groups = storage.GetAllGroups()
		}
		now := time.Now()
		for _, g := range groups {
			role, member := storage.GroupRole(g, user.Id)
			label := ""
			switch role {
			case models.GroupRoleCreator:
				label = "Creator"
			case models.GroupRoleMember:
				label = "Member"
			}
			view.Groups = append(view.Groups, groupRow{
				Id:        g.Id,
				Name:      g.Name,
				Url:       "./" + groupUrl(g),
				People:    len(database.GetGroupMembers(g.Id)),
				Files:     len(storage.GroupFiles(g)),
				Closes:    closesLabel(g, now),
				Role:      label,
				IsMember:  member,
				InviteUrl: storage.GroupInviteUrl(g),
			})
		}
	}
	err = templateFolder.ExecuteTemplate(w, "groups_tab", &view)
	helper.CheckIgnoreTimeout(err)
}

// --------------------------------------------------------------- creating

// createGroup answers POST /groups/create {name, days}
func createGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Days *int   `json:"days"` // required: a missing one must not mean "never"
	}
	user, ok := groupRequest(w, r, &req)
	if !ok {
		return
	}
	if req.Days == nil {
		writeJsonError(w, http.StatusBadRequest, storage.ErrGroupExpiryChoice)
		return
	}
	g, err := storage.CreateGroup(req.Name, *req.Days, user)
	if err != nil {
		writeGroupError(w, err)
		return
	}
	writeJson(w, struct {
		Result    string
		Id        string
		Url       string
		InviteUrl string
		Code      string
	}{"OK", g.Id, "./" + groupUrl(g), storage.GroupInviteUrl(g), storage.GroupCode(g)})
}

// ---------------------------------------------------------------- joining

// isFormRequest reports whether the body is an urlencoded form, the join page's
func isFormRequest(r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mediaType == "application/x-www-form-urlencoded"
}

// joinGroup answers POST /groups/join: JSON {code} from the Join dialog, or
// the join page's form t=<token>, which then goes on to the group (303).
// The form must come from this server's own pages (isSameOriginForm);
// anything that is neither is a 415.
// Every attempt, right or wrong, is rate-limited per IP and per account.
func joinGroup(w http.ResponseWriter, r *http.Request) {
	if !postOnly(w, r) {
		return
	}
	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		writeJsonError(w, http.StatusUnauthorized, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, groupJsonBodyLimit)
	asJson := isJsonRequest(r)
	// Refused before the rate limiter, so a forged request cannot use up
	// the victim's join attempts
	if !asJson {
		if !isFormRequest(r) {
			writeJsonError(w, http.StatusUnsupportedMediaType, errJsonRequired)
			return
		}
		if !isSameOriginForm(r) {
			addNoCacheHeader(w)
			http.Error(w, errForeignOrigin.Error(), http.StatusForbidden)
			return
		}
	}
	if !ratelimiter.IsAllowedGroupJoin(logging.GetIpAddress(r), user.Id) {
		if asJson {
			writeJsonError(w, http.StatusTooManyRequests, errTooManyJoins)
		} else {
			tooManyJoinsPage(w, r)
		}
		return
	}
	if asJson {
		var req struct {
			Code string `json:"code"`
		}
		if err = readJsonBody(r, &req); err != nil {
			writeJsonError(w, http.StatusBadRequest, err)
			return
		}
		g, err := storage.JoinGroupByCode(req.Code, user)
		if err != nil {
			writeGroupError(w, err)
			return
		}
		writeJson(w, struct {
			Result string
			Id     string
			Url    string
		}{"OK", g.Id, "./" + groupUrl(g)})
		return
	}
	token := ""
	if r.ParseForm() == nil {
		token = r.PostForm.Get("t")
	}
	// Locations are relative to /groups/join, and set literally because
	// http.Redirect would rewrite them into absolute paths
	addNoCacheHeader(w)
	g, err := storage.JoinGroupByToken(token, user)
	if err != nil {
		// A page rendered here would load its relative assets from /groups/,
		// so the join page itself says what is wrong with the link
		w.Header().Set("Location", "../join?t="+url.QueryEscape(token))
		w.WriteHeader(http.StatusSeeOther)
		return
	}
	w.Header().Set("Location", "../"+groupUrl(g))
	w.WriteHeader(http.StatusSeeOther)
}

// tooManyJoinsPage answers a rate-limited join form with 429. It is served
// under /groups/, where the header's relative assets would not load, so it is
// a page of its own with a link back to the invite.
func tooManyJoinsPage(w http.ResponseWriter, r *http.Request) {
	back := "../groups"
	if r.ParseForm() == nil && r.PostForm.Get("t") != "" {
		back = "../join?t=" + url.QueryEscape(r.PostForm.Get("t"))
	}
	addNoCacheHeader(w)
	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	w.Header().Set("Retry-After", "6")
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = io.WriteString(w, `<!doctype html><html lang="en"><head><meta charset="utf-8">`+
		`<meta name="viewport" content="width=device-width, initial-scale=1"><title>Too many attempts</title></head>`+
		`<body style="font-family:sans-serif;text-align:center;margin-top:4em">`+
		`<p>`+html.EscapeString(errTooManyJoins.Error())+`</p>`+
		`<p><a href="`+html.EscapeString(back)+`">Try again</a></p></body></html>`)
}

// capitalise upper-cases the first letter of a storage error for a page
func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// JoinView is the /join confirmation page
type JoinView struct {
	AdminView
	Message   string // set instead of the card when the link cannot be used
	GroupName string
	People    int
	Closes    string
	Token     string
}

func showJoinMessage(w http.ResponseWriter, user models.User, status int, message string) {
	view := JoinView{AdminView: *(&AdminView{}).convertGlobalConfig(ViewGroups, user), Message: message}
	addNoCacheHeader(w)
	w.WriteHeader(status)
	helper.CheckIgnoreTimeout(templateFolder.ExecuteTemplate(w, "group_join", &view))
}

// showJoin shows /join?t=: the confirmation card for an invite link. It is
// public, so it signs in first and comes back. It never joins anyone: a Lax
// session cookie rides on cross-site GETs (D20), and after an internal
// sign-in the browser re-POSTs the login form here (307). Only the card's
// own form, POST /groups/join, joins.
func showJoin(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("t")
	user, loggedIn, err := authentication.IsAuthenticated(w, r)
	if err != nil {
		errorHandling.RedirectToErrorPage(w, r, "Unable to log in", "The following error was raised: "+err.Error(), errorHandling.WidthDefault)
		return
	}
	if !loggedIn {
		authentication.SetReturnTo(w, "join?t="+token)
		redirect(w, r, "login")
		return
	}
	if user.ResetPassword && configuration.Get().Authentication.Method == models.AuthenticationInternal {
		redirect(w, r, "changePassword")
		return
	}
	// Part 3, V1: an admin in "View as…" sees the card as the target
	if target, ok := viewas.SwapFor(r, user); ok {
		viewas.Touch(user.Id)
		user = target
	}
	if !database.SupportsGroups() {
		showJoinMessage(w, user, http.StatusBadRequest, capitalise(storage.ErrGroupsUnsupported.Error()))
		return
	}
	g, found := models.Group{}, false
	if token != "" {
		g, found = database.GetGroupByToken(token)
	}
	if !found || g.IsExpired(time.Now().Unix()) {
		showJoinMessage(w, user, http.StatusNotFound, msgInvalidInvite)
		return
	}
	if storage.IsGroupMember(g, user.Id) {
		redirect(w, r, groupUrl(g))
		return
	}
	view := JoinView{
		AdminView: *(&AdminView{}).convertGlobalConfig(ViewGroups, user),
		GroupName: g.Name,
		People:    len(database.GetGroupMembers(g.Id)),
		Closes:    closesLabel(g, time.Now()),
		Token:     token,
	}
	addNoCacheHeader(w)
	helper.CheckIgnoreTimeout(templateFolder.ExecuteTemplate(w, "group_join", &view))
}

// redirectGroupJoin answers /g/join?t=, the public invite link. A page served
// under /g/ would load its relative assets from /g/, so this only sends the
// browser on to the top-level /join (D19). The Location is set literally:
// http.Redirect would turn it into an absolute path.
func redirectGroupJoin(w http.ResponseWriter, r *http.Request) {
	addNoCacheHeader(w)
	w.Header().Set("Location", "../join?t="+url.QueryEscape(r.URL.Query().Get("t")))
	w.WriteHeader(http.StatusTemporaryRedirect)
}

// --------------------------------------------------------------- managing

type groupIdRequest struct {
	Id string `json:"id"`
}

// renameGroup answers POST /groups/rename {id, name}
func renameGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		groupIdRequest
		Name string `json:"name"`
	}
	user, ok := groupRequest(w, r, &req)
	if !ok {
		return
	}
	g, ok := visibleGroup(w, req.Id, user)
	if !ok {
		return
	}
	g, err := storage.RenameGroup(g, req.Name, user)
	if err != nil {
		writeGroupError(w, err)
		return
	}
	writeJson(w, struct {
		Result string
		Name   string
	}{"OK", g.Name})
}

// extendGroup answers POST /groups/extend {id, days}
func extendGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		groupIdRequest
		Days *int `json:"days"` // required: a missing one must not mean "never"
	}
	user, ok := groupRequest(w, r, &req)
	if !ok {
		return
	}
	g, ok := visibleGroup(w, req.Id, user)
	if !ok {
		return
	}
	if req.Days == nil {
		writeJsonError(w, http.StatusBadRequest, storage.ErrGroupExpiryChoice)
		return
	}
	g, err := storage.ExtendGroup(g, *req.Days, user)
	if err != nil {
		writeGroupError(w, err)
		return
	}
	writeJson(w, struct {
		Result       string
		Expires      int64
		NeverExpires bool
		Closes       string
	}{"OK", g.Expires, g.NeverExpires(), closesLabel(g, time.Now())})
}

// resetGroupCode answers POST /groups/resetcode {id}
func resetGroupCode(w http.ResponseWriter, r *http.Request) {
	var req groupIdRequest
	user, ok := groupRequest(w, r, &req)
	if !ok {
		return
	}
	g, ok := visibleGroup(w, req.Id, user)
	if !ok {
		return
	}
	g, err := storage.ResetGroupCode(g, user)
	if err != nil {
		writeGroupError(w, err)
		return
	}
	writeJson(w, struct {
		Result    string
		InviteUrl string
		Code      string
	}{"OK", storage.GroupInviteUrl(g), storage.GroupCode(g)})
}

// removeGroupMember answers POST /groups/removemember {id, userId}
func removeGroupMember(w http.ResponseWriter, r *http.Request) {
	var req struct {
		groupIdRequest
		UserId int `json:"userId"`
	}
	user, ok := groupRequest(w, r, &req)
	if !ok {
		return
	}
	g, ok := visibleGroup(w, req.Id, user)
	if !ok {
		return
	}
	if err := storage.RemoveGroupMember(g, req.UserId, user); err != nil {
		writeGroupError(w, err)
		return
	}
	writeJson(w, answerOK)
}

// leaveGroup answers POST /groups/leave {id}
func leaveGroup(w http.ResponseWriter, r *http.Request) {
	var req groupIdRequest
	user, ok := groupRequest(w, r, &req)
	if !ok {
		return
	}
	g, ok := visibleGroup(w, req.Id, user)
	if !ok {
		return
	}
	if err := storage.LeaveGroup(g, user); err != nil {
		writeGroupError(w, err)
		return
	}
	writeJson(w, answerOK)
}

// deleteGroup answers POST /groups/delete {id}
func deleteGroup(w http.ResponseWriter, r *http.Request) {
	var req groupIdRequest
	user, ok := groupRequest(w, r, &req)
	if !ok {
		return
	}
	g, ok := visibleGroup(w, req.Id, user)
	if !ok {
		return
	}
	if err := storage.DeleteGroup(g, user); err != nil {
		writeGroupError(w, err)
		return
	}
	writeJson(w, answerOK)
}

// removeGroupFiles answers POST /groups/removefiles {id, fileIds, keptId?} (D4).
// keptId is sent by the duplicate view only: the near-copy that stays in the
// group, which then also counts as downloaded by everyone who had downloaded
// a file removed here. Without it nothing is copied.
func removeGroupFiles(w http.ResponseWriter, r *http.Request) {
	var req struct {
		groupIdRequest
		FileIds []string `json:"fileIds"`
		KeptId  string   `json:"keptId"`
	}
	user, ok := groupRequest(w, r, &req)
	if !ok {
		return
	}
	g, ok := visibleGroup(w, req.Id, user)
	if !ok {
		return
	}
	var removed int
	var err error
	if req.KeptId != "" {
		removed, err = storage.RemoveNearCopies(g, req.FileIds, req.KeptId, user)
	} else {
		removed, err = storage.RemoveFilesFromGroup(g, req.FileIds, user)
	}
	if err != nil {
		writeGroupError(w, err)
		return
	}
	writeJson(w, struct {
		Result  string
		Removed int
	}{"OK", removed})
}
