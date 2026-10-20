package webserver

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"unicode"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/helper"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/webserver/access"
	"github.com/forceu/gokapi/internal/webserver/authentication"
)

// The one file list every page uses (spec: "One file list everywhere"). A
// page differs only in what it lists and which actions it allows; both come
// from its context.
const (
	ctxFiles            = "files"
	ctxUpload           = "upload"
	ctxCollectionAdmin  = "collection-admin"
	ctxFileRequest      = "filerequest"
	ctxCollectionPublic = "collection-public"
	ctxGroup            = "group" // a group page (Part 2)
)

const (
	viewGrid  = "grid"
	viewBoard = "board"
	viewList  = "list"
)

// browserActions is one row of the per-page action table. The keys are the
// data-menu / data-act values the front end dispatches on.
type browserActions struct {
	Menu        []string
	Bulk        []string
	DefaultView string
}

var (
	manageMenu = []string{"open", "download", "copy", "hotlink", "share", "qr", "email", "edit", "replace", "add-to-collection", "add-to-group", "delete"}
	manageBulk = []string{"download", "zip", "photos", "link", "add-to-collection", "add-to-group", "change-expiry", "delete"}
)

// browserActionTable is binding (spec, "Per-page content and actions")
var browserActionTable = map[string]browserActions{
	ctxFiles:  {Menu: manageMenu, Bulk: manageBulk, DefaultView: viewGrid},
	ctxUpload: {Menu: manageMenu, Bulk: manageBulk, DefaultView: viewGrid},
	ctxCollectionAdmin: {
		Menu:        append(slices.Clone(manageMenu), "remove-from-collection"),
		Bulk:        append(slices.Clone(manageBulk), "remove-from-collection"),
		DefaultView: viewList,
	},
	ctxFileRequest: {
		Menu:        []string{"open", "download", "delete"},
		Bulk:        []string{"download", "zip", "photos", "delete"},
		DefaultView: viewList,
	},
	ctxCollectionPublic: {
		Menu:        []string{"open", "download"},
		Bulk:        []string{"download", "zip", "photos"},
		DefaultView: viewList,
	},
	// Edit is here (D4 revision): renaming and replacing content. Expiry,
	// download limit and password follow the group (browserItem.GroupLock).
	ctxGroup: {
		Menu:        []string{"open", "download", "edit", "replace", "remove-from-group", "delete"},
		Bulk:        []string{"download", "zip", "photos", "remove-from-group", "delete"},
		DefaultView: viewBoard,
	},
}

// browserItem is one file in a fileBrowser
type browserItem struct {
	File       models.FileApiOutput
	Uploader   string
	Mine       bool
	TakenAt    int64
	Preview    previewInfo
	Bytes      int64
	CanEdit    bool
	CanDelete  bool
	CanReplace bool
	// DownloadUrl is the original file for this context: /files/download for
	// owners, ./cd?id=…&file=… on a collection page, "" where none exists
	// (end-to-end encrypted files; file requests, whose download is a
	// presign flow in JS)
	DownloadUrl string
	Icon        string // Bootstrap icon for files without a thumbnail
	// Group pages (Part 2)
	UploaderId int
	Downloaded bool   // a Downloads row exists for the viewer (G4); the caller fills it in
	CanRemove  bool   // may take it out of this group (D4); the caller fills it in
	Initials   string // of Uploader, for the tile's badge
	// GroupLock is the name of the live group whose terms the file follows
	// (storage.GroupLock), on every page with the edit dialog: its expiry,
	// download limit and password cannot be edited there (D4)
	GroupLock string
	// Foreign: the viewer reaches the file only as a member of a group it is
	// in, and may not edit it. Its management data (the uploader's own /d
	// and hotlink, download counts and terms) is neither sent nor rendered
	// (I2): a group file has unlimited downloads, so its /d link would be a
	// sign-in-free link the uploader never handed out.
	Foreign bool
}

