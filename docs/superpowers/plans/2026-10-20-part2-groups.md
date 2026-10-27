# Part 2 — Groups: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Signed-in people share a trip or an event in one place. A group has an invite link and a short code. Members upload into it, see everyone's photos on one board in shooting order, filter it, tick off what they have downloaded, and skip exact duplicates before they are uploaded. Existing files and whole collections can join a group.

**Architecture:**
- **Data:** five spec tables, plus one small `GroupNearKept` table (decision D11). They are created idempotently at startup (`CREATE TABLE IF NOT EXISTS`, as `Previews` is), so the schema stays at version 17 and rollback stays an image swap.
- **Rules:** one domain file, `internal/storage/Groups.go`, holds every group rule: roles, codes, expiry propagation, adding files, duplicates and cleanup. Handlers stay thin.
- **Access:** `access.CanView` gains the group-member grant.
- **Downloads:** every download route tags its request with a `downloadlog.Source`. Both `LogDownload` sites in storage then write one `Downloads` row.
- **File lists:** group pages use the shared file list, with a new `group` context. One task (G3) owns every change to the shared component. Every later task only registers handlers from its own JS file.

**Tech Stack:** Go (tested only through `build/dev/gotest.sh` in Docker on Colima), SQLite, Go html/template, vanilla JS, Bootstrap 5, DataTables, Dropzone. The Playwright MCP is used for the browser walkthrough.

**Spec:** `docs/design/2026-10-19-group-sharing-design.md`. Read "Part 2 — Groups" (lines 329–464), "Navigation" (lines 58–74), and the Part 1 sections "Access", "One file list everywhere", "Board layout" and "Download all, and zip".

---

## Decisions this plan takes where the spec is silent or contradicts itself

Franco answered D1–D5 on 2026-10-20, and D4 and D9 were changed on his say-so. Everything else is the default he accepted.

| # | Gap | Default in this plan | Task |
|---|---|---|---|
| D1 | **Join code.** The spec stored a `join-code hash`, but the header shows the code. | **Revised 2026-10-20: store the code itself** (`Groups.JoinCode`, unique), as Discord invites and Google Classroom class codes do. The invite token is stored in plain text beside it and grants the same access, so hashing the code protected nothing. Guessing is stopped by the rate limiter and the code space (30^6 ≈ 729 million); a leaked code or link is undone with Reset code. The spec table is amended to match. | G1, G2 |
| D2 | **Who can sign in** | Checked in the live `config.json` on 2026-10-20 (read-only): Google sign-in, `OnlyRegisteredUsers = false`. Any Google account can join. No change. | – |
| D3 | **Encryption and fingerprints** | Live `Encryption.Level = 0` (checked 2026-10-20), so stored SHA1s are plain and the browser pre-check works. It stays an optimisation that reports `checkable:false` if encryption is ever turned on. The server check after upload remains the authoritative one. Franco is fine with the server alone flagging duplicates, so the pre-check must never block an upload. | G7 |
| D4 | **Remove vs. delete, and editing in groups** | **Remove from group:** the creator and the admin on any item; a member on their own items. **Delete:** only the file's owner, or someone with PERM_DELETE, as everywhere else. **Edit stays on group pages (revised 2026-10-20)**: you may rename a file or replace its content. Its expiry, download limit and password are locked while it is in a live group, on every page: the edit dialog hides them and shows "Expiry and download limit follow the group “Iceland trip”." `/api/files/modify` refuses those changes on the server too, so "Change expiry & limits" on the Files tab reports them as failures. | G2, G3, G10 |
| D5 | **Names.** `User.Name` is the lowercased Google email, so tiles and "uploaded by Anna" would show full email addresses to every member. | Until Part 4's display name, members see `displayName(u)`: the part before `@`. The admin's own pages (Users, Files) keep full names. Franco: fine for now; real display names (Part 4) are a priority improvement. | G2 |
| D6 | Turning a collection into a group | The collection and its link stay untouched. Its password does not carry over: group members sign in instead. | G8 |
| D7 | Deleting a group | Only the group goes. Its files stay with their uploaders and keep their current expiry. | G2 |
| D8 | Leaving | Members can **Leave group**. The creator cannot: they delete the group instead. Files they put in stay in the group. | G2, G10 |
| D9 | **Avatars** | Initials in a coloured circle, with the colour derived from the user id. Google's sign-in also offers a profile photo (`picture`), but Gokapi does not store it today; showing it would mean saving its URL at sign-in, with each viewer's browser loading it from Google. **Franco, 2026-10-20: real photos come in Part 4**, with display names, which come from the same sign-in. | G10 |
| D10 | "Upload to" | It is never remembered and resets to *My files* on every visit, so private files never go into a group by accident. Choosing a group disables expiry, download limit, password and "Combine…", because the group sets them. End-to-end encryption and groups exclude each other. | G7 |
| D11 | Near-copies "Keep both" | Persisted in a new table `GroupNearKept(GroupId, FileId)`, because the spec has no table for it. Flags compare only against **other uploaders'** items, so your own burst shots are never flagged. Two items match when their dHash Hamming distance is ≤ `nearCopyMaxDistance = 6`, tuned on the preview with real photos. | G1, G10 |
| D12 | "New to me" and "Hide what I've downloaded" | Your own uploads count as already had. | G3 |
| D13 | Board order | Date taken ascending (shooting order), falling back to the upload date. Live-added items are inserted at their sorted position. | G3, G10 |
| D14 | **Batches of ~100 and server-side filters** (spec "Board layout" 3–4) | **Not built in Part 1 or Part 2.** Every list renders all its items server-side. Filters and "Select all (N)" act on everything rendered, which is the full set. That holds to about 2,000 items in one list, after which the DOM and DataTables get slow. **Franco, 2026-10-20: it must be built before the revamp is finished.** It is scheduled in Part 3; see "Deferred work" below and the spec's build order. | G3 now; Part 3 |
| D15 | Redis | `SupportsGroups()` is false on Redis. Every group action returns `ErrGroupsUnsupported`, which pages show as "Groups need the SQLite database". `SaveDownload` and `SaveCollectionSeen` are no-ops there, so downloads never break. Nothing fails silently. | G1, G2 |
| D16 | "Select all (N)" | It goes on **every** file list, not just groups ("each page differs only in what it lists and which actions it allows"). "Select everything new to me" is offered on group pages only. | G3 |
| D17 | Who is "the admin" in group rules | `user.IsAdmin()` (admin or super admin). | G2 |
| D18 | Extending | The creator picks 30 / 60 / 90 days **from today**, or Never. The new expiry must be later than the current one. Expired and pending-deletion files are skipped, so the 9 s undo window still holds. | G2 |
| D19 | `/g/join?t=` | Every redirect and asset link in Gokapi is relative, so a page served under `/g/` would load `/g/assets/…`. `/g/join` therefore only redirects to the top-level `/join?t=` page. The public link stays exactly as the spec writes it. | G6 |
| D20 | Join requests | `GET /join` shows a confirmation page, and only `POST /groups/join` joins. A Lax session cookie rides on cross-site GETs, so a GET must never change anything. Every group mutation endpoint refuses methods other than POST. | G6 |

---

## Deferred work that must ship before the revamp ends

- **List batching and server-side filters** (spec "Board layout", items 3 and 4). Scheduled for **Part 3**:
  - the item list is fetched in batches of about 100 as the reader nears the bottom;
  - the person filter, "Hide what I've downloaded", "Select all (N)" and "Select everything new to me" are answered by the server over the full set;
  - zip and download act on ids the page has not rendered yet.

  Recorded in the spec's build order and in `~/atlas/memory/gokapi-share-redesign.md`, so it cannot be lost between parts.
- **Display names** (Part 4). Until then members see the part of the email before `@` (D5).

## Global Constraints

Everything in `docs/superpowers/plans/2026-10-19-part1-revisions.md` → Global Constraints still applies. Verbatim where it matters:

- **No Go toolchain on the host.** Every `go` command runs through `build/dev/gotest.sh`. Set `DOCKER_HOST=unix://$HOME/.colima/default/docker.sock`; the script defaults to it.
  - The script serialises runs across worktrees with a lock.
  - `internal/encryption` and `internal/configuration/setup` are killed by the VM's ~4 GiB of memory. That is known: ignore those two packages.
  - If `internal/configuration/setup/test/` is left behind, delete it.
- **Always `-tags=test,awsmock`** (the script does this). Check for `--- PASS: <TestName>`.
- **Protected routes** are registered literally in `internal/webserver/Webserver.go` as `mux.HandleFunc("<path>", requireLogin(...))`. `build/go-generate/updateProtectedUrls.go` reads that form.
  - Each task adds **one contiguous block** of routes with a comment naming the task (`// Part 2, G6: groups tab`), directly after the `/p/info` line. The controller's merges then stay trivial.
- **Every group mutation endpoint answers 405 to anything but POST** (D20).
- **Originals are never altered.** No download route ever serves a preview.
- **Viewing never counts as downloading.** `/p/*` (including `/p/video`), `/files/previewinfo`, `/p/info` and `/groups/item` never write a `Downloads` row.
- **Logging:** new code logs to stdout (`fmt.Println`) only. Never use `logging.LogWarn` or any new log category; the notifier `~/apps/gokapi/gokapi-notify.py` emails every `[warning]` and every unknown category immediately. At most one `[info]` line per startup.
- **Schema stays at version 17.** New tables use `CREATE TABLE IF NOT EXISTS` in `ensureGroupTables()`, chained after `ensurePreviewsTable()`. There are no `Upgrade` steps.
- **Quote `"Groups"` in every SQL statement.** `GROUPS` is an SQLite keyword (window frames).
- **New code goes in new files** where possible, and edits to upstream files stay small.
- **Security:** build any DOM that contains a file name, group name or person name with `textContent` and attribute setters only, never `innerHTML`.
- **Names shown to members** come from `displayName(u)` (D5), never `u.Name`.
- **Commits** end with `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`. Push after each commit.
  - The branch is `feature/groups`, cut from `feature/collections` at `b777ae8`. Parallel tasks use branches `g/<task>` in worktrees under `~/src/Gokapi-wt/<task>`. Colima mounts only paths under `~`, never `/tmp`.
- **Never touch** the live `gokapi` container, its volumes (`gokapi_gokapi_data`, `gokapi_gokapi_config`) or anything under `~/apps/gokapi`. The controller does the preview and the deploy; implementers never do.
- No background processes. Implementers do not dispatch subagents.
- **Parallel tasks commit source only.** Before committing, restore the regenerated assets:

  ```bash
  git checkout -- internal/webserver/web/static/js/min internal/webserver/web/static/css/min \
    internal/webserver/web/templates/string_constants.tmpl build/go-generate/minifyStaticContent.go \
    internal/webserver/api/VersionNumbers.go 2>/dev/null || true
  ```

  - **Exception:** G7 must commit the regenerated `internal/webserver/api/routingParsing.go`, which is the generated parser for its new header.
  - The controller merges, then regenerates the minified assets once.
- **Shared component ownership.** Only G3 changes these files:
  - `FileBrowser.go`
  - `html_file_browser.tmpl`
  - the `FGS.initFileBrowser` part of `media.js`
  - `admin_ui_filebrowser.js`
  - `media.css`

  Any other task that needs a change there reports NEEDS_CONTEXT instead of making it.
- **Exact copy strings:**
  - Navigation and the Groups tab:
    - "Groups"
    - "Create group"
    - "Join with a code"
    - "Join"
    - "not a member"
  - Expiry choices:
    - "30 days"
    - "60 days (suggested)"
    - "90 days"
    - "Never"
  - Closing dates:
    - "closes 12 Jan" (day and short month; the year is added only if it is not the current year)
    - "never closes"
  - Uploading:
    - "Upload to"
    - "My files"
    - "Already here, uploaded by Anna"
  - Group page filters and selection:
    - "Everyone"
    - "Hide what I've downloaded"
    - "Select all (N)"
    - "Select everything new to me"
  - Near-copies:
    - "Looks like Anna's photo from 14:32"
    - "Keep both"
    - "Remove mine"
  - Actions:
    - "Turn into a group"
    - "Add to group…"
    - "Remove from group"
    - "Leave group"
    - "Delete group"
    - "Reset code"
    - "Rename"
    - "Extend"
  - Lists:
    - "Shared with me"

---

## Waves, worktrees, dependencies

| Wave | Tasks | Runs | Depends on |
|---|---|---|---|
| 1 | G1 Data layer → G2 Group rules + `CanView` | sequential, on `feature/groups` | – |
| 2 | G3 Shared component · G4 Downloads recording · G5 Shared with me | parallel worktrees | G2 |
| 3 | G6 Groups tab, create, join · G7 Uploading into a group · G8 Add to group, Turn into a group · G9 Users page | parallel worktrees | G3, G4 |
| 4 | G10 Group page | on `feature/groups` after the wave 3 merge | all |
| 5 | G11 End-to-end script and browser walkthrough, then the whole-branch review, then the preview | controller | all |

