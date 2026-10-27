// Package activity records one row per event the admin's feed reports
// (Part 4, A): uploads, downloads, groups, sign-ins, warnings and "View as…".
// Rows are written synchronously when the event happens, and are not log
// lines. The notifier reads them through GET /api/activity.
package activity

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/helper"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/people"
)

const (
	// diskThreshold is when the disk warning fires: 20 GB free, above the
	// 10 GB at which live refuses uploads (Q2)
	diskThreshold uint64 = 20 << 30
	diskInterval         = 10 * time.Minute
	retention            = 30 * 24 * time.Hour
	pruneInterval        = 24 * time.Hour

	// DefaultLimit and MaxLimit bound one feed page
	DefaultLimit = 500
	MaxLimit     = 1000
)

// saveActivity is database.SaveActivity; tests replace it to simulate a failing write
var saveActivity = database.SaveActivity

// record writes one row, stamped now. A no-op without group support (Redis).
// Best-effort: a failing write prints one stdout line and never aborts the
// upload, download or zip stream whose real work has already succeeded.
func record(a models.Activity, fill ...func(*models.Activity)) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("activity: cannot record %s: %v\n", a.Kind, r)
		}
	}()
	if !database.SupportsGroups() {
		return
	}
	for _, f := range fill {
		f(&a)
	}
	a.At = time.Now().Unix()
	saveActivity(a)
}

// recordFile is record for a file event. The group lookup runs inside
// record's recover, so it is best-effort too.
func recordFile(a models.Activity, f models.File) {
	record(a, func(a *models.Activity) { *a = withFile(*a, f) })
}

// withFile fills the file fields, and the file's group as it is right now
func withFile(a models.Activity, f models.File) models.Activity {
	a.OwnerId = f.UserId
	a.FileId = f.Id
	a.FileName = f.Name
	a.FileSize = f.SizeBytes
	a.FileKind = fileKind(f.ContentType, f.Name)
	if ids := database.GetGroupIdsOfFile(f.Id); len(ids) > 0 {
		a.GroupId = ids[0]
		if g, ok := database.GetGroup(ids[0]); ok {
			a.GroupName = g.Name
		}
	}
	return a
}

func withGroup(a models.Activity, g models.Group) models.Activity {
	a.GroupId = g.Id
	a.GroupName = g.Name
	return a
}

var imageExt = []string{".heic", ".heif", ".jpg", ".jpeg", ".png", ".gif", ".webp", ".avif", ".dng"}
var videoExt = []string{".mov", ".mp4", ".m4v", ".avi", ".mkv", ".webm"}

// fileKind is "image", "video" or "file"
func fileKind(contentType, name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch {
	case strings.HasPrefix(contentType, "image/") || contains(imageExt, ext):
		return "image"
	case strings.HasPrefix(contentType, "video/") || contains(videoExt, ext):
		return "video"
	}
	return "file"
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Upload records a finished upload that stays. It is called on the line after
// each logging.LogUpload, so a duplicate deleted by attachToGroup never
// reaches it (M6). A file-request upload comes from someone without an
// account: the passed user is the request's owner, so the actor is 0.
func Upload(f models.File, actor models.User) {
	a := models.Activity{Kind: models.ActivityUpload, ActorId: actor.Id}
	if f.UploadRequestId != "" {
		a.ActorId = 0
	}
	recordFile(a, f)
}

// Download records one original file served, with the same user and route
// as its Downloads row. actorId 0 is "someone".
func Download(f models.File, actorId int, via string) {
	recordFile(models.Activity{Kind: models.ActivityDownload, ActorId: actorId, Via: via}, f)
}

// GroupCreated records a new group
func GroupCreated(g models.Group, creator models.User) {
	record(withGroup(models.Activity{Kind: models.ActivityGroupCreated, ActorId: creator.Id}, g))
}

// GroupJoined records a person joining a group they were not in
func GroupJoined(g models.Group, u models.User) {
	record(withGroup(models.Activity{Kind: models.ActivityGroupJoined, ActorId: u.Id}, g))
}

// FirstSignIn records an account's first sign-in
func FirstSignIn(u models.User) {
	record(models.Activity{Kind: models.ActivityFirstSignIn, ActorId: u.Id})
}

// OnSignIn is the sign-in hook: it records first_signin when the sign-in
// created the account, and nothing otherwise
func OnSignIn(u models.User, _ map[string]any, created bool) {
	if created {
		FirstSignIn(u)
	}
}

// SignInBlocked records a sign-in the blocklist refused (task K calls it)
func SignInBlocked(email string) {
	record(models.Activity{Kind: models.ActivitySignInBlocked, Detail: email})
}

// PreviewFailed records a file whose preview failed for the last time (Q3)
func PreviewFailed(f models.File) {
	recordFile(models.Activity{Kind: models.ActivityWarningPreview}, f)
}

// ViewAs records the start or end of a "View as…" (Q13). The actor is the
// admin; detail is "target=<id>" or "target=<id> reason=<reason>".
func ViewAs(kind string, admin, target models.User, reason string) {
	detail := "target=" + strconv.Itoa(target.Id)
	if reason != "" {
		detail += " reason=" + reason
	}
	record(models.Activity{Kind: kind, ActorId: admin.Id, Detail: detail})
}

type diskWatcher struct {
	threshold uint64
	low       bool
}

func newDiskWatcher(threshold uint64) *diskWatcher { return &diskWatcher{threshold: threshold} }

// check writes one warning_disk each time free space falls below the
// threshold, and re-arms only once it has climbed back above it (Q2)
func (d *diskWatcher) check(free uint64) {
	if free < d.threshold {
		if !d.low {
			d.low = true
			record(models.Activity{Kind: models.ActivityWarningDisk, Detail: strconv.FormatUint(free, 10)})
		}
		return
	}
	d.low = false
}

var startOnce sync.Once

// StartWatchers starts the disk watcher (now, then every 10 minutes) and the
// pruner (now, then every 24 hours). Only the first call does anything.
func StartWatchers(dataDir string) {
	startOnce.Do(func() {
		go watchDisk(dataDir)
		go prune()
	})
}

func watchDisk(dataDir string) {
	w := newDiskWatcher(diskThreshold)
	for {
		// An unreadable disk is skipped, not reported as 0 bytes free
		if free, err := helper.GetFreeSpace(dataDir); err == nil {
			w.check(free)
		}
		time.Sleep(diskInterval)
	}
}

func prune() {
	for {
		pruneOnce()
		time.Sleep(pruneInterval)
	}
}

// pruneOnce deletes rows older than the retention. A failure prints one
// stdout line; the loop carries on.
func pruneOnce() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("activity: cannot prune: %v\n", r)
		}
	}()
	if database.SupportsGroups() {
		database.PruneActivity(time.Now().Add(-retention).Unix())
	}
}