// makeForeign marks an item as reached only through a group and blanks its
// management data, so no page or JSON answer can carry it
func (item *browserItem) makeForeign() {
	item.Foreign = true
	f := &item.File
	f.UrlDownload, f.UrlHotlink, f.HotlinkId = "", "", ""
	f.ExpireAt, f.ExpireAtString = 0, ""
	f.DownloadsRemaining, f.DownloadCount = 0, 0
	f.UnlimitedDownloads, f.UnlimitedTime, f.IsPasswordProtected = false, false, false
}

// browserPerson is one choice of a group page's person filter
type browserPerson struct {
	Id   int
	Name string
}

// fileBrowser is everything html_file_browser.tmpl's "file_browser" needs
type fileBrowser struct {
	Id           string // unique DOM id: several browsers may share a page
	Context      string // one of the ctx* constants
	Items        []browserItem
	DefaultView  string
	Menu         []string
	Bulk         []string
	ShowScope    bool // the My files / Everything switch: files with PERM_LIST only
	ShowUploader bool
	CollectionId string // collection-admin and collection-public
	// ZipUrl is the zip endpoint up to and including the parameter that takes
	// the comma-separated ids, e.g. "./files/zip?ids=" or
	// "./cz?id=<c>&files=". Empty: no "Download as .zip" button.
	ZipUrl string
	// ZipAllUrl is the same zip without files=, which means every live
	// member: used when everything is selected and nothing is filtered out,
	// so a select-all of ~1,000 items never builds a 16 KB URL (I3). Only
	// /cz and /gz have it.
	ZipAllUrl string
	// ZipByHandler shows the "Download as .zip" button although ZipUrl is
	// empty: a handler registered for the context (FGS.registerFileActions)
	// supplies the zip. File requests do this, because /files/zip refuses
	// request uploads.
	ZipByHandler bool
	ActiveUser   models.User
	// Group pages (Part 2, WithGroup)
	GroupId string
	// DownloadBase is a live add's download URL up to the file id; "" is the
	// owner download where the list Manages
	DownloadBase string
	// SortTakenAsc puts Board, Grid and live adds in shooting order, and
	// sorts List by Taken ascending (D13)
	SortTakenAsc bool
	// GroupTools: the person filter, "Hide what I've downloaded", "Select
	// everything new to me" and the downloaded ticks
	GroupTools bool
	People     []browserPerson // the person filter's choices
	UploadSink bool            // finished uploads on this page are added here (G7)
}

// newFileBrowser fills Menu, Bulk and DefaultView from the action table.
// Callers on collection pages then call WithCollection.
func newFileBrowser(id, context string, items []browserItem, user models.User) fileBrowser {
	actions, ok := browserActionTable[context]
	if !ok {
		panic("unknown file browser context " + context)
	}
	b := fileBrowser{
		Id:          id,
		Context:     context,
		Items:       items,
		DefaultView: actions.DefaultView,
		Menu:        slices.Clone(actions.Menu),
		Bulk:        slices.Clone(actions.Bulk),
		ActiveUser:  user,
	}
	listsOthers := user.HasPermissionListOtherUploads()
	b.ShowScope = context == ctxFiles && listsOthers
	b.ShowUploader = context == ctxFiles || (context == ctxCollectionAdmin && listsOthers)
	switch context {
	case ctxFiles, ctxUpload, ctxCollectionAdmin:
		b.ZipUrl = "./files/zip?ids="
	}
	return b
}

// HasMenu reports whether the "⋯" menu offers this action
func (b fileBrowser) HasMenu(key string) bool { return slices.Contains(b.Menu, key) }

// HasBulk reports whether the selection bar offers this action
func (b fileBrowser) HasBulk(key string) bool { return slices.Contains(b.Bulk, key) }

// Manages is true where the list belongs to its owner: management columns
// (expiry, download count) and data for the admin actions are rendered.
// Items marked Foreign never carry the data, whatever the list.
func (b fileBrowser) Manages() bool { return b.HasMenu("edit") }

// ManagesColumns is true where the List view shows the Expires and Downloads
// columns: a managed list, but not a group page, where most files are other
// people's and their terms follow the group anyway (I2)
func (b fileBrowser) ManagesColumns() bool { return b.Manages() && b.Context != ctxGroup }