---

## File map

| File | Task | Responsibility |
|---|---|---|
| `internal/models/Group.go` (new) | G1 | `Group`, `GroupMember`, role constants |
| `internal/models/Download.go` (new) | G1 | `Download`, `via` constants; `CollectionSeen` |
| `internal/configuration/database/provider/sqlite/groups.go` (new) | G1 | tables and CRUD for all six tables |
| `…/provider/redis/groups.go` (new) | G1 | D15 stubs |
| `…/dbabstraction/DbAbstraction.go`, `…/database/Database.go` | G1 | interface and wrappers |
| `internal/storage/Groups.go` (new) | G2 | every group rule |
| `internal/storage/FileServing.go` (small edit in `CleanUp`) | G2 | drop group rows and downloads with the file; expire groups |
| `internal/webserver/access/Access.go` (small edit) | G2 | group-member grant |
| `internal/webserver/GroupsCommon.go` (new) | G2 | `displayName`, `/groups/list`, JSON helpers, `postOnly` |
| `FileBrowser.go`, `html_file_browser.tmpl`, `media.js`, `admin_ui_filebrowser.js`, `media.css` | G3 | `group` context and the component features |
| `internal/storage/downloadlog/DownloadLog.go` (new) | G4 | request-scoped download source |
| `internal/webserver/DownloadSource.go` (new) and small edits in download handlers | G4 | tag every download route |
| `Collections.go` (small edits), `html_collections_admin.tmpl` | G5, G8 | Shared with me; Turn into a group |
| `internal/webserver/GroupsTab.go` (new), `html_groups.tmpl` (new), `html_join.tmpl` (new), `admin_ui_groups.js` (new) | G6 | Groups tab, create, join, admin endpoints |
| `internal/webserver/authentication/ReturnTo.go` (new), small edits in `Authentication.go` and `Webserver.go` | G6 | return to `/join` after sign-in |
| `internal/webserver/ratelimiter/GroupJoin.go` (new) | G6 | join limiter |
| `static/js/sha1.js` and `sha1-worker.js` (new), `admin_ui_upload.js`, `admin_api.js`, `html_admin.tmpl`, `html_group_upload.tmpl` (new), `api/routing.go`, `api/Api.go`, `GroupsUpload.go` (new) | G7 | uploading into a group |
| `internal/webserver/GroupsAdd.go` (new), `admin_ui_groupactions.js` (new) | G8 | Add to group…, Turn into a group |
| `Webserver.go` (`ViewUsers`), `html_users.tmpl`, `admin_ui_users.js`, `Files.go` | G9 | Users page and per-person files |
| `internal/webserver/GroupPage.go` (new), `html_group.tmpl` (new), `admin_ui_group.js` (new), `internal/storage/NearCopies.go` (new) | G10 | group page, `/gd`, `/gz`, `/groups/item`, near-copies |
| `build/e2e/groups_e2e.py` (new) | G11 | two-user end-to-end flow |

---

## Spec test list → task

| Spec "Testing" item | Task |
|---|---|
| Membership and role rules | G2 (domain), G6 (HTTP) |
| Code reset | G2, G6 |
| Rate limiting on join | G6 |
| Expiry propagation when a group is extended | G2 |
| Adding an existing file: never shortens expiry, lifts download limit | G2, G8 |
| Duplicate lookup | G2, G7 |
| `CanView` grants group members, and only them | G2 |
| Playwright: create, join by link and by code as a second user | G11 |
| Playwright: upload a duplicate | G11 |
| Playwright: downloaded ticks | G11 |
| Playwright: select all and zip | G11 |
| Playwright: empty state | G11 |

---

### Task G1: Data layer

**Files:**
- Create: `internal/models/Group.go`, `internal/models/Download.go`
- Create: `internal/configuration/database/provider/sqlite/groups.go`, `…/sqlite/groups_test.go`
- Create: `internal/configuration/database/provider/redis/groups.go`, `…/redis/groups_test.go`
- Modify: `…/sqlite/Sqlite.go`. In `init`, chain `ensureGroupTables()` after `ensurePreviewsTable()`.
- Modify: `…/sqlite/collections.go` `DeleteCollection`. Also delete that collection's `CollectionsSeen` rows.
- Modify: `…/dbabstraction/DbAbstraction.go`, `…/database/Database.go`. Add the interface methods and their wrappers.

**Interfaces (binding for every later task):**

```go
// internal/models/Group.go
package models

// Group is a shared space signed-in people upload into. It is unlisted: you
// learn of it only from its invite link or code (the admin sees all).
type Group struct {
	Id           string `json:"id" redis:"id"`
	Name         string `json:"name" redis:"name"`
	CreatorId    int    `json:"creatorId" redis:"creatorid"`
	Created      int64  `json:"created" redis:"created"`
	Expires      int64  `json:"expires" redis:"expires"` // 0 = never
	JoinCode     string `json:"-" redis:"joincode"`      // "K7P-4QX", unique; shown to members, reset with the token
	InviteToken  string `json:"-" redis:"invitetoken"`   // the t= of /g/join; the code derives from it
}

// NeverExpires is true for a group created or extended with "Never"
func (g Group) NeverExpires() bool { return g.Expires == 0 }

// IsExpired reports whether the group has closed at now
func (g Group) IsExpired(now int64) bool { return g.Expires != 0 && g.Expires <= now }

const (
	GroupRoleCreator = "creator"
	GroupRoleMember  = "member"
)

// GroupMember is one person in a group
type GroupMember struct {
	GroupId string `json:"groupId"`
	UserId  int    `json:"userId"`
	Joined  int64  `json:"joined"`
	Role    string `json:"role"`
}
```

```go
// internal/models/Download.go
package models

// Download records one original file served to someone, through any route.
// UserId 0 is an anonymous download, stored as NULL.
type Download struct {
	FileId string
	UserId int
	At     int64
	Via    string
}

const (
	DownloadViaGroup      = "group"
	DownloadViaCollection = "collection"
	DownloadViaFile       = "file"
	DownloadViaZip        = "zip"
)

// CollectionSeen records the first time a signed-in person opened a
// collection link, for their "Shared with me" list
type CollectionSeen struct {
	UserId       int
	CollectionId string
	FirstOpened  int64
}
```

Add these to `dbabstraction.Database`, and add same-named wrappers in `database/Database.go`:

```go
	// SupportsGroups is false on providers without group tables (Redis)
	SupportsGroups() bool
	GetGroup(id string) (models.Group, bool)
	GetGroupByToken(token string) (models.Group, bool)
	GetGroupByCode(code string) (models.Group, bool) // code as normalised by storage
	GetAllGroups() []models.Group // newest first
	SaveGroup(g models.Group)
	// DeleteGroup removes the group, its members, its file links and its kept
	// near-copies. Files are never touched.
	DeleteGroup(id string)
	GetGroupMembers(groupId string) []models.GroupMember // by Joined ascending
	GetMembershipsOfUser(userId int) []models.GroupMember
	SaveGroupMember(m models.GroupMember)
	DeleteGroupMember(groupId string, userId int)
	GetGroupFileIds(groupId string) []string
	GetGroupIdsOfFile(fileId string) []string
	AddGroupFile(groupId, fileId string) // idempotent
	RemoveGroupFile(groupId, fileId string)
	RemoveFileFromGroups(fileId string)
	SaveNearCopyKept(groupId, fileId string) // idempotent
	GetNearCopiesKept(groupId string) map[string]bool
	SaveDownload(d models.Download)
	// GetDownloadedFileIds is every file id the user has downloaded, by any route
	GetDownloadedFileIds(userId int) map[string]bool
	DeleteDownloadsOfFile(fileId string)
	SaveCollectionSeen(s models.CollectionSeen) // keeps the first FirstOpened
	GetCollectionsSeen(userId int) []models.CollectionSeen // newest first
```

- [ ] **Step 1: Write the failing SQLite tests** in `…/sqlite/groups_test.go` (`//go:build test`). Follow `previews_test.go`: each test opens its own instance (`instance, err := New(config)`; `defer instance.Close()`).

```go
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
		instance.Close()
	}
}
```

  Also add a case to `TestPreviewsTableCreatedOnStartWithoutSchemaBump`'s pattern: a database created by `createNewDatabase` and then reopened still has `Groups`. The loop above covers that.

- [ ] **Step 2: Run the tests and confirm they fail.**
  `build/dev/gotest.sh ./internal/configuration/database/provider/sqlite/ -run 'TestGroups|TestDownloads|TestGroupTables' -v` → FAIL (undefined methods).

- [ ] **Step 3: Implement `sqlite/groups.go`.**