// FeedFile is the file of a feed event
type FeedFile struct {
	Id   string `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
	Kind string `json:"kind"`
}

// FeedGroup is the group of a feed event
type FeedGroup struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

// FeedEvent is one event as the feed contract shows it
type FeedEvent struct {
	Id     int64      `json:"id"`
	At     int64      `json:"at"`
	Kind   string     `json:"kind"`
	Actor  int        `json:"actor"`
	Owner  int        `json:"owner"`
	File   *FeedFile  `json:"file"`
	Group  *FeedGroup `json:"group"`
	Via    string     `json:"via"`
	Detail string     `json:"detail"`
}

// FeedUser is one person, sent with every page
type FeedUser struct {
	Id             int    `json:"id"`
	Email          string `json:"email"`
	Name           string `json:"name"`
	Admin          bool   `json:"admin"`
	EmailDownloads bool   `json:"emailDownloads"`
}

// FeedResult is the body of GET /api/activity
type FeedResult struct {
	Result string      `json:"Result"`
	Events []FeedEvent `json:"events"`
	Next   int64       `json:"next"`
	More   bool        `json:"more"`
	Reset  bool        `json:"reset"`
	Users  []FeedUser  `json:"users"`
}

// readActivitySince reads the feed's rows; a var for the tests
var readActivitySince = database.GetActivitySince

// Feed returns the events after the cursor. A nil since means "start from
// now": no events, and next is the newest id. since 0 is "from the oldest
// retained row". A cursor whose next row was pruned, or one above the newest
// id (a restored database), answers reset with no events. limit is clamped
// to 1..MaxLimit.
//
// The users are listed last, after the rows are read (M3): a person whose
// first_signin is on this page signed in before the read, so they are in
// the list and are never announced as "someone (#N)".
func Feed(since *int64, limit int) (result FeedResult) {
	limit = max(1, min(limit, MaxLimit))
	result = FeedResult{Result: "OK", Events: make([]FeedEvent, 0)}
	defer func() { result.Users = feedUsers() }()
	if !database.SupportsGroups() {
		return result
	}
	// Bounds first: a row written after this read is simply on the next page
	oldest, newest := database.ActivityBounds()
	result.Next = newest
	if since == nil {
		return result
	}
	if oldest == 0 {
		oldest = newest + 1 // empty table: everything up to newest was pruned
	}
	if *since > newest || (*since != 0 && *since < oldest-1) {
		result.Reset = true
		return result
	}
	rows := readActivitySince(*since, limit+1)
	if len(rows) > limit {
		rows = rows[:limit]
		result.More = true
	}
	for _, a := range rows {
		result.Events = append(result.Events, toFeedEvent(a))
	}
	if len(rows) > 0 {
		result.Next = rows[len(rows)-1].Id
	}
	return result
}

func toFeedEvent(a models.Activity) FeedEvent {
	e := FeedEvent{Id: a.Id, At: a.At, Kind: a.Kind, Actor: a.ActorId, Owner: a.OwnerId, Via: a.Via, Detail: a.Detail}
	if a.FileId != "" {
		e.File = &FeedFile{Id: a.FileId, Name: a.FileName, Size: a.FileSize, Kind: a.FileKind}
	}
	if a.GroupId != "" {
		e.Group = &FeedGroup{Id: a.GroupId, Name: a.GroupName}
	}
	return e
}

func feedUsers() []FeedUser {
	result := make([]FeedUser, 0)
	for _, u := range database.GetAllUsers() {
		result = append(result, FeedUser{Id: u.Id, Email: u.Name, Name: people.Name(u), Admin: u.IsAdmin(),
			EmailDownloads: people.WantsDownloadEmails(u.Id)})
	}
	return result
}