// ItemPerms is true where the menu carries data-can-delete and
// data-can-remove, with or without the rest of the management data (Manages)
func (b fileBrowser) ItemPerms() bool { return b.HasMenu("delete") || b.HasMenu("remove-from-group") }

// LegacyIds is true where the page holds a single browser (Files, Upload):
// only there are the upload page's element ids (row-, cell-name-,
// cell-downloads-, url-button-) rendered. Pages with one browser per
// collection or request may list a file twice, and an id must not repeat.
func (b fileBrowser) LegacyIds() bool { return b.Context == ctxFiles || b.Context == ctxUpload }

// WithCollection scopes the browser to a collection: CollectionId, and on
// the public page the counted collection zip of the selected members.
// Admin browsers keep the uncounted owner zip.
func (b *fileBrowser) WithCollection(c models.Collection) {
	b.CollectionId = c.Id
	if b.Context == ctxCollectionPublic {
		b.ZipUrl = "./cz?id=" + c.Id + "&files="
		b.ZipAllUrl = "./cz?id=" + c.Id
	}
}

// WithGroup scopes the browser to a group page: the group's counted
// download and zip, shooting order, the group tools and its people
func (b *fileBrowser) WithGroup(g models.Group, people []browserPerson) {
	b.GroupId = g.Id
	b.ZipUrl = "./gz?id=" + g.Id + "&files="
	b.ZipAllUrl = "./gz?id=" + g.Id
	b.DownloadBase = "./gd?id=" + g.Id + "&file="
	b.SortTakenAsc = true
	b.GroupTools = true
	b.ShowUploader = true
	b.People = people
}

// InfoUrl is where a tile whose preview is pending polls for it, up to the
// "id=" its file id is appended to
func (b fileBrowser) InfoUrl() string {
	if b.Context == ctxCollectionPublic {
		return "./p/info?c=" + b.CollectionId + "&id="
	}
	if b.Context == ctxGroup {
		// Members may not list each other's files; CanView admits them
		return "./p/info?id="
	}
	return "./files/previewinfo?id="
}

// browserTile feeds the tile, row and "⋯" menu sub-templates one item
type browserTile struct {
	B     fileBrowser
	Item  browserItem
	Eager bool // among the first screen: fetched at high priority, not lazily
}

// eagerTiles is about one screen of thumbnails (spec, "Board layout")
const eagerTiles = 12

// TileAt pairs the index-th item with its browser; -1 for a template
func (b fileBrowser) TileAt(index int, item browserItem) browserTile {
	return browserTile{B: b, Item: item, Eager: index >= 0 && index < eagerTiles}
}

// Blank is the empty item the live-add <template>s are rendered from
func (b fileBrowser) Blank() browserItem { return browserItem{} }

// browserBulk feeds the selection-bar button sub-template
type browserBulk struct {
	Key string
	B   fileBrowser
}

// BulkButton pairs a selection-bar action with its browser
func (b fileBrowser) BulkButton(key string) browserBulk { return browserBulk{Key: key, B: b} }

// Perm tells the front end what the active user may do with files added
// live, which the server never rendered
func (b fileBrowser) Perm(name string) bool {
	u := b.ActiveUser
	switch name {
	case "edit-others":
		return u.HasPermissionEditOtherUploads()
	case "delete-others":
		return u.HasPermissionDeleteOtherUploads()
	case "replace":
		return u.HasPermissionReplace()
	case "replace-others":
		return u.HasPermissionReplaceOtherUploads()
	}
	return false
}