```go
// ensureGroupTables creates the fork's group, download and "shared with me"
// tables if missing. Runs on every start, like ensurePreviewsTable, so the
// schema version stays at upstream's 17 and rollback stays an image swap.
// "Groups" is always quoted: GROUPS is an SQLite keyword.
func (p DatabaseProvider) ensureGroupTables() error {
	return p.rawSqlite(`CREATE TABLE IF NOT EXISTS "Groups" (
			"Id"	TEXT NOT NULL UNIQUE,
			"Name"	TEXT NOT NULL,
			"CreatorId"	INTEGER NOT NULL,
			"Created"	INTEGER NOT NULL,
			"Expires"	INTEGER NOT NULL,
			"JoinCode"	TEXT NOT NULL UNIQUE,
			"InviteToken"	TEXT NOT NULL UNIQUE,
			PRIMARY KEY("Id")
		) WITHOUT ROWID;
		CREATE TABLE IF NOT EXISTS "GroupMembers" (
			"GroupId"	TEXT NOT NULL,
			"UserId"	INTEGER NOT NULL,
			"Joined"	INTEGER NOT NULL,
			"Role"	TEXT NOT NULL,
			PRIMARY KEY("GroupId","UserId")
		) WITHOUT ROWID;
		CREATE INDEX IF NOT EXISTS "GroupMembersByUser" ON "GroupMembers" ("UserId");
		CREATE TABLE IF NOT EXISTS "GroupFiles" (
			"GroupId"	TEXT NOT NULL,
			"FileId"	TEXT NOT NULL,
			PRIMARY KEY("GroupId","FileId")
		) WITHOUT ROWID;
		CREATE INDEX IF NOT EXISTS "GroupFilesByFile" ON "GroupFiles" ("FileId");
		CREATE TABLE IF NOT EXISTS "GroupNearKept" (
			"GroupId"	TEXT NOT NULL,
			"FileId"	TEXT NOT NULL,
			PRIMARY KEY("GroupId","FileId")
		) WITHOUT ROWID;
		CREATE TABLE IF NOT EXISTS "Downloads" (
			"Id"	INTEGER PRIMARY KEY AUTOINCREMENT,
			"FileId"	TEXT NOT NULL,
			"UserId"	INTEGER,
			"At"	INTEGER NOT NULL,
			"Via"	TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS "DownloadsByUser" ON "Downloads" ("UserId","FileId");
		CREATE INDEX IF NOT EXISTS "DownloadsByFile" ON "Downloads" ("FileId");
		CREATE TABLE IF NOT EXISTS "CollectionsSeen" (
			"UserId"	INTEGER NOT NULL,
			"CollectionId"	TEXT NOT NULL,
			"FirstOpened"	INTEGER NOT NULL,
			PRIMARY KEY("UserId","CollectionId")
		) WITHOUT ROWID;`)
}
```

  Implementation notes:
  - Follow `collections.go`:
    - explicit column lists (`const groupColumns = "Id, Name, CreatorId, Created, Expires, JoinCode, InviteToken"`);
    - a `scanGroup(scan func(dest ...any) error)` helper;
    - `helper.Check(err)` on unexpected errors;
    - `sql.ErrNoRows` → `false`;
    - an empty id or token → `false` without querying.
  - `SaveGroup` is `INSERT OR REPLACE`.
  - `AddGroupFile` and `SaveNearCopyKept` are `INSERT OR IGNORE`.
  - `SaveCollectionSeen` is `INSERT OR IGNORE`, which keeps the first `FirstOpened`.
  - `DeleteGroup` runs `DELETE FROM "GroupMembers" / "GroupFiles" / "GroupNearKept" WHERE GroupId = ?`, then deletes the group row.
  - `SaveDownload` stores `UserId` 0 as `NULL` (`sql.NullInt64{Int64: int64(d.UserId), Valid: d.UserId != 0}`).
  - `GetDownloadedFileIds(0)` returns an empty map without querying.
  - `GetGroupMembers` orders by `Joined ASC, UserId ASC`. `GetAllGroups` and `GetCollectionsSeen` order newest first.
  - Close every `rows` cursor before issuing a second query; SQLite in this setup does not allow nested queries (see the `GetAllCollections` comment).
  - `SupportsGroups()` returns `true`.
  - In `Sqlite.go` `init`, replace `return p, p.ensurePreviewsTable()` with:

```go
		if err = p.ensurePreviewsTable(); err != nil {
			return p, err
		}
		return p, p.ensureGroupTables()
```

  - In `collections.go` `DeleteCollection`, add `DELETE FROM CollectionsSeen WHERE CollectionId = ?`.

- [ ] **Step 4: Redis stubs (D15)** in `redis/groups.go`:
  - `SupportsGroups()` returns `false`.
  - Every `Get…` returns its zero value, `false` or an empty slice or map.
  - `SaveDownload`, `SaveCollectionSeen` and `DeleteDownloadsOfFile` do nothing.
  - Every other write panics with `"groups need the SQLite database"`. They are unreachable, because `storage` checks `SupportsGroups()` first.
  - Put one comment at the top explaining D15.
  - `redis/groups_test.go` asserts `SupportsGroups() == false` and that `SaveDownload` does not panic.

- [ ] **Step 5: Run the tests and confirm they pass.**
  `build/dev/gotest.sh ./internal/configuration/database/... -v 2>&1 | grep -E '^(--- |ok|FAIL)'`. Every listed test passes; `--- FAIL` must not appear.

- [ ] **Step 6: Commit and push.**
  `db: group, download and shared-with-me tables, created idempotently`

---

### Task G2: Group rules, `CanView` for members, `/groups/list`

**Files:**
- Create: `internal/storage/Groups.go`, `internal/storage/Groups_test.go`
- Modify: `internal/storage/FileServing.go` `CleanUp`. Two lines in the per-file block, and one call after `cleanUpCollections(timeNow)`.
- Modify: `internal/webserver/access/Access.go`. Add the group grant.
- Modify: `internal/webserver/api/Api.go` `apiEditFile`. Add the group lock (D4), a few lines calling `storage.GroupLock`.
- Test: the `api` package. Add `TestEditFileInGroupLocked` to `Api_test.go` or a new `//go:build test` file.
- Modify: `internal/webserver/access_test.go`. Add the group cases.
- Create: `internal/webserver/GroupsCommon.go`, `internal/webserver/GroupsCommon_test.go`
- Modify: `internal/webserver/Webserver.go`. Add one route block.

**Interfaces:**
- Consumes: G1.
- Produces (binding):

```go
// internal/storage/Groups.go
var (
	ErrGroupNotFound      = errors.New("group not found")
	ErrGroupForbidden     = errors.New("you may not do that in this group")
	ErrGroupsUnsupported  = errors.New("groups need the SQLite database")
	ErrGroupExpiryChoice  = errors.New("pick 30, 60 or 90 days, or never")
	ErrGroupNotLater      = errors.New("the new closing date must be later than the current one")
	ErrGroupName          = errors.New("a group needs a name")
	ErrCreatorCannotLeave = errors.New("the creator cannot leave; delete the group instead")
	ErrCannotRemoveCreator = errors.New("the creator cannot be removed")
	ErrInvalidJoin        = errors.New("no group with that code or link")
)

// GroupExpiryDays are the choices on every create, extend and turn-into form; 0 = never
var GroupExpiryDays = []int{30, 60, 90, 0}
const GroupDefaultExpiryDays = 60

func ExpiryFromChoice(days int, now int64) (int64, error)
func GroupCode(g models.Group) string           // "K7P-4QX", the stored JoinCode
func NormaliseGroupCode(input string) string    // "k7p 4qx" -> "K7P-4QX"; "" if not 6 alphabet chars
func GroupInviteUrl(g models.Group) string       // configuration.Get().ServerUrl + "g/join?t=" + token

func CreateGroup(name string, days int, creator models.User) (models.Group, error)
func GetGroup(id string) (models.Group, bool)    // false when unknown or expired
func GetAllGroups() []models.Group               // live only, newest first
func GroupsOfUser(userId int) []models.Group     // live groups the user is a member of
func GroupRole(g models.Group, userId int) (role string, member bool)
func IsGroupMember(g models.Group, userId int) bool
func CanSeeGroup(g models.Group, user models.User) bool   // member, or user.IsAdmin()
func CanManageGroup(g models.Group, user models.User) bool // creator, or user.IsAdmin()
func GroupFiles(g models.Group) []models.File    // live members: not expired, not pending deletion

func JoinGroupByToken(token string, user models.User) (models.Group, error) // idempotent
func JoinGroupByCode(code string, user models.User) (models.Group, error)   // idempotent
func LeaveGroup(g models.Group, user models.User) error
func RemoveGroupMember(g models.Group, memberId int, actor models.User) error
func RenameGroup(g models.Group, name string, actor models.User) (models.Group, error)
func ExtendGroup(g models.Group, days int, actor models.User) (models.Group, error)
func ResetGroupCode(g models.Group, actor models.User) (models.Group, error)
func DeleteGroup(g models.Group, actor models.User) error

type GroupDuplicate struct {
	FileId     string // the file that was not added
	ExistingId string // the copy already in the group
	UploaderId int    // who put the existing copy there
}
type GroupRefusal struct {
	FileId string
	Reason string // "not found", "not yours", "end-to-end encrypted", "file request upload"
}
type GroupAddResult struct {
	Added      []models.File
	Duplicates []GroupDuplicate
	Refused    []GroupRefusal
}
func AddFilesToGroup(g models.Group, fileIds []string, actor models.User) (GroupAddResult, error)
// AttachUploadToGroup is AddFilesToGroup for a file uploaded into the group
// just now. A byte-identical copy already in the group wins: the new file is
// deleted and its duplicate returned.
func AttachUploadToGroup(g models.Group, file models.File, uploader models.User) (GroupDuplicate, bool)
func FindInGroup(g models.Group, sha1 string) (models.File, bool)
func StoredSha1IsPlain() bool // false when stored SHA1s are salted (D3)
func RemoveFilesFromGroup(g models.Group, fileIds []string, actor models.User) (removed int, err error)
func GroupFromCollection(collectionId string, days int, actor models.User) (models.Group, GroupAddResult, error)
func UserSharesGroupWithFile(userId int, fileId string) bool // for access.CanView
// GroupLock reports whether a file is in a live group, and the name of the
// latest-closing one: its expiry, download limit and password then follow the
// group and cannot be edited (D4)
func GroupLock(fileId string) (groupName string, locked bool)
func cleanUpGroups(timeNow int64)
```

```go
// internal/webserver/GroupsCommon.go
func displayName(u models.User) string                  // D5: before "@"; the whole name if none
func memberNames(g models.Group) map[int]string        // user id -> displayName, for members and file owners
func writeJson(w http.ResponseWriter, v any)           // no-cache, application/json
func writeJsonError(w http.ResponseWriter, status int, err error) // {"Result":"error","ErrorMessage":…}
func postOnly(w http.ResponseWriter, r *http.Request) bool        // false (and a 405 written) unless POST
func readJsonBody(r *http.Request, v any) error        // 1 MiB limit
type groupListItem struct {
	Id string `json:"id"`; Name string `json:"name"`; Expires int64 `json:"expires"`
	NeverExpires bool `json:"neverExpires"`; Files int `json:"files"`; Members int `json:"members"`
	Role string `json:"role"` // "creator", "member", or "" (admin, not a member)
}
// GET /groups/list: the caller's groups; ?all=1 is every group, for the admin only
func listGroups(w http.ResponseWriter, r *http.Request)
```

**The lock in `apiEditFile` (D4).** When `storage.GroupLock(file.Id)` is locked, refuse the request with 400 and the message `Expiry and download limit follow the group "<name>"` if it would:
- set a download limit (anything but unlimited);
- set any expiry other than keeping the current one;
- set or remove a password.

Renaming (`filename`) and replacing content still work. Find where `apiEditFile` reads those headers, and use the same `foundHeaders` checks the rename change (R1) used.

`TestEditFileInGroupLocked` covers:
- for a file in a live group: renaming works; a download limit, an expiry or a password is refused with that message;
- after the group expires, the same changes work again.

`JoinGroupByCode` looks the code up as `database.GetGroupByCode(NormaliseGroupCode(code))`, and an empty normalised code is `ErrInvalidJoin` without querying. `CreateGroup` and `ResetGroupCode` both use `withFreshInvite`.

Rules every function enforces:
- **Admin:** `user.IsAdmin()` (D17).
- **Unsupported provider:** writes return `ErrGroupsUnsupported` when `!database.SupportsGroups()`.
- **Expired groups:** behave as not found everywhere.
- **Names:** `strings.TrimSpace`, at most 200 runes, never empty.

Key implementations (write them as shown; the rest follow the same pattern):

```go
const groupCodeAlphabet = "23456789ABCDEFGHJKMNPQRSTVWXYZ" // no 0/O, 1/I/L or U: nothing to misread

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

// withFreshInvite gives the group a new invite token and a new code that no
// other group uses. A clash is rare; five tries is plenty.
func withFreshInvite(g models.Group) models.Group {
	g.InviteToken = helper.GenerateRandomString(32)
	for i := 0; i < 5; i++ {
		g.JoinCode = newGroupCode()
		if other, taken := database.GetGroupByCode(g.JoinCode); !taken || other.Id == g.Id {
			return g
		}
	}
	panic("could not find a free group code")
}

func ExpiryFromChoice(days int, now int64) (int64, error) {
	switch days {
	case 0:
		return 0, nil
	case 30, 60, 90:
		return now + int64(days)*24*3600, nil
	}
	return 0, ErrGroupExpiryChoice
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
```

`AddFilesToGroup` works as follows:
1. Return `ErrGroupForbidden` unless `CanSeeGroup(g, actor)`.
2. Build `bySha := map[sha1]models.File` from `GroupFiles(g)`, skipping files whose SHA1 starts with `e2e-`.
3. For each distinct id in order:
   - **Refuse it** if it is missing, expired or pending deletion (`"not found"`).
   - **Refuse it** if `f.UserId != actor.Id && !actor.IsAdmin()` (`"not yours"`).
   - **Refuse it** if `f.Encryption.IsEndToEndEncrypted` (`"end-to-end encrypted"`).
   - **Refuse it** if `f.UploadRequestId != ""` (`"file request upload"`).
   - **Skip it silently** if it is already a member by id.
   - **Record a duplicate** if `bySha[f.SHA1]` exists.
   - **Otherwise**, call `database.AddGroupFile`, then `updateFileTerms(f.Id, g)`, set `bySha[f.SHA1]`, and append the re-read file to `Added`.

`AttachUploadToGroup` calls `FindInGroup` first:
- On a hit, call `DeleteFile(file.Id, true)`, print one stdout line `groups: duplicate upload <id> of <existing> in <group>`, and return the duplicate.
- Otherwise call `AddGroupFile` and `updateFileTerms`.

`ExtendGroup`:
1. Requires `CanManageGroup`.
2. `newExpiry, err := ExpiryFromChoice(days, now)`.
3. Return `ErrGroupNotLater` when the group already never expires, or when `newExpiry != 0 && newExpiry <= g.Expires`.
4. Save, then call `updateFileTerms` for every id in `database.GetGroupFileIds(g.Id)`.

`RemoveFilesFromGroup`:
- Requires `CanSeeGroup`.
- A file is removable when `CanManageGroup(g, actor)` or the file's `UserId == actor.Id`. Unremovable ids are skipped, not errors.
- Expiry is never shortened (spec).

`GroupFromCollection`:
- Uses `GetCollection(id)` (live), and requires `collection.UserId == actor.Id || actor.IsAdmin()`.
- Calls `CreateGroup(collection.Name, days, actor)`, then `AddFilesToGroup(g, ids of collection.Files, actor)`.
- The collection itself is not modified (D6).

`UserSharesGroupWithFile`:
- `userId != 0`.
- For each id in `GetGroupIdsOfFile(fileId)`: the group exists, is not expired, and the user is a member.

`cleanUpGroups`:
- Deletes every group whose `IsExpired(timeNow)`.
- Is a no-op when `!SupportsGroups()`.

In `CleanUp`'s per-file deletion block, next to `database.RemoveFileFromCollections(element.Id)`, add:

```go
			database.RemoveFileFromGroups(element.Id)
			database.DeleteDownloadsOfFile(element.Id)
```

After `cleanUpCollections(timeNow)`, add `cleanUpGroups(timeNow)`.

`access.CanView`: after the `UploadRequestId` check and before the collection branch, add:

```go
	// A member of a live group the file is in (spec, "Access")
	if user, loggedIn := authentication.SessionIdentity(r); loggedIn && storage.UserSharesGroupWithFile(user.Id, current.Id) {
		return true
	}
```

- [ ] **Step 1: Write the failing storage tests** in `internal/storage/Groups_test.go` (`//go:build test`):
  - Save the users and files they need with `database.SaveUser` and `database.SaveMetaData`.
  - Remove them in a `defer`, so later tests see the fixture unchanged.
  - Use ids that start with `grp`.

```go
func groupFile(id string, owner int, mutate func(*models.File)) models.File {
	f := models.File{Id: id, Name: id + ".jpg", SHA1: "sha-" + id, UserId: owner, SizeBytes: 1,
		ExpireAt: time.Now().Add(24 * time.Hour).Unix(), DownloadsRemaining: 1, ContentType: "image/jpeg"}
	if mutate != nil {
		mutate(&f)
	}
	database.SaveMetaData(f)
	return f
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

	joined, err := JoinGroupByCode(strings.ToLower(GroupCode(g)), member)
	test.IsNil(t, err)
	test.IsEqualString(t, joined.Id, g.Id)
	_, err = JoinGroupByToken(g.InviteToken, member) // idempotent
	test.IsNil(t, err)
	test.IsEqualInt(t, len(database.GetGroupMembers(g.Id)), 2)
	_, err = JoinGroupByCode("ZZZ-ZZZ", stranger)
	test.IsEqualBool(t, errors.Is(err, ErrInvalidJoin), true)

	test.IsEqualBool(t, CanManageGroup(g, creator), true)
	test.IsEqualBool(t, CanManageGroup(g, member), false)
	test.IsEqualBool(t, CanManageGroup(g, admin), true)
	test.IsEqualBool(t, CanSeeGroup(g, stranger), false)
	test.IsEqualBool(t, CanSeeGroup(g, admin), true)
	_, err = RenameGroup(g, "Other", member)
	test.IsEqualBool(t, errors.Is(err, ErrGroupForbidden), true)
	test.IsEqualBool(t, errors.Is(LeaveGroup(g, creator), ErrCreatorCannotLeave), true)
	test.IsEqualBool(t, errors.Is(RemoveGroupMember(g, 7, admin), ErrCannotRemoveCreator), true)

	// Reset: the old code and link stop working, the new ones work
	oldCode, oldToken := GroupCode(g), g.InviteToken
	g2, err := ResetGroupCode(g, creator)
	test.IsNil(t, err)
	test.IsNotEqualString(t, g2.InviteToken, oldToken)
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
	defer func() {
		for _, f := range []models.File{short, long, forever, twin, e2e, req, others} {
			database.DeleteMetaData(f.Id)
		}
	}()

	res, err := AddFilesToGroup(g, []string{short.Id, long.Id, forever.Id, e2e.Id, req.Id, others.Id, "nope"}, owner)
	test.IsNil(t, err)
	test.IsEqualInt(t, len(res.Added), 3)
	test.IsEqualInt(t, len(res.Refused), 4) // e2e, request, someone else's, unknown
	got, _ := database.GetMetaDataById(short.Id)
	test.IsEqualBool(t, got.UnlimitedDownloads, true)     // limit lifted
	test.IsEqualInt64(t, got.ExpireAt, g.Expires)         // extended to the group's
	got, _ = database.GetMetaDataById(long.Id)
	test.IsEqualInt64(t, got.ExpireAt, long.ExpireAt)     // never shortened
	got, _ = database.GetMetaDataById(forever.Id)
	test.IsEqualBool(t, got.UnlimitedTime, true)

	// Exact duplicate by fingerprint, from another member
	res, _ = AddFilesToGroup(g, []string{twin.Id}, other)
	test.IsEqualInt(t, len(res.Added), 0)
	test.IsEqualInt(t, len(res.Duplicates), 1)
	test.IsEqualString(t, res.Duplicates[0].ExistingId, short.Id)
	test.IsEqualInt(t, res.Duplicates[0].UploaderId, 7)
	found, ok := FindInGroup(g, "sha-grpshort")
	test.IsEqualBool(t, ok && found.Id == short.Id, true)

	// The admin may add anyone's file; strangers may add nothing
	res, _ = AddFilesToGroup(g, []string{others.Id}, admin)
	test.IsEqualInt(t, len(res.Added), 1)
	_, err = AddFilesToGroup(g, []string{short.Id}, models.User{Id: 9})
	test.IsEqualBool(t, errors.Is(err, ErrGroupForbidden), true)

	// Removing never shortens; members remove only their own
	n, _ := RemoveFilesFromGroup(g, []string{short.Id}, other)
	test.IsEqualInt(t, n, 0)
	n, _ = RemoveFilesFromGroup(g, []string{others.Id}, other)
	test.IsEqualInt(t, n, 1)
	got, _ = database.GetMetaDataById(others.Id)
	test.IsEqualInt64(t, got.ExpireAt, g.Expires)
}

func TestExtendPropagates(t *testing.T) {
	owner := models.User{Id: 7, UserLevel: models.UserLevelUser}
	g, _ := CreateGroup("Extend", 30, owner)
	defer database.DeleteGroup(g.Id)
	f := groupFile("grpextend", 7, nil)
	pending := groupFile("grpextpending", 7, func(f *models.File) { f.SHA1 = "sha-pending"; f.PendingDeletion = time.Now().Add(9 * time.Second).Unix() })
	defer database.DeleteMetaData(f.Id)
	defer database.DeleteMetaData(pending.Id)
	_, _ = AddFilesToGroup(g, []string{f.Id}, owner)
	database.AddGroupFile(g.Id, pending.Id)

	_, err := ExtendGroup(g, 30, owner) // same day count from today is not later... (≈ equal)
	test.IsEqualBool(t, errors.Is(err, ErrGroupNotLater), true)
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
	cleanUpGroups(time.Now().Unix())
	_, ok := database.GetGroup(g.Id)
	test.IsEqualBool(t, ok, false)
	test.IsEqualInt(t, len(database.GetGroupIdsOfFile("grpsomefile")), 0)
}
```

  Also write `TestGroupFromCollection`:
  - the owner turns a two-file collection into a group with 60 days;
  - the group has both files, and its name equals the collection's;
  - the collection still exists with the same `FileIds` and password hash;
  - a stranger gets `ErrGroupForbidden`.

  And `TestAttachUploadDuplicate`:
  - a second file with the same SHA1 attached to a group is deleted (`ExpireAt == 0` after `DeleteFile`) and reported;
  - a unique one is added with unlimited downloads.

  The comment `(≈ equal)` in `TestExtendPropagates` marks a real edge case. A 30-day group extended by 30 days from the same second is not later, so it must be refused. Keep the test.

- [ ] **Step 2: Add `access_test.go` cases** in a new `func TestCanViewGroupMembers(t *testing.T)`:
  - The file belongs to user 7 and is in a live group with members 7 and 10. Save user 10 and session `"accessgroupmember"`.
  - **Member 10:** true.
  - **Logged-in stranger 8:** false. The file must have a password hash, so the open-link fallback does not mask the result.
  - **Member 10 after the group expired:** false.
  - **Member 10 after `RemoveFileFromGroups`:** false.
  - **Anonymous:** false.

- [ ] **Step 3: Run the tests and confirm they fail.**
  `build/dev/gotest.sh ./internal/storage/ ./internal/webserver/ -run 'TestGroup|TestCreateJoin|TestAddFiles|TestExtend|TestAttach|TestCanViewGroup' -v` → FAIL.

- [ ] **Step 4: Implement** `Groups.go`, the `CleanUp` lines, the `CanView` grant, and `GroupsCommon.go`. Add this route block to `Webserver.go`:

```go
	// Part 2, G2: groups (shared)
	mux.HandleFunc("/groups/list", requireLogin(listGroups, false, false))
```

  `GroupsCommon_test.go` covers:
  - `displayName` for `"anna.mueller@gmail.com"` → `"anna.mueller"`, and `"Franco"` → `"Franco"`.
  - `/groups/list` returns only the caller's live groups, with roles.
  - `?all=1` is honoured for the admin and ignored for anyone else.
  - Unauthenticated: 401.

- [ ] **Step 5: Run the tests and confirm they pass.**
  `build/dev/gotest.sh ./internal/storage/... ./internal/webserver/... 2>&1 | grep -E '^(--- FAIL|ok|FAIL)'`: everything is `ok`, apart from the two known OOM packages.

- [ ] **Step 6: Commit and push.**
  `groups: rules, codes, expiry propagation, duplicates; CanView for members`

---

### Task G3: The shared file list learns groups (worktree `g/component`)

**Files:** `internal/webserver/FileBrowser.go`, `FileBrowser_test.go`, `html_file_browser.tmpl`, `media.js` (the `initFileBrowser` part), `admin_ui_filebrowser.js`, `media.css`.

**Interfaces:**
- Consumes: `models.Group` (G1), `displayName` (G2).
- Produces (binding for G6–G10):

```go
const ctxGroup = "group"

// manageMenu gains "add-to-group" after "add-to-collection"; manageBulk the same
manageMenu = []string{"open", "download", "copy", "hotlink", "share", "qr", "email", "edit", "replace", "add-to-collection", "add-to-group", "delete"}
manageBulk = []string{"download", "zip", "photos", "link", "add-to-collection", "add-to-group", "change-expiry", "delete"}

ctxGroup: {
	Menu:        []string{"open", "download", "edit", "replace", "remove-from-group", "delete"},
	Bulk:        []string{"download", "zip", "photos", "remove-from-group", "delete"},
	DefaultView: viewBoard,
},

// browserItem gains
	UploaderId int
	Downloaded bool // a Downloads row exists for the viewer (G4)
	CanRemove  bool // may take it out of this group (D4)
	Initials   string

// fileBrowser gains
	GroupId      string
	DownloadBase string          // live adds' download URL, up to the file id; "" = the owner download where Manages
	SortTakenAsc bool            // Board, Grid and live adds in shooting order; List sorted by Taken ascending
	GroupTools   bool            // person filter, "Hide what I've downloaded", "Select everything new to me", ticks
	People       []browserPerson // the person filter's choices
	UploadSink   bool            // finished uploads on this page are added here (G7)

type browserPerson struct{ Id int; Name string }

// WithGroup scopes the browser to a group page (G10 calls it)
func (b *fileBrowser) WithGroup(g models.Group, people []browserPerson)
// ItemPerms is true where tiles carry data-can-delete / data-can-remove
// without the full management data (Manages)
func (b fileBrowser) ItemPerms() bool
func initialsOf(name string) string // "anna.mueller" -> "AM", "ben" -> "B"
```

`browserItemFromFile(r, f, user, context, scopeId, uploaderNames)`:
- Rename the parameter `collectionId` to `scopeId`. It is the collection id for `collection-public` and the group id for `group`.
- For `ctxGroup`:
  - `DownloadUrl = "./gd?id=" + scopeId + "&file=" + f.Id`.
  - Previews are not scoped (`previewCollection = ""`), because `CanView` admits members.
  - `Mine`, `Uploader`, `CanEdit`, `CanDelete` and `CanReplace` follow the default branch.
  - `UploaderId = f.UserId`; `Initials = initialsOf(Uploader)`.
  - `CanRemove` is left to the caller (G10 knows the role).

`WithGroup` sets:
- `GroupId`;
- `ZipUrl = "./gz?id=" + g.Id + "&files="`;
- `DownloadBase = "./gd?id=" + g.Id + "&file="`;
- `SortTakenAsc`, `GroupTools` and `ShowUploader` to true;
- `People`.

`InfoUrl()` returns `"./p/info?id="` for `ctxGroup`.

`ItemPerms()` is `b.HasMenu("delete") || b.HasMenu("remove-from-group")`. `Manages()` stays `HasMenu("edit")`, so group pages are managed lists: the Expires and Downloads columns show, and the edit data is rendered.

**The group lock in the edit dialog (D4):**
- `browserItem` gains `GroupLock string`: the name from `storage.GroupLock`, filled in `browserItemFromFile` for every managing context, not only `group`.
- The menu renders `data-group-lock="<name>"` when it is set.
- `editFromMenu` in `admin_ui_filebrowser.js` passes it on. `showEditModal` in `admin_ui_upload.js` then:
  - hides the expiry, download-limit and password fields;
  - shows `Expiry and download limit follow the group “<name>”.` with `textContent`.

  That `showEditModal` edit is the one change G3 makes outside the shared files. Keep it minimal.
- "Change expiry & limits" needs no special case: the server refuses each locked file, and the dialog's existing failure list shows the message.

**Template:**
- **`file_browser`:** the root gets these attributes:
  - `data-group-id`
  - `data-download-base`
  - `data-sort="{{ if .SortTakenAsc }}taken-asc{{ else }}uploaded-desc{{ end }}"`
  - `data-group-tools="{{ .GroupTools }}"`
  - `data-upload-sink="{{ .UploadSink }}"`
- **Toolbar, only when `.GroupTools`:**
  - `<select class="form-select form-select-sm w-auto" data-fgs-person aria-label="Whose files">`, with an "Everyone" option (value `""`) and one option per person (value = id, text = name);
  - `<label><input type="checkbox" data-fgs-hide-downloaded> Hide what I've downloaded</label>`.
- **Selection bar, first after the count:**
  - **Every context:** `<button data-fgs-select-all>Select all (N)</button>`. JS keeps N equal to the number of visible tiles.
  - **Only with `.GroupTools`:** `<button data-fgs-select-new>Select everything new to me</button>`.
- **`file_browser_tile`:**
  - adds `data-uploader-id` and `data-downloaded="{{ $it.Downloaded }}"` on the figure;
  - adds `<span class="fgs-tick" aria-label="Downloaded" title="Downloaded"><i class="bi bi-check-circle-fill"></i></span>` and, when `.B.GroupTools`, `<span class="fgs-initials" aria-hidden="true">{{ $it.Initials }}</span>`;
  - CSS shows the tick only when `[data-downloaded=true]` on group browsers.
- **`file_browser_row`:** a tick icon next to the name in the same case. When `.SortTakenAsc`, the Taken `<th>` gets `data-fgs-sort data-fgs-sort-dir="asc"` instead of Uploaded.
- **`file_browser_menu`:**
  - Render `data-can-delete` and `data-can-remove` when `$b.ItemPerms`, independently of `$b.Manages`.
  - New entries: "Add to group…" (`data-menu="add-to-group"`, `bi-people`, disabled for e2e), next to Add to collection; and "Remove from group" (`data-menu="remove-from-group"`, `bi-dash-circle`, disabled unless `$it.CanRemove`).
- **`file_browser_bulk_button`:** `add-to-group` → "Add to group…"; `remove-from-group` → "Remove from group" (`btn-outline-danger`).
- **`file_browser_dialogs`:** add the Add to group dialog markup, which G8 wires up:
  - `#fgs-group-modal`, title "Add to group…";
  - `#fgs-group-what`;
  - `#fgs-group-list` (list-group);
  - `#fgs-group-result` (aria-live);
  - a footer link "Create a group on the Groups tab" → `./groups`.

**`media.js`, inside `initFileBrowser`:**
- **Filters:**
  - Generalise `hideForScope` into `applyVisibility(el)`. It hides when the scope says so, or any active filter in `filters` (key → predicate on the tile) rejects the tile. Rows follow their tile.
  - `api.setFilter(key, fnOrNull)` re-applies visibility, then `refreshCount`, `layoutBoard` and `updateSelection`.
  - The person select sets the filter `person`: `tile.getAttribute("data-uploader-id") === value`.
  - The checkbox sets `downloaded`: hide where `data-downloaded=true` or `data-mine=true` (D12).
  - Both choices are remembered per context like the view (`store("person")` is **not** remembered, because a group's people differ; `hideDownloaded` is remembered).
- **Select all (N):** selects every visible tile. **Select everything new to me:** selects visible tiles that are neither downloaded nor mine. Both enter select mode if it is off. `refreshCount` updates the N label.
- **`api.markDownloaded(ids)`:** sets `data-downloaded=true` on tile and row, then re-applies visibility. The built-in bulk download, zip, Save to Photos and the menu download call it when `root.getAttribute("data-group-tools") === "true"`. Registered handlers can call it too.
- **Sorted insertion in `addItem`:** when `data-sort=taken-asc`, insert the tile before the first tile whose `(data-taken || data-uploaded)` is greater. Otherwise prepend, as today. With DataTables, `row.add(...).draw(false)` already re-sorts.
- **DataTables order:** the `<th data-fgs-sort>` column, direction `data-fgs-sort-dir || "desc"`.
- **`addItem` fields:**
  - The download URL is `data-download-base + id` when that is set. That overrides the `manages` default; `preview.Download` still wins.
  - Uploader name: `file.UploaderName`, when given, wins over "mine → own name".
  - `file.UploaderId` sets `data-uploader-id`.
  - `data-downloaded` comes from `file.Downloaded`.
  - `data-can-remove` comes from `file.CanRemove` when ItemPerms (the template tells: `[data-menu=remove-from-group]` exists).
  - Initials are computed in JS with the same rule as `initialsOf`.
- **Export:** `setFilter`, `markDownloaded` and `selectWhere(fn)` on `api`.

**`admin_ui_filebrowser.js`:** `bulkDelete` / `deleteOne` already read `data-can-delete`. Make sure that works when only ItemPerms is rendered, and skip (with a count in the status) any tile whose `data-can-delete` is not `"true"`. Change nothing else.

**`media.css`:**
- `.fgs-tick`: hidden by default, top-left, white on a green circle. Show it under `[data-group-tools=true] .fgs-tile[data-downloaded=true]`.
- `.fgs-initials`: bottom-left badge on grid and board tiles.

- [ ] **Step 1: Update `TestFileBrowserActionTable`** with the new `manageMenu`/`manageBulk`, `ctxGroup`'s row and `test.IsEqualInt(t, len(want), 6)`.
  Then add `TestGroupBrowser`:
  - `newFileBrowser("g", ctxGroup, …).WithGroup(models.Group{Id: "G1"}, people)` gives `ZipUrl "./gz?id=G1&files="`, `DownloadBase "./gd?id=G1&file="`, `InfoUrl "./p/info?id="`, `DefaultView "board"`.
  - `Manages()` and `ItemPerms()` are both true.
  - An item for a file in a live group has `GroupLock` set to the group's name, and the rendered menu contains `data-group-lock`.
  - `browserItemFromFile(r, f, user7, ctxGroup, "G1", names)` gives `DownloadUrl "./gd?id=G1&file="+f.Id`, `CanEdit` false, `CanDelete` true for the owner, `UploaderId` 7.
  - `initialsOf("anna.mueller") == "AM"`, `initialsOf("ben") == "B"`, `initialsOf("") == ""`.

- [ ] **Step 2: Add a template render test** `TestGroupBrowserMarkup`. Execute the `file_browser` template into a buffer, with `templateFolder` as other tests use it:
  - A group browser with one downloaded item contains `data-fgs-select-new`, `data-fgs-person`, `Hide what I&#39;ve downloaded`, `data-downloaded="true"`, `data-can-remove`, `data-sort="taken-asc"`.
  - It contains `data-menu="edit"` and `data-menu="replace"`, and does not contain `data-menu="copy"` or `data-menu="share"`.
  - A `ctxFiles` browser contains `data-fgs-select-all` and `data-menu="add-to-group"`, and does not contain `data-fgs-select-new`.

- [ ] **Step 3: Run the tests and confirm they fail.** `build/dev/gotest.sh ./internal/webserver/ -run 'TestFileBrowser|TestGroupBrowser|TestBrowserItem' -v` → FAIL.

- [ ] **Step 4: Implement** the Go and template changes, then rerun until the tests PASS. Then run all of `./internal/webserver/...`; existing pages must stay green.

- [ ] **Step 5: Implement** the JS and CSS. Run `node --check internal/webserver/web/static/js/media.js internal/webserver/web/static/js/admin_ui_filebrowser.js`.

- [ ] **Step 6: Commit (source only) and push.**
  `file list: group context, filters, select all / new to me, ticks, shooting order`

---

### Task G4: Record every download (worktree `g/downloads`)

**Files:**
- Create: `internal/storage/downloadlog/DownloadLog.go`, `internal/storage/downloadlog/DownloadLog_test.go`
- Modify: `internal/storage/FileServing.go`. Call `recordDownload(file, r)` next to `logging.LogDownload` at both sites, in `ServeFile` and in the `ServeFilesAsZip` loop.
- Create: `internal/webserver/DownloadSource.go`, `internal/webserver/DownloadSource_test.go`
- Modify, one line each, in these handlers:
  - `downloadFile` and `downloadFileWithNameInUrl`, through `serveFile`;
  - `showHotlink`;
  - `downloadPresigned`;
  - `downloadOwnFile` (Files.go);
  - `downloadFilesZip` and `downloadCollectionZip` (Zip.go);
  - `downloadCollectionFile` (Collections.go);
  - `apiDownloadSingle` and `apiDownloadZip` (api/Api.go).

**Interfaces:**

```go
// internal/storage/downloadlog
package downloadlog

type Source struct {
	Via    string // models.DownloadVia*
	UserId int    // 0 = anonymous
}
type ctxKey struct{}

// With tags the request: storage records one Downloads row per original file
// it serves for this request. Untagged requests record nothing.
func With(r *http.Request, via string, userId int) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), ctxKey{}, Source{Via: via, UserId: userId}))
}
func From(r *http.Request) (Source, bool) {
	s, ok := r.Context().Value(ctxKey{}).(Source)
	return s, ok
}
```

```go
// internal/storage/FileServing.go (new unexported helper, in a new file storage/downloads.go)
func recordDownload(file models.File, r *http.Request) {
	src, ok := downloadlog.From(r)
	if !ok || !database.SupportsGroups() {
		return
	}
	database.SaveDownload(models.Download{FileId: file.Id, UserId: src.UserId, At: time.Now().Unix(), Via: src.Via})
}
```

```go
// internal/webserver/DownloadSource.go
// tagDownload marks r with the route and the signed-in person, if any:
// requireLogin routes have the user in context; public ones read the session
// without side effects. In Header or Disabled auth there is no session, so
// public downloads stay anonymous.
func tagDownload(r *http.Request, via string) *http.Request
// tagDownloadUser is the API form: the API key's owner
func tagDownloadUser(r *http.Request, via string, userId int) *http.Request
```

| Route | via |
|---|---|
| `/downloadFile`, `/dh/…`, `/h/…`, `/files/download`, `/downloadPresigned` (one file), `/api/files/download/{id}` | `file` |
| `/cd` | `collection` |
| `/files/zip`, `/cz`, `/downloadPresigned` (several files), `/api/files/downloadzip` | `zip` (one row per entry) |
| `/gd` (G10) | `group` |
| `/gz` (G10) | `zip` |

The ServeFile call sites pass the tagged request (`r = tagDownload(r, …)` before the call). `ServeFilesAsZip` receives the same `r`, so each entry is recorded with `zip`.

- [ ] **Step 1: Write the failing tests.**
  - `DownloadLog_test.go`: `With` → `From` round-trips the source; an untagged request gives `ok == false`.
  - `DownloadSource_test.go` (`//go:build !integration && test`) defines `TestEveryDownloadRouteRecords`:
    1. Use the test server on `localhost:53843` and an `accessFile` fixture with no password, owned by user 7.
    2. Before each request, clear rows with `database.DeleteDownloadsOfFile(id)`.
    3. Request the route. Then check `database.GetDownloadedFileIds(7)` for the session-holder cases, or count anonymous rows. For the count, add an unexported test helper in package `sqlite`, or assert through `GetDownloadedFileIds` with a session cookie.
    4. Cover:
       - `/downloadFile?id=` with the owner's session → recorded for user 7;
       - the same without a session → no row for 7, but a row (exercise it through a fresh session for user 8 and check user 8);
       - `/files/download?id=` (session 7);
       - `/files/zip?ids=a,b` (two rows);
       - `/cd` and `/cz` on a collection containing the file;
       - `/h/<hotlink>` on an image fixture with a hotlink.
    5. **Negative cases:** `/p/thumb`, `/p/view`, `/p/video`, `/p/info` and `/files/previewinfo` write **no** row.

- [ ] **Step 2: Run the tests and confirm they fail.** `build/dev/gotest.sh ./internal/storage/downloadlog/ ./internal/webserver/ -run 'TestDownloadLog|TestEveryDownloadRoute' -v`.

- [ ] **Step 3: Implement**, rerun until the tests PASS, then run the whole `./internal/storage/... ./internal/webserver/...` suite.

- [ ] **Step 4: Commit (source only) and push.** `downloads: one row per original served, through any route`

---

### Task G5: "Shared with me" (worktree `g/shared`)

**Files:**
- Modify: `internal/webserver/Collections.go`. Record the visit at the hook after the password gate (around line 98, before the view is built), and add `SharedWithMe` to the admin view.
- Modify: `html_collections_admin.tmpl`.
- Test: `internal/webserver/SharedWithMe_test.go`.

**Behaviour:**
- **Recording on `/c`:** after the password gate passes, if `authentication.SessionIdentity(r)` reports a signed-in user who is not the collection's owner, call `database.SaveCollectionSeen({UserId, CollectionId, FirstOpened: now})`. Anonymous visitors record nothing. Neither does the password form (spec).
- **`showCollectionsAdmin` view:** add `SharedWithMe []sharedCollectionInfo{Id, Name, Url, OwnerName, FileCount, TotalSize, FirstOpened}`, built from `GetCollectionsSeen(user.Id)`. Use `storage.GetCollection` (live only); skip dead and own collections. `OwnerName` uses `displayName`.
- **Template:** below the user's own collections, an `<h2 class="h5 mt-4">Shared with me</h2>` section, rendered only when the list is non-empty. A table: Name (link to `./c?id=…`, which opens the public page) · From · Files · Size · First opened. There are no actions.

- [ ] **Step 1: Write the failing test.**
  1. User 8 opens `/c?id=` of user 7's collection with a session cookie.
  2. `GetCollectionsSeen(8)` has it.
  3. User 8's `/collections` page contains "Shared with me" and the collection's name.
  4. An anonymous visit records nothing.
  5. The owner's own visit records nothing.
  6. A password-protected collection opened without the cookie (form shown) records nothing.
  7. After `deleteCollection`, the row is gone (G1 covers the database side; assert the page no longer lists it).
- [ ] **Step 2: Run the test and confirm it fails.** Then **Step 3:** implement, and rerun until it PASSES: `build/dev/gotest.sh ./internal/webserver/ -run TestSharedWithMe -v`.
- [ ] **Step 4: Commit (source only) and push.** `collections: "Shared with me" for signed-in visitors`

---

### Task G6: Groups tab, creating, joining, managing (worktree `g/tab`)

**Files:**
- Create: `internal/webserver/GroupsTab.go`, `GroupsTab_test.go`, `templates/html_groups.tmpl`, `templates/html_join.tmpl`, `static/js/admin_ui_groups.js`
- Create: `internal/webserver/authentication/ReturnTo.go`, `ReturnTo_test.go`
- Modify: `internal/webserver/authentication/Authentication.go:240`. Redirect to `PopReturnTo(w, r)` instead of `"admin"`.
- Modify: `internal/webserver/Webserver.go`. Add the route block and `ViewGroups = 7`. In `showLogin`, the already-signed-in redirect (around line 506) and the internal-login success redirect (around line 541) use `authentication.PopReturnTo(w, r)`.
- Create: `internal/webserver/ratelimiter/GroupJoin.go`, `GroupJoin_test.go`
- Modify: `templates/html_header.tmpl`. Add "Groups" after "Files".

**Interfaces:**
- Consumes: G2 everything; `displayName`, `writeJson`, `postOnly`, `readJsonBody`.
- Produces (G10 calls these from the group page):

| Endpoint | Method | Body | OK answer | Who |
|---|---|---|---|---|
| `/groups` | GET page | – | – | anyone signed in |
| `/groups/create` | POST | `{name, days}` | `{Result:"OK", Id, Url:"./g?id=…", InviteUrl, Code}` | anyone signed in |
| `/groups/join` | POST | JSON `{code}` or form `t=<token>` | JSON `{Result:"OK", Id}`; form → 303 to `g?id=` | anyone signed in, rate-limited |
| `/join?t=` | GET page | – | confirmation page | public; signs in first |
| `/g/join?t=` | GET | – | 307 to `../join?t=<token>` | public |
| `/groups/rename` | POST | `{id, name}` | `{Result:"OK", Name}` | creator, admin |
| `/groups/extend` | POST | `{id, days}` | `{Result:"OK", Expires, NeverExpires, Closes}` | creator, admin |
| `/groups/resetcode` | POST | `{id}` | `{Result:"OK", InviteUrl, Code}` | creator, admin |
| `/groups/removemember` | POST | `{id, userId}` | `{Result:"OK"}` | creator, admin |
| `/groups/leave` | POST | `{id}` | `{Result:"OK"}` | members, except the creator |
| `/groups/delete` | POST | `{id}` | `{Result:"OK"}` | creator, admin |
| `/groups/removefiles` | POST | `{id, fileIds}` | `{Result:"OK", Removed}` | per D4 |

Every endpoint above except the pages:
- answers 405 to a non-POST (`postOnly`);
- answers `{Result:"error", ErrorMessage}` on failure: status 403 for `ErrGroupForbidden`, 404 for `ErrGroupNotFound` and for a non-member asking about a group (it is unlisted), 400 otherwise.

Also add:

```go
// closesLabel: "closes 12 Jan", "closes 12 Jan 2027" outside the current year, "never closes"
func closesLabel(g models.Group, now time.Time) string
```

```go
// authentication/ReturnTo.go
const cookieReturnTo = "fgs_return"
var returnToPattern = regexp.MustCompile(`^join\?t=[A-Za-z0-9_-]{1,64}$`)
// SetReturnTo remembers where to go after signing in. Only the join page is
// allowed, so this can never become an open redirect.
func SetReturnTo(w http.ResponseWriter, target string)
// PopReturnTo returns the remembered target, clearing it, or "admin"
func PopReturnTo(w http.ResponseWriter, r *http.Request) string
```

The cookie is `HttpOnly`, `SameSite=Lax`, `Path=/` with `MaxAge=3600`. Clearing it sets `MaxAge=-1`.

```go
// ratelimiter/GroupJoin.go
var groupJoinLimiter = newLimiter()
// IsAllowedGroupJoin is reject-style, like IsAllowedNewUuid: 10 attempts,
// then one every 6 s, per IP and per account. Both must allow.
func IsAllowedGroupJoin(ip string, userId int) bool {
	if isUnitTest {
		return true
	}
	byIp := groupJoinLimiter.Get("ip:"+ip, rate.Every(6*time.Second), 10).Allow()
	byUser := groupJoinLimiter.Get("user:"+strconv.Itoa(userId), rate.Every(6*time.Second), 10).Allow()
	return byIp && byUser
}
```

Check how `newLimiter`, `store` and `isUnitTest` are actually named in `RateLimiter.go`, and adapt.

**Pages:**
- **`/groups` (`html_groups.tmpl`, `ActiveView 7`):**
  - **No groups:** an empty state with only two buttons, "Create group" and "Join with a code".
  - **Otherwise:** a table: Name (link `./g?id=`) · People · Files · Closes · Role. For the admin, every group is listed, and groups he is not in get a `badge` "not a member".
  - **Create dialog:**
    - Name.
    - Expiry radios: "30 days", "60 days (suggested)" (checked), "90 days", "Never".
    - Creating opens the new group's page.
  - **Join dialog:** a code field (placeholder `K7P-4QX`, `autocapitalize="characters"`) and a "Join" button. Errors are shown inline, e.g. "No group with that code or link", or "Too many attempts — wait a minute".
- **`/join?t=` (`html_join.tmpl`):**
  - **Not signed in:** `SetReturnTo(w, "join?t="+token)`, then redirect to `login`.
  - **Signed in, valid token:**
    - already a member → redirect to `g?id=`;
    - otherwise a card: "Join *Iceland trip*?", the people count, the closing date, and a `<form method="post" action="./groups/join">` with a hidden `t` and a "Join" button.
  - **Invalid or expired token:** "This invite link is no longer valid."
  - The template uses the normal `header`/`footer` (top-level path, so the relative assets work).

- [ ] **Step 1: Write the failing tests.**
  - `ReturnTo_test.go`:
    - `join?t=abc` round-trips;
    - `//evil.com`, `https://evil.com`, `admin`, `join?t=a b` and `join?t=abc&x=1` are refused, so `PopReturnTo` gives `"admin"`;
    - Pop clears the cookie.
  - `GroupJoin_test.go`:
    1. Save the current test mode, call `SetUnitTestMode(false)`, and restore it in a `defer`.
    2. Ten calls for one ip/user are allowed; the eleventh is refused.
    3. A different user from the same IP is refused after the IP's burst.
  - `GroupsTab_test.go` (HTTP test server):
    - create with days 45 → 400;
    - create OK → `/groups/list` shows it with role creator;
    - `GET /groups/create` → 405;
    - join by code as user 8 (JSON) → member;
    - join by form `t=` → 303 to `g?id=`;
    - after `ratelimiter.SetUnitTestMode(false)`, 11 wrong codes → the 11th answers 429 `Too many attempts`;
    - rename by member 8 → 403;
    - rename by creator → OK;
    - reset code → the old code fails, the new one works;
    - leave by creator → 400 with `ErrCreatorCannotLeave`'s message;
    - removemember by creator → OK;
    - delete by member → 403;
    - delete by admin → OK;
    - `/g/join?t=x` → 307 with `Location: ../join?t=x`;
    - `/join?t=<valid>` without a session → redirect to login, and a `fgs_return` cookie is set;
    - `/groups` for a user with no groups contains "Create group" and "Join with a code" and no table;
    - for the admin, it contains "not a member" for a group he is not in;
    - `closesLabel` covers the three formats.

- [ ] **Step 2: Run the tests and confirm they fail.** `build/dev/gotest.sh ./internal/webserver/... -run 'TestReturnTo|TestGroupJoin|TestGroupsTab|TestClosesLabel' -v`.

- [ ] **Step 3: Implement** the Go code, the templates and the route block:

```go
	// Part 2, G6: groups tab, joining, managing
	mux.HandleFunc("/groups", requireLogin(showGroups, true, false))
	mux.HandleFunc("/groups/create", requireLogin(createGroup, false, false))
	mux.HandleFunc("/groups/join", requireLogin(joinGroup, false, false))
	mux.HandleFunc("/groups/rename", requireLogin(renameGroup, false, false))
	mux.HandleFunc("/groups/extend", requireLogin(extendGroup, false, false))
	mux.HandleFunc("/groups/resetcode", requireLogin(resetGroupCode, false, false))
	mux.HandleFunc("/groups/removemember", requireLogin(removeGroupMember, false, false))
	mux.HandleFunc("/groups/leave", requireLogin(leaveGroup, false, false))
	mux.HandleFunc("/groups/delete", requireLogin(deleteGroup, false, false))
	mux.HandleFunc("/groups/removefiles", requireLogin(removeGroupFiles, false, false))
	mux.HandleFunc("/join", showJoin)
	mux.HandleFunc("/g/join", redirectGroupJoin)
```

  `/groups/join` reads the IP with `logging.GetIpAddress(r)`. On a refused attempt it answers 429; a refused attempt costs a token like any other.

  Rerun until the tests PASS.

- [ ] **Step 4: Write `admin_ui_groups.js`.** It handles the create and join dialogs (fetch, then `window.location = data.Url`), and builds error text with `textContent`. Run `node --check`.

- [ ] **Step 5: Commit (source only) and push.** `groups: tab, create, join by link or code, return after sign-in, rate-limited`

---

### Task G7: Uploading into a group (worktree `g/upload`)

**Files:**
- Create: `static/js/sha1.js`: an incremental SHA-1 that works in a worker and in node.
- Create: `static/js/sha1-worker.js`.
- Create: `build/dev/sha1_test.mjs`.
- Create: `internal/webserver/GroupsUpload.go` and `GroupsUpload_test.go`, for `/groups/has`.
- Create: `templates/html_group_upload.tmpl`: `define "group_upload_area"`, the drop zone and progress list for a fixed group.
- Modify: `internal/webserver/api/routing.go`. Add `GroupId string \`header:"groupId"\`` to `paramChunkComplete`.
- Regenerate and commit: `internal/webserver/api/routingParsing.go`, through `build/dev/gotest.sh`, which runs `go generate`.
- Modify: `internal/webserver/api/Api.go` `apiChunkComplete` / `doBlockingPartCompleteChunk`.
- Modify: `admin_api.js` `apiChunkComplete`. Add an optional `groupId` header.
- Modify: `admin_ui_upload.js`: the target, the guards for absent settings, the accept hook, and the sink.
- Modify: `html_admin.tmpl`: the "Upload to" selector.
- Modify: `Webserver.go`: the route block. `UploadView`'s upload browser gets `UploadSink = true`. That field exists after G3; this is a one-line handler change, not a component change.

**Interfaces:**
- Consumes: G2 `GetGroup`, `CanSeeGroup`, `FindInGroup`, `StoredSha1IsPlain`, `AttachUploadToGroup`, `displayName`; G3 `UploadSink` / `data-upload-sink`.
- Produces (G10 relies on these):
  - `{{ template "group_upload_area" .Group.Id }}` renders a working uploader for that group.
  - **`GET /groups/has?id=<group>&sha1=<hex>`** → `{"checkable":bool,"exists":bool,"uploader":"anna","fileId":"…"}`.
    - Only members and the admin may ask; anyone else gets 404.
    - `checkable` is false when `!storage.StoredSha1IsPlain()` (D3), and then `exists` is false.
  - **JS:** `FGS.sha1File(file, onProgress) → Promise<hex>`, which hashes in the worker with 4 MiB slices and keeps memory flat.
  - **JS:** `FGS.groupPrecheck(groupId, file) → Promise<{exists, uploader}>`. It resolves `{exists:false}` when the server cannot check, the file is empty, or the hash fails. A precheck failure never blocks an upload.
  - **Chunk complete** accepts the header `groupId`.

**Server, in `apiChunkComplete`, before the non-blocking goroutine starts (errors after the 200 OK never reach the client):**

```go
	var group models.Group
	if request.GroupId != "" {
		g, ok := storage.GetGroup(request.GroupId)
		if !ok || !storage.CanSeeGroup(g, user) {
			sendError(w, http.StatusForbidden, "You are not a member of this group")
			return
		}
		if request.IsE2E {
			sendError(w, http.StatusBadRequest, "End-to-end encrypted files cannot go into a group")
			return
		}
		group = g
		request.UnlimitedDownloads = true
		request.AllowedDownloads = 0
		request.Password = ""
		if g.NeverExpires() {
			request.UnlimitedTime = true
		} else {
			// CreateUploadConfig only knows "now + days"; the exact expiry is
			// applied by AttachUploadToGroup
			request.ExpiryDays = int(math.Ceil(float64(g.Expires-time.Now().Unix()) / 86400))
		}
	}
```

- Check the real field names in `paramChunkComplete` and adapt.
- Pass `group` into `doBlockingPartCompleteChunk`. After `NewFileFromChunk` succeeds and before `preview.Enqueue`, add:

```go
	if group.Id != "" {
		if _, dup := storage.AttachUploadToGroup(group, file, user); dup {
			return // the copy already in the group stays; this one was deleted
		}
	}
```

**Browser:**
- **Upload page, next to "Combine each upload into one link":**
  - `<label for="fgs-upload-target">Upload to</label><select id="fgs-upload-target" class="form-select form-select-sm w-auto">`, with "My files" (value `""`) and one option per group from `GET ./groups/list` (member groups only), filled on load with `textContent`.
  - The select is not stored (D10).
  - **Choosing a group:**
    - add the class `fgs-target-group` to the options area, which greys out and disables the expiry, download-limit, password and Combine cards;
    - show a note: "Files uploaded to a group stay until the group closes and can be downloaded by its members.";
    - when end-to-end encryption is on, the group options are disabled with the title "Not available with end-to-end encryption".
- **`uploadTarget()`:** the `data-fgs-upload-target` of the drop zone when present (group page), else the select's value.
- **`sendChunkComplete`:**
  - when the target is a group, send `groupId` and do not read the absent or disabled settings;
  - guard every `document.getElementById(...)` read, so a page without the settings works.
- **Duplicate pre-check:** Dropzone's `accept(file, done)`. When the target is a group, it calls `FGS.groupPrecheck`:
  - `exists` → `done("Already here, uploaded by " + uploader)`, shown on that file's progress row; nothing is uploaded;
  - otherwise `done()`.
  - While hashing, the progress row says "Checking…".
- **Combine:** skipped when the target is a group (no collection is created).
- **`addUploadedFile`:**
  - adds into `[data-fgs-browser][data-upload-sink="true"]`, not specifically the `upload` context;
  - on a group page, it fetches `./groups/item?id=<g>&file=<id>` (G10) instead of `apiFilesListById`, retrying 3 times one second apart on 404. The attach races the SSE "finished" event by milliseconds. After the third 404 it shows "Already in the group" on the progress row.

  The `./groups/item` call is the only G10 dependency. Write it now; until G10 lands it is exercised only by G11.

- [ ] **Step 1: Write the failing tests.**
  - `build/dev/sha1_test.mjs` (`node build/dev/sha1_test.mjs`) checks:
    - `""` → `da39a3ee5e6b4b0d3255bfef95601890afd80709`;
    - `"abc"` → `a9993e364706816aba3e25717850c26c9cd0d89d`;
    - 1,000,000 × `"a"`, fed in uneven chunks (1, 63, 64, 65, 4 MiB) → `34aa973cd4c4daa4f61eeb2bdbad27316534016f`.
  - `GroupsUpload_test.go`:
    - `/groups/has` for a member, with the SHA1 of a group file → `exists` true, `uploader` = the owner's `displayName`;
    - for a stranger → 404;
    - with `checkable` false when the storage reports salted hashes. Add a test seam in G2, e.g. `storage.SetSha1PlainForTest(false)`, or set the test configuration's encryption level and restore it.
  - API test (in `api` or `webserver`):
    1. `chunk/complete` with `groupId` from a non-member → 403, and no file is created.
    2. From a member, using the existing chunk-upload test helpers (find them in `Api_test.go` with `grep -n "chunk/complete\|apiChunkComplete" internal/webserver/api/*_test.go`), the resulting file:
       - is in `GetGroupFileIds`;
       - has `UnlimitedDownloads` set;
       - has `ExpireAt == group.Expires`.
    3. A second upload of the same bytes into the same group:
       - leaves one group member;
       - leaves the new file with `ExpireAt == 0` (deleted).
- [ ] **Step 2: Run the tests and confirm they fail.** `node build/dev/sha1_test.mjs` and `build/dev/gotest.sh ./internal/webserver/... -run 'TestGroupsHas|TestChunkCompleteGroup' -v`.
- [ ] **Step 3: Implement** the server side.
  - Run `build/dev/gotest.sh ./internal/webserver/api/ -v` once. It regenerates `routingParsing.go`; check it in `git diff`.
  - Route block:

```go
	// Part 2, G7: uploading into a group
	mux.HandleFunc("/groups/has", requireLogin(groupHas, false, false))
```

- [ ] **Step 4: Implement** the browser side.
  - `sha1-worker.js` is served the way `sse-worker.js` is. Find how with `grep -rn "sse-worker" internal/webserver --include=*.go --include=*.tmpl --include=*.js | grep -v "static/js/min"`, and follow it, including any hashed-name or minify list entry.
  - Run `node --check` on every changed JS file. Run `node build/dev/sha1_test.mjs`.
- [ ] **Step 5: Run** the full `./internal/webserver/...` and `./internal/storage/...` suites.
- [ ] **Step 6: Commit and push** source plus `routingParsing.go`. `upload: into a group, with a duplicate check before anything is sent`

---

### Task G8: Add to group…, Turn into a group (worktree `g/add`)

**Files:**
- Create: `internal/webserver/GroupsAdd.go`, `GroupsAdd_test.go`
- Create: `static/js/admin_ui_groupactions.js`
- Modify: `html_collections_admin.tmpl`. Add the "Turn into a group" button and its small dialog.
- Modify: `Webserver.go`. Add the route block.

**Interfaces:**
- Consumes: G2 `AddFilesToGroup`, `GroupFromCollection`, `GroupExpiryDays`; G3's `add-to-group` menu and bulk keys and the `#fgs-group-modal` markup.
- Produces:
  - **`POST /groups/add`** `{id, fileIds}` → `{Result:"OK", Added:n, Duplicates:[{FileId, Name, Uploader}], Refused:[{FileId, Name, Reason}]}`.
  - **`POST /groups/fromcollection`** `{collectionId, days}` → `{Result:"OK", Id, Url:"./g?id=…", Added:n}`.
  - Both refuse non-POST with 405. Errors follow G6's status mapping.

**Browser (`admin_ui_groupactions.js`):**
- `FGS.registerFileActions({"add-to-group": openAddToGroup})`, for every context.
- `openAddToGroup(a)` fills `#fgs-group-what`, e.g. "5 files", noting any end-to-end encrypted files left out.
- It loads `./groups/list` (the caller's groups) into `#fgs-group-list` as buttons (name + "N files").
  - **With none:** "You are not in any group yet." and the footer link.
  - **Clicking a group** POSTs, then writes into `#fgs-group-result`: "Added 3 to “Iceland trip”." Plus, when there are any, "2 already there (uploaded by anna)." and "1 left out: not yours."
  - Use `textContent` throughout.
- **Turn into a group:** shown on collection rows the user owns (or all rows for the admin), next to the existing row actions.
  - The dialog has the same four expiry radios as G6, with 60 checked, and a "Turn into a group" button.
  - On OK it navigates to `data.Url`.

- [ ] **Step 1: Write the failing HTTP tests.**
  - **add:**
    - own files → added;
    - another user's file by a non-admin → refused "not yours";
    - the admin adds anyone's;
    - a duplicate by SHA1 → listed with the uploader name;
    - e2e → refused;
    - a non-member → 404;
    - `GET` → 405.
  - **fromcollection:**
    - owner → the group exists with the collection's name and files;
    - the collection is unchanged;
    - a stranger → 403/404;
    - days 45 → 400.
- [ ] **Step 2: Run the tests and confirm they fail.** Then **Step 3:** implement, rerun until they PASS: `build/dev/gotest.sh ./internal/webserver/ -run 'TestGroupsAdd|TestGroupFromCollection' -v`.
  - Route block:

```go
	// Part 2, G8: adding files to a group
	mux.HandleFunc("/groups/add", requireLogin(addToGroup, false, false))
	mux.HandleFunc("/groups/fromcollection", requireLogin(groupFromCollection, false, false))
```

- [ ] **Step 4: Write the JS and the template.** Run `node --check`.
- [ ] **Step 5: Commit (source only) and push.** `groups: add existing files, turn a collection into a group`

---

### Task G9: Users page (worktree `g/users`)

**Files:**
- Modify: `Webserver.go`. In `case ViewUsers` and in `userInfo`, add `FileCount int`, `TotalSize string`, and `Groups []string` (names, via `storage.GroupsOfUser`).
- Modify: `html_users.tmpl`. Add the columns Files · Size · Groups, and a "View files" link → `./files?user=<id>`.
- Modify: `admin_ui_users.js` `addRowUser`. Add three more cells with defaults `0`, `0 B` and `—`, keeping the cell order identical to the template.
- Modify: `Files.go` `showFiles`. Accept `?user=<id>` for users with `HasPermissionListOtherUploads()`:
  - list only that user's files;
  - `ShowScope` false;
  - the page heading reads "<displayName>'s files" with a link back to Users.
  - The parameter is ignored for anyone else.
- Test: `Users_test.go` (new).

**Counting:** reuse the listing rules of `isListedFile` in `Files.go` (live, not request uploads). Size uses `helper.ByteCountSI` or whatever helper the codebase already uses for `Size` strings.

- [ ] **Step 1: Write the failing tests.**
  - The admin's `/users` contains the fixture user's file count and group name.
  - `/files?user=7` for the admin lists only user 7's files and contains "User's files" (fixture user 7's name is `User`).
  - `/files?user=5` as user 7 shows user 7's own files, so the parameter is ignored.
- [ ] **Step 2: Run the tests and confirm they fail.** Then **Step 3:** implement, rerun until they PASS: `build/dev/gotest.sh ./internal/webserver/ -run 'TestUsersPage|TestFilesForUser' -v`.
- [ ] **Step 4: `node --check` the JS.** Commit (source only) and push. `users: files, size and groups per person; their files on the Files grid`

---

### Task G10: The group page (on `feature/groups`, after the wave 3 merge)

**Files:**
- Create: `internal/webserver/GroupPage.go`, `GroupPage_test.go`, `templates/html_group.tmpl`, `static/js/admin_ui_group.js`
- Create: `internal/storage/NearCopies.go`, `NearCopies_test.go`
- Modify: `Webserver.go`. Add the route block.

**Interfaces:**
- Consumes everything above.
- Produces:

```go
// internal/storage/NearCopies.go
const nearCopyMaxDistance = 6 // dHash bits; tuned on the preview with real photos (D11)
type NearCopy struct {
	FileId    string // the viewer's own item
	MatchId   string // someone else's item it looks like
	MatchUser int
	MatchAt   int64 // taken-at of the match, or its upload date
}
// NearCopies: for each of userId's files in files with a ready preview and a
// non-zero PHash, the closest item from another uploader within the
// threshold, unless kept (Keep both). Exact SHA1 twins are not near-copies.
func NearCopies(files []models.File, userId int, kept map[string]bool) []NearCopy
```

| Route | Wrapper | What |
|---|---|---|
| `/g?id=` | `requireLogin(showGroup, true, false)` | the page. Non-member, non-admin → 404 (unlisted). |
| `/gd?id=&file=` | `requireLogin(downloadGroupFile, false, false)` | `CanSeeGroup` and the file in `GroupFiles(g)`, else 404. `r = tagDownload(r, models.DownloadViaGroup)`, then `storage.ServeFile(file, w, r, true, true, forceDecryption, true)`, the same flags as `/cd`. |
| `/gz?id=&files=` | `requireLogin(downloadGroupZip, false, false)` | the selected live members (reuse `selectedMembers` from Zip.go), `tagDownload(r, models.DownloadViaZip)`, `storage.ServeZip(files, g.Name, w, r, true)` |
| `/groups/item?id=&file=` | `requireLogin(groupItem, false, false)` | JSON for a live add: `FileApiOutput` fields plus `UploaderId`, `UploaderName`, `Downloaded`, `CanRemove` and `Preview` (`previewInfo`). Records nothing. 404 when the file is not in the group. |
| `/groups/nearkeep` | `requireLogin(keepNearCopy, false, false)` | POST `{id, fileId}`. Only the owner of `fileId`. Calls `SaveNearCopyKept`. |

**Page (`html_group.tmpl`, `GroupView{AdminView; …}`):**
- **Header:**
  - the name;
  - the people as initials circles (D9, colour `hsl((id*47)%360, 45%, 45%)`) with their names;
  - `closesLabel`;
  - invite link and code, each with a copy button (ClipboardJS `.copyurl` as elsewhere).
- **Creator and admin controls:** Rename · Extend (four radios, the current option disabled when it would not be later) · Reset code (confirms first) · Remove member (× by each non-creator name) · Delete group (confirms first).
- **Members:** "Leave group" (confirms first).
- **Admin who is not a member:** a banner "You are not a member of this group".
- **Near-copies:** when `NearCopies` is non-empty, a list above the board. Each entry: "Looks like Anna's photo from 14:32", both thumbnails, and "Keep both" / "Remove mine".
  - "Remove mine" → `/groups/removefiles` with that file, then `browser.removeItem`.
  - "Keep both" → `/groups/nearkeep`, then the entry hides.
- **Upload area:** `{{ template "group_upload_area" .Group.Id }}` (G7).
- **The board:**
  - `newFileBrowser("fgs-group", ctxGroup, items, user)` then `.WithGroup(g, people)` and `UploadSink = true`.
  - Items are `GroupFiles(g)` sorted by `TakenAt` (from the preview), falling back to `UploadDate`, ascending, with the id as tie-break.
  - Each item gets `Downloaded` from `GetDownloadedFileIds(user.Id)`, and `CanRemove = CanManageGroup(g,user) || f.UserId == user.Id`.
  - `people` are the members who have at least one item, in member order.
- **Singletons:** include `media_viewer`, `media_help_sheet`, `file_browser_dialogs` and `file_browser_support` once.

**Browser (`admin_ui_group.js`):**
- `FGS.registerFileActions({"remove-from-group": removeFromGroup}, "group")`:
  - it sends only tiles with `data-can-remove="true"` to `/groups/removefiles`, then calls `a.browser.removeItem` for each;
  - the status reads "Removed 3 from the group." and, if any were skipped, "2 skipped: not yours.".
- The header controls call G6's endpoints, and update the DOM in place with `textContent`. Leave and Delete navigate to `./groups`.
- The near-copy actions.
- Copy buttons.

- [ ] **Step 1: Write the failing tests.**
  - **`NearCopies_test.go`:**
    - two files of different owners with PHash distance 3 → one flag for the viewer's file;
    - distance 7 → none;
    - same owner → none;
    - kept → none;
    - same SHA1 → none;
    - PHash 0 → none.
    - Save `models.Preview` rows for the fixtures with `database.SavePreview`.
  - **`GroupPage_test.go`:**
    - **The member's `/g?id=`** contains:
      - the name and "closes";
      - the code;
      - `data-context="group"`;
      - `data-default-view="board"`;
      - "Select everything new to me".
    - **Order:** items are in taken-at order. Assert the index of two file ids in the body.
    - **Ticks:** a file the member downloaded through `/gd` renders `data-downloaded="true"`. That proves the row was recorded via `group`; check `GetDownloadedFileIds`.
    - **A stranger's `/g?id=`:** 404. **The admin non-member:** "You are not a member of this group".
    - **`/gd`:**
      - a stranger → 404;
      - a file not in the group → 404.
    - **`/gz` with two ids:** two Downloads rows with `zip`.
    - **`/groups/item`:**
      - a group file → 200 with `UploaderName`;
      - another file → 404;
      - no row is recorded.
    - **`/groups/nearkeep`:** a member who does not own the file → 403.
- [ ] **Step 2: Run the tests and confirm they fail.** Then **Step 3:** implement, rerun until they PASS: `build/dev/gotest.sh ./internal/storage/ ./internal/webserver/ -run 'TestNearCopies|TestGroupPage|TestGroupDownloads|TestGroupItem' -v`.
  - Route block:

```go
	// Part 2, G10: the group page
	mux.HandleFunc("/g", requireLogin(showGroup, true, false))
	mux.HandleFunc("/gd", requireLogin(downloadGroupFile, false, false))
	mux.HandleFunc("/gz", requireLogin(downloadGroupZip, false, false))
	mux.HandleFunc("/groups/item", requireLogin(groupItem, false, false))
	mux.HandleFunc("/groups/nearkeep", requireLogin(keepNearCopy, false, false))
```

- [ ] **Step 4: Write the JS.** Run `node --check`. Then run the full suite: `build/dev/gotest.sh 2>&1 | grep -E '^(--- FAIL|ok|FAIL)'`.
- [ ] **Step 5: Commit and push.** `groups: the group page — board in shooting order, filters, ticks, near-copies`

---

### Task G11: End to end (controller, after G10)

**Files:** create `build/e2e/groups_e2e.py`, modelled on `collections_e2e.py`: urllib, two cookie jars, internal auth. It runs against a local test container on `127.0.0.1:53900`, set up as `setup_local.py` does.

**The script covers, with PASS/FAIL lines:**
1. User A logs in and creates a group with 60 days. Read the code and the invite link from `/groups/create`.
2. User B joins by link: `GET /join?t=` shows the confirmation page, and `POST /groups/join` returns 303 to `g?id=`.
3. User C joins by code.
4. A uploads two files into the group through `uploadChunk` and `chunk/complete` with `groupId`. Wait for `/groups/item` to answer 200.
5. B asks `/groups/has` with A's file's SHA1 → `exists`, `uploader`.
6. B uploads the same bytes anyway; the group still has two items.
7. B downloads one through `/gd`. B's `/g?id=` then shows that tile with `data-downloaded="true"` and the other with `false`.
8. B downloads both through `/gz?files=a,b`. The zip has two entries, byte-equal to the originals.
9. A new user D with no groups gets `/groups` with the empty state.
10. `collections_e2e.py` still passes.

**Browser walkthrough** with the Playwright MCP, on the preview (see Deploy below), at desktop width and on an iPhone viewport (390×844, iOS user agent):
- create a group;
- join it in a second browser context by link and by code;
- the empty state;
- upload a duplicate on the group page ("Already here, uploaded by …");
- the board in shooting order, the person filter, "Hide what I've downloaded";
- the downloaded ticks after a download;
- "Select all (N)" and "Download as .zip";
- "Select everything new to me";
- a near-copy, made with a re-encoded JPEG of the same photo, then "Keep both";
- Add to group… from the Files tab;
- Turn into a group;
- "Upload to" on the Upload page;
- the viewer on the group page;
- "Shared with me".

- [ ] **Step 1: Write the script.** Run it against the test container and confirm every line is PASS.
- [ ] **Step 2: Run the browser walkthrough** and fix anything it finds as follow-up commits.
- [ ] **Step 3: Commit and push.** `e2e: groups, two users, duplicates, ticks, zip`

---

## Controller: merges, review, preview, deploy

1. **After wave 1:** review G1 and G2 on `feature/groups`.
2. **Waves 2 and 3:**
   - Create a worktree per task:

     ```bash
     git -C ~/src/Gokapi worktree add ~/src/Gokapi-wt/<task> -b g/<task> feature/groups
     ```

   - Review each task as it finishes.
   - Merge into `feature/groups` in this order: G3, G4, G5, then G6, G7, G8, G9.
   - After each wave, regenerate the minified assets once (`build/dev/gotest.sh` runs `go generate`), commit `build: regenerate static assets after wave N`, and run the full suite.
3. **G10, then G11.**
4. **Whole-branch review** (superpowers:requesting-code-review) against `feature/collections`.
5. **Preview for Franco** (MacBook and iPhone). The live volumes are only ever read:

   ```bash
   docker volume create gokapi_grp_data && docker volume create gokapi_grp_config
   docker run --rm -v gokapi_gokapi_data:/from:ro -v gokapi_grp_data:/to alpine cp -a /from/. /to/
   docker run --rm -v gokapi_gokapi_config:/from:ro -v gokapi_grp_config:/to alpine cp -a /from/. /to/
   ```

   - Switch the copy's `config.json` to internal auth with a temporary password, as Part 1's preview did.
   - Copy the branding to `~/src/gokapi-grp-custom/` and add the Groups icon there.
   - Build `gokapi-fgs:groups`.
   - Run it on `127.0.0.1:53910` with the compose file's hardening: `no-new-privileges`, `cap_drop ALL`, `pids_limit 200`, `mem_limit 1g`, `cpus 1.5`, `TZ`, and the `GOKAPI_*` env.
   - Expose it with `tailscale serve --bg --https=8443 http://127.0.0.1:53910`.
   - Internal auth means joining is tested through the internal-login return path. Both paths call `PopReturnTo`.
6. **Deploy, only after Franco's explicit yes:**
   1. Build the image tag.
   2. Stop `gokapi`, back up the DB with a sidecar `sqlite3 .backup`, and copy the backup to `~/apps/gokapi/backups`.
   3. Back up `~/apps/gokapi/custom/admin.js` and `version.txt` as `.bak.<date>`.
   4. Add `groups: "bi-people"` to both icon maps (`icons` keyed on href, `iconsByLabel` keyed on label) and bump `version.txt`.
   5. Swap the image in compose.
   6. Smoke-test: `/groups`, a group page, `/g/join`, a `/d` link, a collection link. Check the logs for any `[warning]`.
   7. Open the PR `feature/groups` → `feature/collections` and link it.
   8. Remove the preview: the container, its volumes, the tailscale serve entry, and `~/src/gokapi-grp-custom`.
7. **Memory:** update `~/atlas/memory/gokapi-share-redesign.md` with what is live and what comes next.


---

## Rulings during execution (2026-10-20)

Decisions the controller took while executing this plan; the ledger they came from is scratch and is deleted after go-live, so this is the durable record.

- TestExtendPropagates sets the group's Expires to now+31d (saved) before "extend 30 → ErrGroupNotLater", so the refusal is deterministic — the plan's intent is the edge case, not timing — cost if wrong: none, test-only.
- G3 updates the action-table test so HasMenu("edit") is true for ctxGroup too — follows the D4 revision Franco asked for — cost if wrong: none.
- G4's anonymous case asserts no row for users 7 and 8 after a cookie-less /downloadFile, and a separate session-8 request records for 8 — removes the muddled wording, same intent — cost if wrong: none.
- Redis RemoveFileFromGroups is a no-op, not a panic — CleanUp calls it on every deletion, a panic would break deletion on Redis — cost if wrong: none (Redis unused live).
- SQLite RemoveFileFromGroups also clears that file's GroupNearKept rows — no dangling kept-marker — cost if wrong: none.
- group "admin" = IsAdmin() && Id != 0 (zero User is UserLevelSuperAdmin) — stricter than D17, closes a real trap — cost if wrong: none.
- GroupFromCollection on a dead collection returns ErrorCollectionNoFiles; AttachUploadToGroup leaves a refused upload private; names capped at 200 runes — all consistent with the brief's intent — cost if wrong: small, visible in G7/G8 tests.
- parallel tasks also restore go-generate outputs ProtectedUrls.go and docs/setup.rst before committing; the controller regenerates them once after each merge — avoids merge conflicts on generated files — cost if wrong: one regenerate commit.
- G3 group-locked save keeps sending originalPassword:true (only `password` omitted) — omitting it means "remove password" server-side and would 400 every rename of a protected group file — cost if wrong: none.
- G4 recordDownload skips HEAD requests — a link-preview HEAD sends no bytes and must not tick "downloaded" — cost if wrong: none.
- G3's GroupLock for live adds is an addItem input field `file.GroupLock` (string, group name); G7's /groups/item consumer and G10's endpoint supply it — keeps the lock in the component, cost if wrong: a later small wiring change.
- G6 form POST /groups/join failures 303 to ../join?t= ("no longer valid") and 429 renders an asset-free page with a Try again link — a page under /groups/ can't load relative assets (D19) — cost if wrong: cosmetic.
- G6 /groups/join JSON adds Url — lets JS navigate — cost: none.
- G7 blocking chunk/complete duplicate answers 409 "Already in the group"; group uploads get ExpiryTimestamp = g.Expires exactly and UnlimitedTime false for closing groups; openapi.json documents groupId — all needed for correctness — cost: G11 must expect 409 in blocking mode.
- G8 /groups/fromcollection returns Added only; ErrGroupsUnsupported → 501 — brief-compliant — cost: none.
- G9 per-person heading uses displayName ("user's files" — names are stored lowercase) — cost: cosmetic.
- G9 must not show two count columns: drop the new "Files" column, keep upstream "Uploads" as the file count, and compute Size over the same files Uploads counts; keep "Groups" — two adjacent near-identical counts confuse — cost if wrong: one column.
- G10 person filter lists every uploader with items in the group (members in member order, then former members), not only current members — otherwise a leaver's photos can't be filtered to — cost if wrong: one extra option.
- /groups/nearkeep owner-only (creator/admin 403) — "Keep both" is the uploader's choice per spec — cost: none.
- G10's group.css and /gd,/gz using tagDownloadUser accepted.
- near-copies are flagged only to the uploader of the NEWER item (later UploadDate; tie → larger id), per spec "the new item's … hash is compared … flagged to its uploader"; so "Keep both" by that uploader dismisses the pair for good — cost if wrong: the older uploader never sees the pair.
- one final fix wave covers I1–I5, M1 (Upload-page group live add via /groups/item), M4 (fromcollection must not leave an empty group), and the Everything-view gap — spec says "their own files plus those in their groups" — cost if wrong: small scope growth.
- Groups sidebar icon is bi-people-fill, not the plan's bi-people (Users already uses bi-people) — cost if wrong: cosmetic.
- Final review fix wave (beyond the brief): `/collections/delete` is POST + JSON only; the `/groups/join` form path is refused when Origin is foreign (the live `ServerUrl` is `https://share.franco.international/`).
- `/custom/` serves only the named branding files; no directory listing (found during the preview walkthrough; live had a public listing including `.bak` files).
- Group upload area hint is "Up to 10 GB per file" via a branding CSS override (`.fgs-group-upload #fgs-uploadlimit::before`); the live branding needs the same rule at deploy.