// browserItemFromFile builds one item for a context. The request decides,
// through previewUrls, whether a video may stream for this viewer.
// scopeId is the collection id on a collection page (collection-public),
// where it also scopes preview access, and the group id on a group page;
// uploaderNames maps user ids to names.
func browserItemFromFile(r *http.Request, f models.File, user models.User, context, scopeId string, uploaderNames map[int]string) browserItem {
	config := configuration.Get()
	out, err := f.ToFileApiOutput(config.ServerUrl, config.IncludeFilename)
	helper.Check(err)
	item := browserItem{File: out, Bytes: f.SizeBytes, Icon: fileTypeIcon(out.Name)}
	previewCollection := ""
	switch context {
	case ctxCollectionPublic:
		previewCollection = scopeId
		item.DownloadUrl = "./cd?id=" + scopeId + "&file=" + f.Id
	case ctxGroup:
		// Previews stay unscoped: CanView admits the group's members
		item.DownloadUrl = "./gd?id=" + scopeId + "&file=" + f.Id
	case ctxFileRequest:
		item.DownloadUrl = ""
	default:
		item.DownloadUrl = directDownloadUrl(out)
	}
	if context != ctxCollectionPublic {
		own := user.Id != 0 && f.UserId == user.Id
		item.Mine = own
		item.Uploader = uploaderNames[f.UserId]
		item.CanEdit = own || user.HasPermissionEditOtherUploads()
		item.CanDelete = own || user.HasPermissionDeleteOtherUploads()
		item.CanReplace = item.CanEdit && !out.IsEndToEndEncrypted &&
			((own && user.HasPermissionReplace()) || user.HasPermissionReplaceOtherUploads())
	}
	if context == ctxGroup {
		item.UploaderId = f.UserId
		item.Initials = initialsOf(item.Uploader)
		if !item.CanEdit {
			item.makeForeign()
		}
	}
	if slices.Contains(browserActionTable[context].Menu, "edit") {
		item.GroupLock, _ = storage.GroupLock(f.Id)
	}
	item.Preview = previewUrls(r, f, previewCollection, item.DownloadUrl)
	item.TakenAt = item.Preview.TakenAt
	return item
}

// initialsOf is the avatar badge of a name: the first letters of its first
// and last parts, upper case ("anna.mueller" -> "AM", "ben" -> "B").
// media.js (initialsOf) holds the same rule for files added live.
func initialsOf(name string) string {
	parts := strings.FieldsFunc(name, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	if len(parts) == 0 {
		return ""
	}
	first := []rune(parts[0])[:1]
	if len(parts) == 1 {
		return strings.ToUpper(string(first))
	}
	last := []rune(parts[len(parts)-1])[:1]
	return strings.ToUpper(string(first) + string(last))
}

// writePreviewInfo answers a tile polling for its preview. Never cached, so
// a "pending" cannot stick; never counted, since nothing is downloaded.
func writePreviewInfo(w http.ResponseWriter, info previewInfo) {
	addNoCacheHeader(w)
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	_ = json.NewEncoder(w).Encode(info)
}

// Handling of /files/previewinfo
// The preview of one file the user may list (their own, or anyone's with
// PERM_LIST), for a tile whose preview was still pending when the page loaded.
func filePreviewInfo(w http.ResponseWriter, r *http.Request) {
	user, err := authentication.GetUserFromRequest(r)
	if err != nil {
		addNoCacheHeader(w)
		http.NotFound(w, r)
		return
	}
	file, ok := storage.GetFile(r.URL.Query().Get("id"))
	if !ok || (file.UserId != user.Id && !user.HasPermissionListOtherUploads()) || !access.CanView(r, file, "") {
		addNoCacheHeader(w)
		http.NotFound(w, r)
		return
	}
	out, err := file.ToFileApiOutput(configuration.Get().ServerUrl, configuration.Get().IncludeFilename)
	helper.Check(err)
	// A request upload has no direct download (downloadOwnFile refuses it):
	// the page supplies its own presigned flow
	download := directDownloadUrl(out)
	if file.UploadRequestId != "" {
		download = ""
	}
	writePreviewInfo(w, previewUrls(r, file, "", download))
}

// Handling of /p/info
// The public twin of /files/previewinfo: anyone access.CanView admits, with
// c= scoping it to a collection page. The download URL is left out; the page
// already has it.
func publicPreviewInfo(w http.ResponseWriter, r *http.Request) {
	collectionId := r.URL.Query().Get("c")
	file, ok := storage.GetFile(r.URL.Query().Get("id"))
	if !ok || !access.CanView(r, file, collectionId) {
		addNoCacheHeader(w)
		http.NotFound(w, r)
		return
	}
	writePreviewInfo(w, previewUrls(r, file, collectionId, ""))
}
