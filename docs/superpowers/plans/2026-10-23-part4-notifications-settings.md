# Part 4 — Activity feed, quieter emails, Settings, display names, blocklist: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Franco gets a few clear emails instead of one per upload: new people, new groups, warnings and every "View as…" at once, everything else as a summary grouped by group and person. Friends can ask for one email a day about their own files. Everyone has a Settings page with their display name, which replaces the email prefix everywhere, and their Google photo replaces the initials avatars. The admin can block an email address or a domain.

**Architecture:**
- **Activity:** an `Activity` table, created `IF NOT EXISTS`, gets one row per event, written at the moment the event happens. Its autoincrement `Id` is the cursor. `GET /api/activity` (admin key, `ManageLogs`) returns events after a cursor, plus every person's name, email and email setting.
- **Notifier:** the notifier is rewritten as a pure state machine, `step(state, events, users, now) -> (state, emails)`, wrapped in a thin poll-and-send loop. It is replay-tested against a recorded evening on the host's `/usr/bin/python3` (3.9.6). It lives in the repo (`deploy/notifier/`); its config and credentials stay in `~/apps/gokapi/notify.config.json`.
- **People:** a new package `internal/people` is the only source of a person's shown name, initials and photo. `displayName(u)` and View-as's copy of it delegate to it. A `Profiles` table holds the display name, the Google name, the photo bytes, "Use initials instead", and the email setting.
- **Sign-in:** the OAuth sign-in gets explicit guard and hook lists, filled in one place at startup. The blocklist is a guard; profile capture and first-sign-in activity are hooks.

**Tech Stack:**
- Go, tested only through `build/dev/gotest.sh` in Docker on Colima;
- SQLite, Go `html/template`, vanilla JS, Bootstrap 5;
- Python 3.9 stdlib for the notifier and its test (`unittest`, `zoneinfo`, `smtplib`);
- Python e2e scripts (`urllib`), and the Playwright MCP for the walkthrough.

**Spec:** `docs/design/2026-10-19-group-sharing-design.md`, section "Part 4 — Notifications and Settings" (lines 526–597), plus "Navigation".

Rulings from Parts 1–3 still bind: D1–D20 in `2026-10-20-part2-groups.md`, and every "Rulings during execution" section in `2026-10-19-*.md`, `2026-10-20-part2-groups.md`, `2026-10-21-part2-revisions.md`, `2026-10-21-part3-batching-photos-viewas.md` and `2026-10-22-part3-revisions.md`.

---

## Decisions (Franco answered "sounds good" to the defaults on 2026-10-23)

| # | Gap | Decision | Task |
|---|---|---|---|
| Q1 | Where the feed comes from | A new `Activity` table, one row per event, written when it happens. `Id` (autoincrement) is the cursor. Rows are kept 30 days. A cursor older than the oldest row, or newer than the newest (a DB restored from backup), answers `reset: true`, and the notifier carries on from `next` without sending old events. | A |
| Q2 | Disk warning | Warn at **20 GB** free, not 10: live already refuses uploads below 10 GB (`GOKAPI_MIN_FREE_SPACE=10240`). Checked every 10 minutes on the data directory. Warn again only after free space has climbed back above 20 GB. A restart while still low warns once more; that is accepted. | A |
| Q3 | Preview failures | One `warning_preview` event per file, written only after its **final** attempt fails (`Attempts >= maxAttempts`). The notifier puts every warning in one poll into **one** email. | A, N |
| Q4 | Franco's own activity | Uploads, downloads, joins and groups by an admin user are left out of Franco's emails, and do not count as activity for the one-hour quiet rule. The 3-hour cap counts from the first held event. **Exception:** "View as…" events (Q13). | N |
| Q5 | `log.txt` | The notifier no longer reads it. `[auth]`, deprecation and unknown-category lines stop being emailed. Their replacements are the feed's warnings and the View-as events (Q13). | N |
| Q6 | Summary sections | One section per group, in the order of first activity. Then **Your files** (files owned by an admin, outside any group). Then one section per other person's own files ("Anna's files"). A file in a collection but in no group goes under its owner. | N |
| Q7 | Friends' emails | One digest a day at **19:00 Europe/Zurich**, covering what was collected since their last digest. It is skipped if nothing happened. A person's downloads of their own files never count. Every email ends with a link to `/settings`. English only. It goes only to accounts whose name contains `@`. | N, S1 |
| Q8 | No real email from the preview | The notifier has `redirect_all_to`, enforced in its single send function, so every recipient is rewritten. It also refuses to send when `api_url` is not the live one and no redirect is set. The preview runs with the redirect set to Franco. | N |
| Q9 | Display name | Kept from Google's `name` claim at every sign-in (`GoogleName`). A person can set their own (`DisplayName`): trimmed, 1–40 characters, no control characters, not unique, empty means "back to the default". Shown name = `DisplayName`, else `GoogleName`, else the part before "@". Everyone, admin included, sees shown names. The Users page (admin) shows the name and the email. | S1, S2 |
| Q10 | Profile photo | Fetched from Google's `picture` claim at sign-in, at most once a day per person. Only `https://*.googleusercontent.com`, 5 s timeout, at most 512 KB, sniffed as JPEG, PNG or WebP. Stored in the database and served by Gokapi at `/avatar?id=N&v=<updated>`. It is visible to yourself, the admin, and people who share a group with you. Settings has a "Use initials instead" switch. | S1, S2 |
| Q11 | Where Settings appears | In the user-icon menu only, above Logout. Not in the sidebar, so no branding change is needed. | S2 |
| Q12 | The notifier's access | An admin API key with `ManageLogs`, in `notify.config.json` on the mini only. It is created on live during the approved deploy. | Deploy |
| Q13 | "View as…" alarm (keeps Part 3's P3) | `viewas_start` and `viewas_end` are feed events, written next to the existing `[auth]` log lines. They are emailed **immediately** and exempt from Q4: an email Franco did not trigger is a break-in signal. | A, N |
| Q14 | Blocklist (Franco: yes) | Entries are exact emails or `@domain`, matched case-insensitively. `@gmail.com` matches `x@gmail.com`, not `x@evilgmail.com` or `x@mail.gmail.com`. Blocking ends the person's sessions and their API keys stop working. Their files and download history stay. The blocklist is checked at Google sign-in **before** an account is created, at password login, and on every API key. An entry that matches an admin is refused. Blocked attempts are `signin_blocked` events, summarised. It is managed in a "Blocked" section on the Users page. | K |

**Rulings taken while planning (Franco may veto):**
- **R-a. The notifier's code moves into the repo** at `deploy/notifier/gokapi_notify.py`, with its tests and a synthetic fixture. The repo is private.
  - At deploy the file is copied to `~/apps/gokapi/gokapi-notify.py`, after a `.bak.<date>` backup.
  - Config, state and credentials stay on the host and are never committed.
  - Why: the replay test has to live somewhere reviewable, and the old script was the only part of the system without history.
- **R-b. The cursor travels in a `since` header, not `?since=`.** Gokapi's API router matches on `r.URL.String()` (`api/Api.go:80`), so a query string never routes. Every other Gokapi API parameter is a header (`/logs/get` uses `timestamp`).
- **R-c. The busy-evening fixture is invented.** Invented names and files, timings modelled on the `[FGS]` send times of 2026-10-18 in `notify.log`. No live data, backups or `log.txt` go into it.
- **R-d. Cutover.**
  - The new notifier's first run uses `--from-start`, which reads the `Activity` table from row 1. That is safe because the table is new at the swap, and nothing that happens between the image swap and the notifier start is lost.
  - Without the flag, a missing cursor means "start from now".
  - The state file is new (`.notify-state-v2.json`).

---

## Global Constraints

Everything in `2026-10-20-part2-groups.md` and `2026-10-21-part3-batching-photos-viewas.md` → Global Constraints still applies. Updated for Part 4:

- **No Go toolchain on the host.** Every `go` command runs through `build/dev/gotest.sh`, which also runs `go generate`.
  - It sets `DOCKER_HOST=unix:///Users/franco/.colima/default/docker.sock`, passes `-tags=test,awsmock`, and serialises runs with a lock.
  - `internal/encryption` and `internal/configuration/setup` are killed by the VM's memory. That is known: ignore them, and delete a left-behind `internal/configuration/setup/test/`.
  - Check for `--- PASS: <TestName>`. Example: `build/dev/gotest.sh ./internal/people/ -run TestShownName -v`.
- **Docker:** every docker command names the context: `docker --context colima`. Only one agent at a time uses port 53900 and the shared Playwright browser.
- **Protected routes** are registered literally in `internal/webserver/Webserver.go` as `mux.HandleFunc("<path>", requireLogin(...))`.
  - Each task adds **one contiguous block**, commented with the task (`// Part 4, S1: settings and avatars`), directly after the Part 3 blocks.
- **Mutations are POST + JSON only** and answer 405 to anything else (D20). Pages and feeds are GET and never write.
- **"View as…" guard:** every new route is allowlisted read-only in `viewas/Guard.go` or refused by default. `TestViewAsRoutes` must stay green.
  - New GET routes `/settings` and `/avatar` are allowlisted.
  - `/settings/save`, `/users/block` and `/users/unblock` are refused, which is the default.
  - `/api/activity` is under `/api/`, which is always refused.
- **Logging:**
  - New code logs to stdout (`fmt.Println`) only. Never `logging.LogWarn`, and no new log category: the **old** notifier stays live until the deploy and emails every `[warning]` and unknown category.
  - At most one `[info]` line per startup.
  - Activity rows are the new channel: they are **not** log lines.
- **Schema stays at version 17.** New tables (`Activity`, `Profiles`, `Blocklist`) use `CREATE TABLE IF NOT EXISTS` in `ensure…` functions chained after `ensureViewAsTable()`.
  - No `ALTER TABLE` on upstream tables. No `Upgrade` steps. Quote table names.
  - The Redis provider gets no-op stubs, as for groups (`SupportsGroups()` false means feature off).
- **Names:** every name shown to a person comes from `people.Name(u)`. Initials come from `people.Initials(u)`, and the photo from `people.PhotoUrl(u)`, which is empty when there is none.
  - `displayName(u)` in `GroupsCommon.go` and `viewas/ViewAs.go` becomes a one-line call to `people.Name`.
  - Raw `User.Name` (the email) appears only on the Users page, next to the shown name, and in the user menu as a second line.
- **Security:**
  - Build any DOM containing a file, group or person name with `textContent` and attribute setters only, never `innerHTML`. A display name is now user input.
  - Anything that puts a name into a header or cookie must encode it. The View-as cookie already uses `url.QueryEscape`; `viewas.js` must decode `+` as a space.
- **New code goes in new files** where possible; edits to upstream files stay small.
- **Commits** end with `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`. Push after each commit.
- **Branches and worktrees:**
  - The branch is `feature/part4`, cut from `feature/collections` at `707fc6e`.
  - Parallel tasks use branches `p4/<task>` in worktrees `~/src/Gokapi-wt/p4-<task>`, created by the controller.
  - Colima mounts only paths under `~`.
- **Parallel tasks commit source only.** Before committing, restore the regenerated files:

  ```bash
  git checkout -- internal/webserver/web/static/js/min internal/webserver/web/static/css/min \
    internal/webserver/web/templates/string_constants.tmpl build/go-generate/minifyStaticContent.go \
    internal/webserver/api/VersionNumbers.go internal/webserver/ProtectedUrls.go docs/setup.rst 2>/dev/null || true
  ```

  - **Exception:** `internal/webserver/api/routingParsing.go` *is* committed by task A. It is generated from the route structs, and the controller re-checks it after merge.
  - A new `admin_*.js` file is bundled automatically. A new non-`admin_` JS file needs a converter entry in `build/go-generate/minifyStaticContent.go`; no Part 4 task needs one.
  - The controller regenerates the minified assets once per merge and git-adds any new min file.
- **Shared component ownership:**
  - **W0 (controller)** owns `authentication/Authentication.go` and the first version of `internal/people`.
  - **S2** alone owns the shared UI: `html_header.tmpl` (user menu), `html_file_browser.tmpl`, `media.js`, `media.css`, `html_group.tmpl`, `group.css`, `admin_ui_group*.js`, `html_admin.tmpl`, `admin_ui_upload.js`, and the name and avatar fields in `FileBrowser.go` and `GroupPage.go`.
  - **K** owns `html_users.tmpl` and `admin_ui_users.js` until it merges. S2 then adds name + email there.
  - A wave-1 task that needs a change in a file owned by someone else reports NEEDS_CONTEXT instead of making it.
- **Never touch** the live `gokapi` container, its volumes (`gokapi_gokapi_data`, `gokapi_gokapi_config`) or anything under `~/apps/gokapi`. Only the controller makes previews and deploys, and only after Franco's explicit yes for the deploy. Reading `notify.log` and the plist is fine.
- No background processes. Implementers do not dispatch subagents.
- **In wave 1** implementers run **Go or Python tests only**. Container and Playwright checks are listed as **"Controller checks"** and run after the merge, one at a time.
- **Photo dates:** no copy promises dates (Part 3 P11 still holds).
- **Exact copy strings:**
  - Settings page:
    - title `Settings`;
    - headings `Your name`, `Your photo`, `Emails`;
    - name field label `Display name`;
    - help `Shown to the people you share with. Leave empty to use your Google name.`;
    - switch `Use initials instead of my Google photo`;
    - switch `Email me when my files are downloaded`;
    - help `At most one email a day, at 19:00, and only if someone downloaded something.`;
    - no-email note `Your account has no email address, so it cannot get emails.`;
    - button `Save`, then the toast `Saved`.
  - Upload page switch: `Email me when my files are downloaded`.
  - User menu: the shown name; the email on a second line in small muted text; then `Settings`, a divider, and `Logout`.
  - Users page:
    - row `Anna Muster` with `anna@example.com` under it;
    - section `Blocked`;
    - input placeholder `name@example.com or @example.com`;
    - button `Block`, row button `Unblock`;
    - row menu item `Block this person`;
    - confirm `Block anna@example.com? They are signed out now and cannot sign in again. Their files stay.`;
    - refusal `You cannot block an admin.`
  - Friend email:
    - subject `Your files were downloaded`;
    - lines like `Ben and Carla downloaded 41 of your photos from Iceland trip.` and `Someone downloaded "CV.pdf" 3 times.`;
    - footer `You get this because "Email me when my files are downloaded" is on. Turn it off: <site>/settings`.
  - Franco's emails:
    - `[FGS] New person: <name> (<email>)`;
    - `[FGS] New group: <group> by <name>`;
    - `[FGS] Warning: <n> issue(s)`;
    - `[FGS] View as…: <admin> viewing as <target>` (and `… stopped: <reason>`);
    - `[FGS] Activity: <section names, comma-separated, at most 3 then "and N more">`.

---

## The feed contract (A produces it, N consumes it)

`GET /api/activity`, headers `apikey: <admin key with ManageLogs>`, optional `since: <int>`, optional `limit: <1..1000, default 500>`.

```json
{
  "Result": "OK",
  "events": [
    {"id": 41, "at": 1792345678, "kind": "upload",
     "actor": 7, "owner": 7,
     "file": {"id": "abc", "name": "IMG_0001.HEIC", "size": 2345678, "kind": "image"},
     "group": {"id": "g1", "name": "Iceland trip"},
     "via": "", "detail": ""}
  ],
  "next": 41,
  "more": false,
  "reset": false,
  "users": [
    {"id": 7, "email": "anna@example.com", "name": "Anna Muster",
     "admin": false, "emailDownloads": true}
  ]
}
```

- **`kind`** is one of:
  - `upload`, `download`;
  - `group_created`, `group_joined`;
  - `first_signin`, `signin_blocked`;
  - `warning_disk`, `warning_preview`;
  - `viewas_start`, `viewas_end`.
- **Fields:**
  - `actor` is `0` when nobody was signed in ("someone") or for system events.
  - `owner` is the file's owner.
  - `file` and `group` are `null` when not applicable.
  - `file.kind` is `image`, `video` or `file`.
  - `via` is a download route: `group`, `collection`, `file`, `zip`, `api`.
  - `detail` is free text: the free bytes for `warning_disk`, the reason for `viewas_end`, the target's user id for View-as events (in `group`-less form: `"target=12"` / `"target=12 reason=exit"`), and the refused email for `signin_blocked`.
- **No `since` header:** `events` is empty and `next` is the newest id ("start from now"). `since: 0` means from the oldest retained row.
- **`reset: true`:** when `since` is below the oldest retained id minus one (pruned), or above the newest id (restored DB), `events` is empty and `next` is the newest id.
- **`more: true`** when `limit` cut the page; call again with `since: next`.
- `users` lists every user, every time (live has about ten).
- **Errors** follow the Gokapi API's usual `sendError` shape. A non-admin key gets 401 `AdminOnly`; a key without `ManageLogs` gets 401.

---

## Waves, worktrees, dependencies

| Wave | Tasks | Runs | Depends on |
|---|---|---|---|
| 0 | **W0** Sign-in hooks and the `people` package (controller, on `feature/part4`) | controller | – |
| 1 | **A** Activity table, recording, warnings, feed · **S1** Profiles, photos, `/settings`, `/avatar` · **K** Blocklist · **N** Notifier rewrite + replay test | parallel worktrees | W0 (N needs only the contract) |
| 1b | Controller merges A, K, S1, then N; regenerates assets; runs the Controller checks | controller | wave 1 |
| 2 | **S2** Shared UI: photos and names everywhere, user menu, Upload switch, Users page names | worktree `p4-ui` | S1, K |
| 3 | **E** `build/e2e/part4_e2e.py` + seed; the existing e2e green; notifier against the container (file sink); Playwright walkthrough | controller-dispatched, one agent | all |
| 4 | Whole-branch review → fix round → preview → Franco's go → deploy | controller | E |

---

## File map

| File | Status | Owner | Responsibility |
|---|---|---|---|
| `internal/people/people.go` | create (W0), extend (S1) | W0 → S1 | `Name`, `Initials`, `PhotoUrl`, `WantsDownloadEmails`, `HasEmail` |
| `internal/people/profiles.go` | create | S1 | Profile read/write, `SetDisplayName`, `SetPreferences`, sign-in capture |
| `internal/people/photo.go` | create | S1 | Fetch, validate and store the Google photo |
| `internal/webserver/authentication/signin.go` | create | W0 | Guard and hook lists |
| `internal/webserver/authentication/Authentication.go` | modify | W0 | Call guards before `getOrCreateUser`, hooks after the session |
| `internal/webserver/wiring.go` | create | W0, then one line each for A, S1, K | The **one** place guards and hooks are registered |
| `internal/configuration/database/provider/sqlite/activity.go` | create | A | `Activity` table and queries |
| `internal/configuration/database/provider/sqlite/profiles.go` | create | S1 | `Profiles` table |
| `internal/configuration/database/provider/sqlite/blocklist.go` | create | K | `Blocklist` table |
| `internal/configuration/database/provider/sqlite/Sqlite.go` | modify | A, S1, K (one `ensure…` line each) | Chain the new tables |
| `…/dbabstraction/DbAbstraction.go`, `…/database/Database.go`, `…/provider/redis/*` | modify | A, S1, K (own blocks) | Interface, wrappers, Redis stubs |
| `internal/models/Activity.go`, `Profile.go`, `BlockEntry.go` | create | A, S1, K | Models |
| `internal/activity/activity.go` | create | A | `Record…` helpers, disk watcher, pruning, feed assembly |
| `internal/webserver/api/ApiActivity.go`, `routing.go`, `routingParsing.go`, `openapi.json` | create/modify | A | `/api/activity` |
| `internal/webserver/fileupload/FileUpload.go:49`, `api/Api.go:539`, `storage/downloads.go`, `storage/Groups.go` (`CreateGroup`, `join`), `preview/Worker.go`, `viewas/ViewAs.go` | modify (one call each) | A | Recording points |
| `internal/blocklist/blocklist.go` | create | K | `Matches`, `Block`, `Unblock`, the guard |
| `internal/webserver/api/Api.go:1337` (`isValidApiKey`) | modify | K | Blocked owner → invalid |
| `internal/webserver/Settings.go`, `Avatar.go`, `UsersBlock.go` | create | S1, S1, K | Handlers |
| `internal/webserver/web/templates/html_settings.tmpl`, `static/js/admin_ui_settings.js` | create | S1 | Settings page |
| `internal/webserver/viewas/Guard.go` | modify | S1 | Allowlist `/settings`, `/avatar` |
| `html_users.tmpl`, `admin_ui_users.js` | modify | K, then S2 | Blocked section; name + email |
| `html_header.tmpl`, `html_file_browser.tmpl`, `html_group.tmpl`, `media.js`, `media.css`, `group.css`, `admin_ui_group.js`, `admin_ui_groupupload.js`, `admin_ui_groupactions.js`, `html_admin.tmpl`, `admin_ui_upload.js`, `FileBrowser.go`, `GroupPage.go`, `viewas.js` | modify | S2 | Shared UI |
| `deploy/notifier/gokapi_notify.py`, `deploy/notifier/test_gokapi_notify.py`, `deploy/notifier/fixtures/busy_evening.json`, `deploy/notifier/README.md`, `deploy/notifier/notify.config.example.json` | create | N | Notifier |
| `build/e2e/part4_e2e.py`, `build/e2e/seed_part4.py` | create | E | e2e |

---

### Task W0: sign-in hooks and the `people` package (controller)

**Files:**
- Create: `internal/webserver/authentication/signin.go`, `internal/webserver/authentication/signin_test.go`
- Create: `internal/people/people.go`, `internal/people/people_test.go`
- Create: `internal/webserver/wiring.go`
- Modify: `internal/webserver/authentication/Authentication.go` (`CheckOauthUserAndRedirect`, `getOrCreateUser`, `IsCorrectUsernameAndPassword`)
- Modify: `internal/webserver/GroupsCommon.go:26` and `internal/webserver/viewas/ViewAs.go:227` (delegate to `people.Name`)
- Modify: `internal/webserver/FileBrowser.go:392` (`initialsOf` delegates to `people.InitialsOfName`)

**Interfaces:**
- Produces:
  - `authentication.SignInGuard func(email string) error`
  - `authentication.SignInHook func(user models.User, claims map[string]any, created bool)`
  - `authentication.AddSignInGuard(g SignInGuard)`, `authentication.AddSignInHook(h SignInHook)`, `authentication.ResetSignInForTest()`, `authentication.GuardCountForTest() int`, `authentication.HookCountForTest() int`
  - `authentication.ErrSignInRefused`
  - `people.Name(u models.User) string`, `people.Initials(u models.User) string`, `people.InitialsOfName(name string) string`
  - `people.PhotoUrl(u models.User) string` (W0: always `""`)
  - `people.WantsDownloadEmails(userId int) bool` (W0: always `false`)
  - `people.HasEmail(u models.User) bool`
  - `webserver.wireSignIn()`, called once from `Start` before the server listens.

- [ ] **Step 1: Write the failing tests**

`internal/people/people_test.go`:

```go
//go:build test

package people

import (
	"testing"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test"
)

func TestNameFallsBackToEmailPrefix(t *testing.T) {
	test.IsEqualString(t, Name(models.User{Id: 1, Name: "anna.mueller@example.com"}), "anna.mueller")
	test.IsEqualString(t, Name(models.User{Id: 2, Name: "ben"}), "ben")
	test.IsEqualString(t, Name(models.User{Id: 3, Name: "@odd"}), "@odd")
}

func TestInitials(t *testing.T) {
	test.IsEqualString(t, InitialsOfName("anna.mueller"), "AM")
	test.IsEqualString(t, InitialsOfName("Ben"), "B")
	test.IsEqualString(t, Initials(models.User{Id: 1, Name: "carla.r@example.com"}), "CR")
}

func TestHasEmail(t *testing.T) {
	test.IsEqualBool(t, HasEmail(models.User{Name: "a@b.ch"}), true)
	test.IsEqualBool(t, HasEmail(models.User{Name: "tester"}), false)
}
```

`internal/webserver/authentication/signin_test.go` (uses the package's existing OAuth test setup; read `Authentication_test.go` for how `authSettings` and a fake `OAuthUserClaims` are made):

```go
//go:build test

package authentication

func TestSignInGuardRefusesBeforeCreate(t *testing.T) {
	ResetSignInForTest()
	defer ResetSignInForTest()
	AddSignInGuard(func(email string) error {
		if email == "blocked@example.com" {
			return ErrSignInRefused
		}
		return nil
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/oauth-callback", nil)
	info := OAuthUserInfo{Subject: "s1", Email: "blocked@example.com", ClaimsSent: testClaims{}}
	test.IsNil(t, CheckOauthUserAndRedirect(w, r, info))
	_, exists := database.GetUserByName("blocked@example.com")
	test.IsEqualBool(t, exists, false)            // refused before getOrCreateUser
	test.IsEqualBool(t, w.Code == http.StatusTemporaryRedirect && strings.Contains(w.Header().Get("Location"), "error"), true)
}

func TestSignInHookSeesCreated(t *testing.T) {
	ResetSignInForTest()
	defer ResetSignInForTest()
	var calls []bool
	AddSignInHook(func(u models.User, claims map[string]any, created bool) { calls = append(calls, created) })
	info := OAuthUserInfo{Subject: "s2", Email: "newperson@example.com", ClaimsSent: testClaims{"name": "New Person"}}
	test.IsNil(t, CheckOauthUserAndRedirect(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil), info))
	test.IsNil(t, CheckOauthUserAndRedirect(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil), info))
	test.IsEqualInt(t, len(calls), 2)
	test.IsEqualBool(t, calls[0], true)
	test.IsEqualBool(t, calls[1], false)
}

func TestPasswordLoginRunsGuards(t *testing.T) {
	ResetSignInForTest()
	defer ResetSignInForTest()
	AddSignInGuard(func(email string) error { return ErrSignInRefused })
	// existing password-login test user and CSRF helper from Authentication_test.go
	_, ok, _ := IsCorrectUsernameAndPassword(testUserName, testUserPassword, validLoginCsrf(t))
	test.IsEqualBool(t, ok, false)
}
```

`testClaims` is a `map[string]any` with `Claims(v any) error` marshalling itself, which the tests define if the package has none.

- [ ] **Step 2: Run the tests and see them fail**

Run: `build/dev/gotest.sh ./internal/people/ ./internal/webserver/authentication/ -run 'TestName|TestInitials|TestHasEmail|TestSignIn|TestPasswordLoginRunsGuards' -v`. Expected: a build failure, because the package and functions don't exist yet.

- [ ] **Step 3: Implement**

`signin.go`:

```go
package authentication

import (
	"encoding/json"
	"errors"

	"github.com/forceu/gokapi/internal/models"
)

// ErrSignInRefused is returned by a guard that refuses a sign-in. The person
// sees the same "not authorised" page as any refused OAuth user, so a
// blocklist is never revealed.
var ErrSignInRefused = errors.New("sign-in refused")

// SignInGuard may refuse a sign-in before any account is created
type SignInGuard func(email string) error

// SignInHook runs after a successful OAuth sign-in, once the session exists.
// It must return quickly: slow work (a photo fetch) goes in a goroutine.
type SignInHook func(user models.User, claims map[string]any, created bool)

var signInGuards []SignInGuard
var signInHooks []SignInHook

// AddSignInGuard registers a guard. Only webserver.wireSignIn calls it.
func AddSignInGuard(g SignInGuard) { signInGuards = append(signInGuards, g) }

// AddSignInHook registers a hook. Only webserver.wireSignIn calls it.
func AddSignInHook(h SignInHook) { signInHooks = append(signInHooks, h) }

// ResetSignInForTest empties both lists
func ResetSignInForTest() { signInGuards, signInHooks = nil, nil }

// GuardCountForTest and HookCountForTest let the wiring test prove that
// wireSignIn registered everything: a dropped line fails a test, not a ban
func GuardCountForTest() int { return len(signInGuards) }
func HookCountForTest() int  { return len(signInHooks) }

func runSignInGuards(email string) error {
	for _, g := range signInGuards {
		if err := g(email); err != nil {
			return err
		}
	}
	return nil
}

func claimsMap(c OAuthUserClaims) map[string]any {
	result := map[string]any{}
	if c == nil {
		return result
	}
	var raw json.RawMessage
	if c.Claims(&raw) != nil || json.Unmarshal(raw, &result) != nil {
		return map[string]any{}
	}
	return result
}

func runSignInHooks(user models.User, claims OAuthUserClaims, created bool) {
	m := claimsMap(claims)
	for _, h := range signInHooks {
		h(user, m, created)
	}
}
```

In `Authentication.go`:
- `getOrCreateUser` returns `(models.User, ok bool, created bool, error)`.
- `CheckOauthUserAndRedirect` becomes:

```go
	if isValidOauthUser(userInfo, groups) {
		if runSignInGuards(userInfo.Email) != nil {
			errorHandling.RedirectGenericErrorPage(w, r, errorHandling.TypeOAuthNotAuthorised)
			return nil
		}
		user, ok, created, errCreate := getOrCreateUser(userInfo.Email)
		if errCreate != nil {
			return errCreate
		}
		if ok {
			sessionmanager.CreateSession(w, true, authSettings.OAuthRecheckInterval, user.Id)
			runSignInHooks(user, userInfo.ClaimsSent, created)
			http.Redirect(w, r, PopReturnTo(w, r), http.StatusTemporaryRedirect)
			return nil
		}
	}
```

- In `IsCorrectUsernameAndPassword`, once the user is found and before the password check, add `if runSignInGuards(user.Name) != nil { return models.User{}, false, true }`.

`people.go`:

```go
// Package people is the one place a person's shown name, initials and photo
// come from (Part 4). Everything a person sees about another person goes
// through it.
package people

import (
	"strings"
	"unicode"

	"github.com/forceu/gokapi/internal/models"
)

// Name is the name shown for u: their display name, else their Google name,
// else the part of the account name before "@" (Part 2's displayName).
func Name(u models.User) string {
	if p, ok := profileOf(u.Id); ok {
		if p.DisplayName != "" {
			return p.DisplayName
		}
		if p.GoogleName != "" {
			return p.GoogleName
		}
	}
	return emailPrefix(u.Name)
}

func emailPrefix(name string) string {
	if before, _, found := strings.Cut(name, "@"); found && before != "" {
		return before
	}
	return name
}

// Initials is the avatar badge for u
func Initials(u models.User) string { return InitialsOfName(Name(u)) }

// InitialsOfName: first letters of the first and last parts, upper case
// ("anna.mueller" -> "AM", "ben" -> "B"). media.js initialsOf mirrors it.
func InitialsOfName(name string) string {
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

// HasEmail reports whether the account name is an email address
func HasEmail(u models.User) bool { return strings.Contains(u.Name, "@") }

// PhotoUrl is the relative URL of u's photo, or "" for initials
func PhotoUrl(u models.User) string { return photoUrlOf(u.Id) }

// WantsDownloadEmails is the "Email me when my files are downloaded" setting
func WantsDownloadEmails(userId int) bool { return wantsDownloadEmails(userId) }

// W0 stubs; S1 replaces them in profiles.go
type profile struct{ DisplayName, GoogleName string }

func profileOf(int) (profile, bool)  { return profile{}, false }
func photoUrlOf(int) string          { return "" }
func wantsDownloadEmails(int) bool   { return false }
```

Copy the body of `initialsOf` from `FileBrowser.go:392`, keeping its behaviour (the last part's first letter). Then make `FileBrowser.go`'s `initialsOf` return `people.InitialsOfName(name)`. `displayName` in `GroupsCommon.go` and `viewas/ViewAs.go` each become `return people.Name(u)`.

`wiring.go`:

```go
package webserver

// wireSignIn registers every sign-in guard and hook, in one place, so none
// can silently go missing (Part 4). Called once from Start.
func wireSignIn() {
	// Part 4, K: blocklist guard
	// Part 4, A: first sign-in activity
	// Part 4, S1: profile capture
}
```

Call `wireSignIn()` from `Start` in `Webserver.go`, before the routes are served.

- [ ] **Step 4: Run the tests; expect them to pass, and the full webserver suite to stay green**

Run: `build/dev/gotest.sh ./internal/people/ ./internal/webserver/... -v 2>&1 | grep -E -- '--- (FAIL|PASS): (TestName|TestInitials|TestHasEmail|TestSignIn|TestPasswordLogin|TestViewAsRoutes)|^(ok|FAIL)'`. Expected: PASS, and `ok` for every package.

- [ ] **Step 5: Commit and push**

```bash
git add internal/people internal/webserver/authentication internal/webserver/wiring.go internal/webserver/Webserver.go internal/webserver/GroupsCommon.go internal/webserver/viewas/ViewAs.go internal/webserver/FileBrowser.go
git commit -m "Part 4 W0: sign-in guards and hooks, people package for shown names"
git push -u origin feature/part4
```

---

### Task A: activity table, recording, warnings and the feed

**Files:**
- Create: `internal/models/Activity.go`, `internal/configuration/database/provider/sqlite/activity.go` + `activity_test.go`, `internal/activity/activity.go` + `activity_test.go`, `internal/webserver/api/ApiActivity.go` + `ApiActivity_test.go`
- Modify:
  - `Sqlite.go` (chain `ensureActivityTable`);
  - `DbAbstraction.go`, `Database.go`, the Redis provider (stubs);
  - `api/routing.go` (route plus `paramActivityGet`), `api/routingParsing.go` (generated), the OpenAPI JSON that `api/openapi_test.go` reads;
  - one recording call each in `fileupload/FileUpload.go:49`, `api/Api.go:539`, `storage/downloads.go`, `storage/Groups.go` (`CreateGroup`, `join`), `preview/Worker.go`, `viewas/ViewAs.go` (`LogViewAsStart`/`End` call sites);
  - `wiring.go` (one line), and `Webserver.go` `Start` (start the disk watcher and the pruner).

**Interfaces:**
- Consumes: `people.Name`, `people.WantsDownloadEmails`, `authentication.AddSignInHook` (W0).
- Produces:
  - `models.Activity{Id int64; At int64; Kind string; ActorId, OwnerId int; FileId, FileName string; FileSize int64; FileKind, GroupId, GroupName, Via, Detail string}`
  - `database.SaveActivity(a models.Activity)`, `database.GetActivitySince(since int64, limit int) []models.Activity`, `database.ActivityBounds() (oldest, newest int64)`, `database.PruneActivity(before int64)`
  - `activity.Upload(f models.File, actor models.User)`, `activity.Download(f models.File, actorId int, via string)`, `activity.GroupCreated(g models.Group, creator models.User)`, `activity.GroupJoined(g models.Group, u models.User)`, `activity.FirstSignIn(u models.User)` (as a `SignInHook` adapter: `activity.OnSignIn`), `activity.SignInBlocked(email string)`, `activity.PreviewFailed(f models.File)`, `activity.ViewAs(kind string, admin, target models.User, reason string)`, `activity.StartWatchers(dataDir string)`
  - the feed contract above.

- [ ] **Step 1: Write the failing tests**

`sqlite/activity_test.go`:

```go
func TestActivityCursor(t *testing.T) {
	instance, err := New(config)
	test.IsNil(t, err)
	defer instance.Close()
	for i := 0; i < 5; i++ {
		instance.SaveActivity(models.Activity{At: int64(100 + i), Kind: "upload", ActorId: 7, FileId: fmt.Sprintf("f%d", i)})
	}
	oldest, newest := instance.ActivityBounds()
	test.IsEqualBool(t, newest-oldest == 4, true)
	page := instance.GetActivitySince(oldest+1, 2)
	test.IsEqualInt(t, len(page), 2)
	test.IsEqualBool(t, page[0].Id == oldest+2 && page[1].Id == oldest+3, true)
	instance.PruneActivity(103)
	oldest2, _ := instance.ActivityBounds()
	test.IsEqualBool(t, oldest2 == oldest+3, true)
}
```

`internal/activity/activity_test.go`:

```go
func TestFeedStartFromNowAndReset(t *testing.T) {
	clearActivity(t)
	Upload(models.File{Id: "a1", Name: "x.jpg", SizeBytes: 10, ContentType: "image/jpeg", UserId: 7}, models.User{Id: 7})
	f := Feed(nil, 500)                 // no since header
	test.IsEqualInt(t, len(f.Events), 0)
	test.IsEqualBool(t, f.Next > 0 && !f.Reset, true)
	since := f.Next
	Upload(models.File{Id: "a2", Name: "y.mp4", SizeBytes: 20, ContentType: "video/mp4", UserId: 7}, models.User{Id: 7})
	f = Feed(&since, 500)
	test.IsEqualInt(t, len(f.Events), 1)
	test.IsEqualString(t, f.Events[0].File.Kind, "video")
	future := f.Next + 100
	f = Feed(&future, 500)
	test.IsEqualBool(t, f.Reset && len(f.Events) == 0, true)
}

func TestFeedPaging(t *testing.T) {
	clearActivity(t)
	zero := int64(0)
	for i := 0; i < 7; i++ {
		Download(models.File{Id: fmt.Sprintf("d%d", i), UserId: 7}, 0, "file")
	}
	f := Feed(&zero, 5)
	test.IsEqualBool(t, len(f.Events) == 5 && f.More, true)
	f = Feed(&f.Next, 5)
	test.IsEqualBool(t, len(f.Events) == 2 && !f.More, true)
	test.IsEqualInt(t, f.Events[0].Actor, 0) // "someone"
}

func TestDiskWarningRearms(t *testing.T) {
	clearActivity(t)
	w := newDiskWatcher(20 << 30)
	w.check(15 << 30)
	w.check(14 << 30)
	w.check(25 << 30)
	w.check(19 << 30)
	test.IsEqualInt(t, countKind(t, "warning_disk"), 2)
}

func TestDownloadGroupFromFile(t *testing.T) {
	// a file in group g1 ("Iceland trip") downloaded via "group" carries the group
}
```

`api/ApiActivity_test.go`:
- an admin key with `ManageLogs` gets 200 and the contract fields;
- an admin key without `ManageLogs` gets 401;
- a non-admin key with `ManageLogs` gets 401 `AdminOnly`;
- `since: abc` gets 400;
- `since: 0` returns the oldest page;
- `users` includes `emailDownloads` and `name` from `people.Name`.

Use the existing key fixtures in `Api_test.go` (`getRecorder(url, apikey, headers)`).

Recording tests, one per point:
- **upload** via `fileupload.Process` and the chunk complete; a duplicate deleted by `attachToGroup` records **nothing** (M6);
- **download** through `recordDownload` (HEAD and untagged record nothing, as for `Downloads`);
- **group create and join** through `storage.CreateGroup` and `JoinGroupByCode`;
- **first sign-in** through the wired hook: `created=true` records, `false` does not;
- **preview** final failure only: attempts 1 and 2 record nothing, 3 records;
- **View-as** start and end, with the reason in `detail`.

- [ ] **Step 2: Run and see them fail**

`build/dev/gotest.sh ./internal/activity/ ./internal/configuration/database/provider/sqlite/ ./internal/webserver/api/ -run 'Activity|Feed|Disk' -v` → build failure.

- [ ] **Step 3: Implement**

Table:

```go
func (p DatabaseProvider) ensureActivityTable() error {
	return p.rawSqlite(`CREATE TABLE IF NOT EXISTS "Activity" (
			"Id"	INTEGER PRIMARY KEY AUTOINCREMENT,
			"At"	INTEGER NOT NULL,
			"Kind"	TEXT NOT NULL,
			"ActorId"	INTEGER NOT NULL DEFAULT 0,
			"OwnerId"	INTEGER NOT NULL DEFAULT 0,
			"FileId"	TEXT NOT NULL DEFAULT '',
			"FileName"	TEXT NOT NULL DEFAULT '',
			"FileSize"	INTEGER NOT NULL DEFAULT 0,
			"FileKind"	TEXT NOT NULL DEFAULT '',
			"GroupId"	TEXT NOT NULL DEFAULT '',
			"GroupName"	TEXT NOT NULL DEFAULT '',
			"Via"	TEXT NOT NULL DEFAULT '',
			"Detail"	TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS "idx_Activity_At" ON "Activity" ("At");`)
}
```

`AUTOINCREMENT` is deliberate: ids are never reused after pruning, so a cursor stays meaningful.

`activity.go` essentials:
- every `Record…` writes **synchronously**, returns nothing, and is a no-op when `!database.SupportsGroups()`.
- `fileKind(contentType, name)`:
  - `image` when the content type starts with `image/`, or the extension is one of `.heic .heif .jpg .jpeg .png .gif .webp .avif .dng`;
  - `video` for `video/`, or `.mov .mp4 .m4v .avi .mkv .webm`;
  - otherwise `file`.
- **Group at write time:** `database.GetGroupIdsOfFile(f.Id)` → the first id → `GetGroup` for the name. Uploads are recorded after `attachToGroup`, so the group is already known.
- `Download(f, actorId, via)` is called from `storage.recordDownload` **after** `SaveDownload`, with the same `UserId` and `Via`. The owner is `f.UserId`.
- `Upload` is called at exactly the two `logging.LogUpload` call sites (`FileUpload.go:49`, `Api.go:539`), on the line after. This keeps M6: a duplicate that is deleted never reaches it.
- `OnSignIn(u, claims, created)` records `first_signin` when `created`, and nothing otherwise.
- `SignInBlocked(email)` is called by K's guard. A **is** the producer: K calls `activity.SignInBlocked`, so `activity.go` must export it from the start.
- `PreviewFailed(f)` is called in `preview/Worker.go` inside `fail()`, when `attempts >= maxAttempts`.
- `ViewAs("viewas_start"|"viewas_end", admin, target, reason)`: `detail` is `target=<id>` or `target=<id> reason=<reason>`; the actor is the admin.
- `StartWatchers(dataDir)` starts two goroutines, both started from `Start`:
  - a 10-minute ticker running `diskWatcher.check(free)`, where `free` comes from `helper.GetFreeSpace(dataDir)`, ignoring errors;
  - a pruner that, once at start and every 24 h, deletes rows older than 30 days.

```go
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
			record(models.Activity{Kind: "warning_disk", Detail: strconv.FormatUint(free, 10)})
		}
		return
	}
	d.low = false
}
```

`Feed(since *int64, limit int) FeedResult` follows the contract exactly:
- `limit` is clamped to 1..1000;
- query `limit+1` rows to know `more`;
- `next` is the last returned id, or the newest id when nothing was returned;
- `users` comes from `database.GetAllUsers()` mapped with `people.Name`, `people.WantsDownloadEmails` and `IsAdmin()`.

API route, in `routing.go` next to `/logs/get`:

```go
	{
		Url:           "/activity",
		ApiPerm:       models.ApiPermManageLogs,
		AdminOnly:     true,
		execution:     apiActivityGet,
		RequestParser: &paramActivityGet{},
	},
```

```go
type paramActivityGet struct {
	Since        int64 `header:"since"`
	Limit        int   `header:"limit"`
	foundHeaders map[string]bool
}

func (p *paramActivityGet) ProcessParameter(_ *http.Request) error {
	if p.Since < 0 {
		return errors.New("since must not be negative")
	}
	return nil
}
```

Check what the generator calls the int parser (`parseHeaderInt`), and that `foundHeaders["since"]` tells "absent" from "0". `apiActivityGet` passes `nil` when the header was absent. Add `/activity` to the OpenAPI JSON: GET, the two headers, and the response schema matching the contract.

`wiring.go`: `authentication.AddSignInHook(activity.OnSignIn)` under `// Part 4, A`.

- [ ] **Step 4: Run; expect PASS, with `TestViewAsRoutes` and `openapi_test` green**

`build/dev/gotest.sh ./internal/... 2>&1 | grep -E '^(ok|FAIL|---)'`. Ignore the two known memory kills.

- [ ] **Step 5: Commit (source plus `routingParsing.go`) and push**

`git commit -m "Part 4 A: activity table, recording points, warnings and /api/activity"`

**Controller checks (after merge, container on 53900):**
- create an admin API key with `ManageLogs` in the UI;
- `curl -H "apikey: K" -H "since: 0" http://127.0.0.1:53900/api/activity` shows the upload, group and join rows after `seed_part3.py`;
- `?since=1` in a query string is ignored (header only).

---

### Task S1: profiles, photos, `/settings`, `/avatar`

**Files:**
- Create:
  - `internal/models/Profile.go`;
  - `sqlite/profiles.go` + `profiles_test.go`;
  - `internal/people/profiles.go`, `internal/people/photo.go` + tests;
  - `internal/webserver/Settings.go` + `Settings_test.go`, `internal/webserver/Avatar.go` + `Avatar_test.go`;
  - `templates/html_settings.tmpl`, `static/js/admin_ui_settings.js`.
- Modify:
  - `Sqlite.go` (`ensureProfilesTable`), `DbAbstraction.go`, `Database.go`, Redis stubs;
  - `internal/people/people.go` (replace the three W0 stubs);
  - `viewas/Guard.go` (allowlist `/settings`, `/avatar`);
  - `Webserver.go` (route block `// Part 4, S1`), `wiring.go` (one line).
- Test: as listed, plus `ViewAsRoutes_test.go`, which must stay green unchanged.

**Interfaces:**
- Consumes: `people.*` (W0), `authentication.AddSignInHook`, `storage.UserSharesGroupWithFile`-style helpers.
- Produces:
  - `models.Profile{UserId int; DisplayName, GoogleName string; Photo []byte; PhotoType string; PhotoUpdated int64; UseInitials, EmailDownloads bool}`;
  - `database.GetProfile(userId int) (models.Profile, bool)`, `database.SaveProfile(p models.Profile)`;
  - `people.SetDisplayName(userId int, name string) error`, `people.ErrDisplayName`;
  - `people.SetPreferences(userId int, emailDownloads, useInitials *bool)`;
  - `people.OnSignIn` (a `SignInHook`: stores `GoogleName` from `claims["name"]`, then fetches `claims["picture"]` in a goroutine when the photo is over a day old);
  - `people.PhotoOf(userId int) (data []byte, contentType string, updated int64, ok bool)`;
  - `storage.UsersShareGroup(a, b int) bool`;
  - routes `GET /settings` (page), `POST /settings/save` (JSON `{displayName?: string, emailDownloads?: bool, useInitials?: bool}` → `{Result:"OK", Name: <shown name>}`), `GET /avatar?id=N&v=T`;
  - `POST /seed/photo` (admin only, JSON `{userId, jpegBase64}`).
    - It is registered **only** when the environment has `GOKAPI_E2E_SEED=1`, read once in `Start`, so on live the path does not exist (404).
    - It exists because e2e and the preview run internal auth, where no Google photo can arrive, and the image has no `sqlite3` to seed one.
    - It stores through the same `storePhoto` path, with the same sniffing and size checks.
    - It is refused during View as by default.
    - Test both states: without the variable the route answers 404; with it, a non-admin gets 403 and an admin stores the photo.

- [ ] **Step 1: Write the failing tests**

`people/profiles_test.go`:

```go
func TestShownNameOrder(t *testing.T) {
	u := models.User{Id: 501, Name: "anna.m@example.com"}
	test.IsEqualString(t, Name(u), "anna.m")
	OnSignIn(u, map[string]any{"name": "Anna Muster"}, true)
	test.IsEqualString(t, Name(u), "Anna Muster")
	test.IsNil(t, SetDisplayName(u.Id, "  Annie  "))
	test.IsEqualString(t, Name(u), "Annie")
	test.IsNil(t, SetDisplayName(u.Id, ""))
	test.IsEqualString(t, Name(u), "Anna Muster")
	test.IsNotNil(t, SetDisplayName(u.Id, strings.Repeat("x", 41)))
	test.IsNotNil(t, SetDisplayName(u.Id, "bad\u0000name"))
	OnSignIn(u, map[string]any{"name": "Anna M. Muster"}, false) // Google name refreshes, display name untouched
	_ = SetDisplayName(u.Id, "Annie")
	test.IsEqualString(t, Name(u), "Annie")
}

func TestPreferencesPartialUpdate(t *testing.T) {
	on, off := true, false
	SetPreferences(502, &on, nil)
	test.IsEqualBool(t, WantsDownloadEmails(502), true)
	SetPreferences(502, nil, &on)
	test.IsEqualBool(t, WantsDownloadEmails(502), true) // untouched
	SetPreferences(502, &off, nil)
	test.IsEqualBool(t, WantsDownloadEmails(502), false)
	test.IsEqualBool(t, WantsDownloadEmails(9999), false) // default off
}
```

`people/photo_test.go`. `photoClient` and `photoHostOk` are package vars, so the tests can point them at an `httptest.NewTLSServer`:
- a JPEG under 512 KB is stored, and `PhotoUrl` becomes `./avatar?id=503&v=<updated>`;
- an `http://` URL, a host not ending in `.googleusercontent.com`, a body over 512 KB, `text/html` sniffed, or a timeout each store nothing and keep the old photo;
- a second `OnSignIn` within 24 h does not fetch again (count requests);
- `UseInitials` true makes `PhotoUrl` return `""`.

`Avatar_test.go`:
- yourself gets 200 with `Content-Type: image/jpeg`, `X-Content-Type-Options: nosniff` and `Cache-Control: private, max-age=31536000, immutable`;
- the admin gets 200;
- a person sharing a group gets 200;
- a stranger gets 404, never 403, so existence doesn't leak;
- no photo gets 404, and a bad id gets 404;
- no session redirects to login.

`Settings_test.go`:
- GET renders the shown name, the two switches' states, and the no-email note for `tester`;
- POST JSON with partial fields updates only those;
- 41 characters gets 400 with `people.ErrDisplayName`'s text;
- GET to `/settings/save` gets 405, and a form body gets 415 (D20, `groupRequest` helpers).

- [ ] **Step 2: Run and see them fail**

`build/dev/gotest.sh ./internal/people/ ./internal/webserver/ -run 'Shown|Preferences|Photo|Avatar|Settings' -v`

- [ ] **Step 3: Implement**

Table:

```go
func (p DatabaseProvider) ensureProfilesTable() error {
	return p.rawSqlite(`CREATE TABLE IF NOT EXISTS "Profiles" (
			"UserId"	INTEGER PRIMARY KEY,
			"DisplayName"	TEXT NOT NULL DEFAULT '',
			"GoogleName"	TEXT NOT NULL DEFAULT '',
			"Photo"	BLOB,
			"PhotoType"	TEXT NOT NULL DEFAULT '',
			"PhotoUpdated"	INTEGER NOT NULL DEFAULT 0,
			"UseInitials"	INTEGER NOT NULL DEFAULT 0,
			"EmailDownloads"	INTEGER NOT NULL DEFAULT 0
		);`)
}
```

Deleting a user also deletes their profile row: hook it where `DeleteUser` is wrapped in `Database.go`.

`people/profiles.go` replaces the W0 stubs:
- `profileOf` reads `database.GetProfile` (only when `database.SupportsGroups()`);
- `photoUrlOf` returns `fmt.Sprintf("./avatar?id=%d&v=%d", id, p.PhotoUpdated)` when there is a photo and `!p.UseInitials`.

Validation:

```go
var ErrDisplayName = errors.New("A display name is at most 40 characters, with no control characters.")

func cleanDisplayName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > 40 {
		return "", ErrDisplayName
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", ErrDisplayName
		}
	}
	return s, nil
}
```

`photo.go`:

```go
var photoClient = &http.Client{Timeout: 5 * time.Second}
var photoHostOk = func(u *url.URL) bool {
	return u.Scheme == "https" && strings.HasSuffix(u.Hostname(), ".googleusercontent.com")
}

const maxPhotoBytes = 512 << 10

func fetchPhoto(userId int, raw string) {
	u, err := url.Parse(raw)
	if err != nil || !photoHostOk(u) {
		return
	}
	resp, err := photoClient.Get(u.String())
	if err != nil {
		fmt.Println("people: photo fetch failed for user", userId, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxPhotoBytes+1))
	if err != nil || len(data) > maxPhotoBytes {
		return
	}
	ct := http.DetectContentType(data)
	if ct != "image/jpeg" && ct != "image/png" && ct != "image/webp" {
		return
	}
	storePhoto(userId, data, ct, time.Now().Unix())
}
```

Redirects: set `photoClient.CheckRedirect` so a redirect target must also pass `photoHostOk`.

`OnSignIn` does three things:
- stores `GoogleName`: the `name` claim, trimmed, through `cleanDisplayName`, and ignored if invalid;
- if the `picture` claim is a string and `time.Now().Unix()-PhotoUpdated > 86400`, starts `go fetchPhoto(...)`;
- for tests, `fetchPhotoSync` is a package var so the test can wait.

Settings page:
- Model `showGroups`: `SettingsView{AdminView; Name, DisplayName, Email string; HasEmail, EmailDownloads, UseInitials, HasPhoto bool; PhotoUrl string}`, rendered through template `settings_page`.
- The layout follows `html_groups.tmpl`'s card style, with the exact copy strings from Global Constraints.
- `admin_ui_settings.js` posts JSON with `fetch`, shows the toast `Saved`, and updates the name text with `textContent`.
- The email switch is disabled with the no-email note when `!HasEmail`.
- During View as the page renders the **target's** settings. Its controls are hidden by the existing View-as display logic: add `data-viewas-hide` or whatever the Part 3 mechanism uses; read `viewas.js`.

Routes (`// Part 4, S1: settings and avatars`):

```go
	mux.HandleFunc("/settings", requireLogin(showSettings, false, false))
	mux.HandleFunc("/settings/save", requireLogin(saveSettings, true, false))
	mux.HandleFunc("/avatar", requireLogin(serveAvatar, false, false))
```

Copy the exact `requireLogin` argument pattern of the neighbouring Part 3 routes. `Guard.go` `allowedPaths` gets `"/settings": true, "/avatar": true`.

`storage.UsersShareGroup(a, b int) bool` builds the set of group ids from `database.GetMembershipsOfUser(a)` and checks whether any of `b`'s memberships is in it. Put it in `storage/Groups.go` next to `UserSharesGroupWithFile`.

`wiring.go`: `authentication.AddSignInHook(people.OnSignIn)` under `// Part 4, S1`.

- [ ] **Step 4: Run the whole suite and expect it green** (`TestViewAsRoutes` included)

- [ ] **Step 5: Commit and push**: `Part 4 S1: profiles, Google photo, /settings and /avatar`

**Controller checks:**
- log in as tester, open `/settings`, set a display name, and check that the group page, which still renders through `displayName`, shows it;
- `/avatar?id=<tester>` gives 404, since there is no photo under internal auth;
- under View as, `/settings` renders read-only and a POST to `/settings/save` gets 403.

---

### Task K: blocklist

**Files:**
- Create: `internal/models/BlockEntry.go`, `sqlite/blocklist.go` + test, `internal/blocklist/blocklist.go` + `blocklist_test.go`, `internal/webserver/UsersBlock.go` + `UsersBlock_test.go`
- Modify:
  - `Sqlite.go`, `DbAbstraction.go`, `Database.go`, Redis stubs;
  - `api/Api.go` `isValidApiKey`: a blocked owner is invalid;
  - `wiring.go` (one line), `Webserver.go` (block `// Part 4, K`);
  - `html_users.tmpl` (a `Blocked` section under the table; `Block this person` in each row's `⋯` menu), `admin_ui_users.js`.

**Interfaces:**
- Consumes: `authentication.AddSignInGuard`, `authentication.ErrSignInRefused` (W0); `activity.SignInBlocked` (A — until A merges, K's worktree adds a local `var onBlocked = func(string) {}` in `blocklist.go`, and the controller swaps in `activity.SignInBlocked` at merge; note it in the commit).
- Produces:
  - `blocklist.Matches(email string) bool`, `blocklist.Block(entry string, by models.User) error`, `blocklist.Unblock(entry string)`, `blocklist.List() []models.BlockEntry`, `blocklist.Guard(email string) error`;
  - errors `blocklist.ErrAdmin` ("You cannot block an admin.") and `blocklist.ErrEntry`;
  - `POST /users/block {entry}`, `POST /users/unblock {entry}` (admin only, JSON, 405/415 otherwise), `GET`-free; the list is rendered into `/users`.

- [ ] **Step 1: Write the failing tests**

```go
func TestMatches(t *testing.T) {
	reset(t)
	must(t, Block("Spammer@Example.com", admin))
	must(t, Block("@gmail.com", admin))
	cases := map[string]bool{
		"spammer@example.com": true, "SPAMMER@EXAMPLE.COM": true, "other@example.com": false,
		"x@gmail.com": true, "x@GMAIL.com": true, "x@evilgmail.com": false, "x@mail.gmail.com": false,
		"gmail.com": false, "": false,
	}
	for email, want := range cases {
		test.IsEqualBool(t, Matches(email), want)
	}
}

func TestBlockRefusesAdminsAndBadEntries(t *testing.T) {
	reset(t)
	test.IsEqualBool(t, errors.Is(Block(adminUser.Name, admin), ErrAdmin), true)
	test.IsEqualBool(t, errors.Is(Block("@"+domainOf(adminUser.Name), admin), ErrAdmin), true)
	for _, bad := range []string{"", "nobody", "@", "a@", "@@x.com", "a b@x.com"} {
		test.IsEqualBool(t, errors.Is(Block(bad, admin), ErrEntry), true)
	}
}

func TestBlockEndsSessionsAndKeys(t *testing.T) {
	// user u with two sessions and an API key
	must(t, Block(u.Name, admin))
	test.IsEqualInt(t, countSessions(u.Id), 0)
	// isValidApiKey(u's key) is false: tested in api package (Api_test.go addition)
}
```

The wiring test lives in `internal/webserver/UsersBlock_test.go`, so it goes through the **real** `wireSignIn`:

```go
func TestBlockedOauthSignInIsRefusedThroughWiring(t *testing.T) {
	authentication.ResetSignInForTest()
	wireSignIn()
	must(t, blocklist.Block("@blocked.example", adminUser))
	info := authentication.OAuthUserInfo{Subject: "s", Email: "New@Blocked.Example", ClaimsSent: testClaims{}}
	test.IsNil(t, authentication.CheckOauthUserAndRedirect(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil), info))
	_, created := database.GetUserByName("New@Blocked.Example")
	test.IsEqualBool(t, created, false)
}
```

Also test that `wireSignIn()` registers exactly one guard (`len(signInGuards)` via an exported test helper `authentication.GuardCountForTest()`). A future refactor that drops the line then fails loudly, closed and visible.

Handler tests:
- a non-admin gets 403;
- GET gets 405, and a form body gets 415;
- blocking an admin gets 400 with `You cannot block an admin.`;
- blocking twice is idempotent;
- unblock removes the entry.

- [ ] **Step 2: Run and see them fail**

- [ ] **Step 3: Implement**

```go
func (p DatabaseProvider) ensureBlocklistTable() error {
	return p.rawSqlite(`CREATE TABLE IF NOT EXISTS "Blocklist" (
			"Entry"	TEXT NOT NULL PRIMARY KEY,
			"Added"	INTEGER NOT NULL,
			"AddedBy"	INTEGER NOT NULL
		);`)
}
```

```go
// normalise lower-cases and checks an entry: an exact email or "@domain"
func normalise(entry string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(entry))
	if strings.ContainsAny(e, " \t\r\n") || strings.Count(e, "@") != 1 {
		return "", ErrEntry
	}
	local, domain, _ := strings.Cut(e, "@")
	if domain == "" || !strings.Contains(domain, ".") {
		return "", ErrEntry
	}
	_ = local // "" means a domain entry
	return e, nil
}

// Matches reports whether email is blocked, exactly or by its whole domain
func Matches(email string) bool {
	e := strings.ToLower(strings.TrimSpace(email))
	_, domain, found := strings.Cut(e, "@")
	if !found || domain == "" {
		return false
	}
	return database.IsBlocked(e) || database.IsBlocked("@"+domain)
}
```

- `Block` normalises the entry, then refuses it (`ErrAdmin`) if any user with `IsAdmin()` matches it.
- It saves the entry, then calls `database.DeleteAllSessionsByUser(u.Id)` for **every** existing user it matches.
- `Guard(email)` returns `authentication.ErrSignInRefused` and calls `onBlocked(email)` when `Matches`.
- In `isValidApiKey`, after the owner is found, add `if blocklist.Matches(user.Name) { return models.User{}, models.ApiKey{}, false }`. Check for an import cycle: `blocklist` imports `database`, `models` and `authentication` (for the error). If `api → blocklist → authentication` cycles, move `ErrSignInRefused` to `internal/models` and say so.
- `wiring.go`: `authentication.AddSignInGuard(blocklist.Guard)` under `// Part 4, K`.
- Users page:
  - a `Blocked` card under the users table: an input, a `Block` button, and one row per entry with `Unblock`;
  - `Block this person` in each non-admin row's `⋯` menu, behind the exact confirm text;
  - all DOM through `textContent`; reload the section after each call.

- [ ] **Step 4: Run the whole suite and expect it green**

- [ ] **Step 5: Commit and push**: `Part 4 K: blocklist checked at sign-in, password login and API keys`

**Controller checks:**
- block a seeded user and check that their open session gets the login page on the next request;
- check that their API key now returns 401;
- check that unblocking lets a password login work again.

---

### Task N: the notifier, rewritten, with a replay test

**Files:**
- Create:
  - `deploy/notifier/gokapi_notify.py`;
  - `deploy/notifier/test_gokapi_notify.py`;
  - `deploy/notifier/fixtures/busy_evening.json`;
  - `deploy/notifier/notify.config.example.json`;
  - `deploy/notifier/README.md` (what it does, config keys, cutover, the launchd plist it runs under).
- Do **not** touch `~/apps/gokapi`. Read `~/apps/gokapi/gokapi-notify.py` and `notify.log` for the existing behaviour and timings only.

**Interfaces:**
- Consumes: the feed contract above (no Go).
- Produces the CLI: `gokapi_notify.py [--once] [--test] [--from-start] [--config PATH]`.
- Config keys:

  | Key | Default |
  |---|---|
  | `api_url` | `http://127.0.0.1:53842/api/activity` |
  | `api_key` | required |
  | `site_url` | `https://share.franco.international` |
  | `smtp_user`, `smtp_app_password`, `mail_to` | required |
  | `redirect_all_to` | optional |
  | `deliver` | `smtp`, or `dir:<path>` writes `.eml` files |
  | `poll_seconds` | 60 |
  | `quiet_minutes` | 60 |
  | `max_hold_minutes` | 180 |
  | `friends_digest_time` | `19:00` |
  | `timezone` | `Europe/Zurich` |
  | `state_path` | `.notify-state-v2.json` next to the config |
  | `max_friend_emails_per_day` | 100 |

- Python **3.9**: no `X | None`, no `match`, stdlib only. Run tests with `/usr/bin/python3 -m unittest`.

**Design.** All decisions are made in pure functions; only `main()` touches the network, the clock and the disk.

```python
def step(state, feed, now_utc, cfg):
    """Fold one feed page into state; return (state, emails).

    state  -- dict, JSON-serialisable, persisted by the caller after every step
    feed   -- the parsed /api/activity answer (events, next, reset, users)
    emails -- list of dicts {to, subject, body, kind}; nothing is sent here
    """
```

State keys:
- `cursor`;
- `held` (Franco's summary items) and `held_first_at` / `held_last_at`;
- `friends` (`{owner_id: [items]}`) and `friend_last_digest_date` (local date string);
- `outbox` (emails not yet sent, capped at 200, oldest dropped and counted in `outbox_dropped`);
- `failing` (`{"feed": since_ts or None, "smtp": since_ts or None}`);
- `next_send_at` (backoff).

Rules `step` implements:
1. `feed.reset` → set `cursor = feed.next` and keep everything held. Log `feed reset: cursor <old> -> <new>` once.
2. **Immediate events** go straight into **one** email per step, subject chosen by priority: View-as, then warning, then new person, then new group, joined with `; ` when several.
   - Immediate kinds: `first_signin`, `group_created` by a non-admin, `warning_disk`, `warning_preview`, `viewas_start`, `viewas_end`.
   - View-as events are always immediate, whoever the actor.
3. **Held for Franco:** `upload`, `download`, `group_joined` and `signin_blocked` whose actor is not an admin. A download by a non-admin of an admin's file counts. Each sets `held_first_at` if empty and always sets `held_last_at = event.at`.
4. **Friends:** every `download` where `owner != actor`, the owner's user has `emailDownloads` true and an email, and the owner is not an admin, is appended to `friends[owner]`. Franco's own downloads of a friend's files count for that friend.
5. **Summary due:** when `held` is non-empty and either `now - held_last_at >= quiet_minutes*60` or `now - held_first_at >= max_hold_minutes*60`. Build one summary (format below), clear `held`, `held_first_at` and `held_last_at`.
6. **Friends' digests:** once the local time in `timezone` is at or after `friends_digest_time` and `friend_last_digest_date != today_local`, then for every owner with items whose setting is **still** on, build one email. Then clear all `friends` lists (an owner who switched off loses theirs) and set `friend_last_digest_date = today_local`. At most `max_friend_emails_per_day`; the rest stay for tomorrow.
7. Append the emails to the outbox in the order immediate, summary, friends; advance `cursor = feed.next`. The caller persists state **before** sending, then sends from the outbox and removes each email only after it sent. A crash between the two re-sends at most that step's emails, and never loses events.

Summary format:
- Subject: `[FGS] Activity: <sections>`.
- Body: one paragraph per section.
- Emails are **plain text** with no markdown, since the spec's bold would show as literal `**` on a phone. Each section is its name on its own line, the clauses on the next line, and a blank line between sections.
- A **group** section reads `Iceland trip` then `Anna uploaded 143 photos, 6 videos (2.1 GB) · Ben downloaded 87 · Carla joined`:
  - per person, in first-activity order;
  - uploads counted by `file.kind` (photos/videos/files), with the size total in SI units with one decimal (`helper.ByteCountSI` style: `2.1 GB`);
  - downloads are distinct files per person;
  - a person who did both gets both clauses: `Anna uploaded 12 photos, downloaded 30`.
- An **owner-files** section (Your files, Anna's files) reads per file: `"CV.pdf" downloaded 3 times (someone, Ben, via link)`.
  - The people are distinct shown names in first-seen order, with `someone` for actor 0.
  - The via labels are `via link` for `file` and `collection`, `via zip` for `zip`, `via API` for `api`.
  - Uploads outside a group get one line per person: `Anna uploaded 4 files (12.3 MB)`.
- `signin_blocked` gets a last section, `Blocked sign-ins` then `x@spam.example (2 attempts)`.
- People are named with the feed's `users[].name`. An unknown actor id is `someone (#<id>)`.

Friend email:
- Subject: `Your files were downloaded`.
- One line per group, then one line for files outside groups:
  - `Ben and Carla downloaded 41 of your photos from Iceland trip.`
  - `Ben, Carla and 3 others downloaded …` when there are more than three people.
  - `of your videos` / `of your files` when the files were not all photos.
  - `Someone downloaded "CV.pdf" 3 times.` for a single file outside groups; for several: `Someone and Ben downloaded 5 of your files.`
- Footer: as in Global Constraints.

**Send and failure handling** (in `deliver(cfg, email)` and `main`):
- `deliver` is the **only** place mail leaves:
  - it rewrites `To` (and every envelope recipient) to `redirect_all_to` when that is set;
  - it refuses (raises `UnsafeConfig`) when `api_url` is not `http://127.0.0.1:53842/api/activity` and `redirect_all_to` is unset.
  - `deliver: dir:<path>` writes an `.eml` file instead of SMTP.
- **Feed unreachable** (URLError, non-200, bad JSON): log once when the failure starts (`feed unreachable since …: <error>`), stay quiet while it lasts, log once when it recovers. Never email about it.
- **SMTP rejected or failing:** keep the outbox, log once per failure streak, back off `next_send_at` by 1, 5, 15, then 60 minutes (capped). Never email about it.
- Polling continues during SMTP failure, so new events keep folding into the held state and the outbox.
- `--from-start` sends `since: 0` on the first call when the state has no cursor. Without it, no cursor means no `since` header ("start from now").

**The fixture** `fixtures/busy_evening.json` is synthetic (R-c): `{"users": [...], "pages": [{"at_poll": "<ISO UTC>", "events": [...]}]}`. One poll per minute from 18:30 to 01:30 Zürich time on an invented date, with ~600 events:
- **Two invariants the expected times depend on:** no held event of any kind before 18:40 (the pre-19:00 friend downloads fall in 18:40–18:59), and every held event after 21:40 falls at or before 22:44, apart from admin activity, which is never held.
- 18:40–21:55 continuous group uploads, no gap ≥ 60 min — the **3-hour cap** must fire at 21:40 (first held 18:40);
- 22:00 a `group_created` by a non-admin — immediate;
- 22:02–22:44 bursts of downloads by two friends, modelled on 2026-10-18 22:02–22:44 in `notify.log`;
- 22:44 then nothing — the **quiet rule** fires at 23:44;
- 19:05 and 23:10 `first_signin` — immediate;
- 20:15 one `warning_disk` and three `warning_preview` in the same poll — **one** email;
- 21:00 `viewas_start` by the admin, 21:07 `viewas_end` with `reason=exit` — two immediate emails although the actor is an admin;
- admin uploads at 19:30 and 23:30 — never in a summary, and the 23:30 one does not reset the quiet timer;
- friend downloads of a friend's files before 19:00 and after 19:00 — the first digest at 19:00 holds only the earlier ones; the later ones wait for **the next day's** 19:00. The fixture continues to 19:01 the next day with an empty middle, so the digest crosses midnight and stays at most one a day.

`test_gokapi_notify.py` (`unittest`):

```python
class ReplayBusyEvening(unittest.TestCase):
    def replay(self, crash_at=None, smtp_fail=None):
        """Drive step() over every fixture page with a fake clock and a fake sender.
        crash_at: ISO time after which state is round-tripped through JSON (restart).
        smtp_fail: (start, end) ISO window in which every send raises SMTPAuthenticationError."""
        ...
        return sent  # list of (at, to, subject, body)

    def test_exact_immediate_emails(self):
        sent = self.replay()
        immediate = [(at, subj) for at, to, subj, _ in sent if not subj.startswith("[FGS] Activity") and to == FRANCO]
        self.assertEqual(immediate, EXPECTED_IMMEDIATE)   # a literal list of (time, subject) in the test

    def test_summaries_three_hour_cap_and_quiet_hour(self):
        sent = self.replay()
        summaries = [at for at, to, subj, _ in sent if subj.startswith("[FGS] Activity")]
        self.assertEqual(summaries, ["2026-03-14T21:40:00+01:00", "2026-03-14T23:44:00+01:00"])

    def test_summary_body_groups_by_group_and_person(self):
        body = self.summary_bodies()[1]
        self.assertIn("Iceland trip\nAnna uploaded ", body)
        self.assertIn("Ben downloaded 87", body)
        self.assertIn("Your files\n\"CV.pdf\" downloaded 3 times (someone, via link)", body)
        self.assertNotIn("**", body)

    def test_admin_activity_never_summarised(self): ...
    def test_restart_mid_replay_no_resend_no_skip(self):
        self.assertEqual(self.replay(crash_at="2026-03-14T22:30:00+01:00"), self.replay())

    def test_smtp_failure_keeps_outbox_and_never_mails_itself(self):
        sent = self.replay(smtp_fail=("2026-03-14T22:00:00+01:00", "2026-03-14T22:40:00+01:00"))
        self.assertEqual(sorted(s for _, _, s, _ in sent), sorted(s for _, _, s, _ in self.replay()))
        self.assertFalse(any("notifier" in s.lower() or "error" in s.lower() for _, _, s, _ in sent))

    def test_friend_digest_once_a_day_across_midnight(self):
        friend = [(at, to) for at, to, subj, _ in self.replay() if subj == "Your files were downloaded"]
        self.assertEqual(friend, EXPECTED_FRIEND_DIGESTS)  # one at 19:00 day 1, one at 19:00 day 2, nobody else

    def test_friend_digest_skipped_when_setting_turned_off(self): ...
    def test_someone_for_anonymous(self): ...

class Delivery(unittest.TestCase):
    def test_redirect_rewrites_every_recipient(self): ...
    def test_refuses_non_live_api_without_redirect(self): ...
    def test_dir_sink_writes_eml(self): ...

class Feed(unittest.TestCase):
    def test_reset_moves_cursor_keeps_held(self): ...
    def test_from_start_sends_since_zero_once(self): ...
    def test_feed_failure_logged_once_per_streak(self): ...
```

`EXPECTED_IMMEDIATE` and `EXPECTED_FRIEND_DIGESTS` are written into the test by hand from the fixture's design above, **before** the implementation exists. They must not be generated by running the code.

- [ ] **Step 1:** write the fixture generator inline in the test module (a deterministic function, seeded), plus the expected lists. Write every test.
- [ ] **Step 2:** `cd deploy/notifier && /usr/bin/python3 -m unittest -v` → fails (module missing).
- [ ] **Step 3:** implement `gokapi_notify.py`. Keep the old script's `log()` format and its `--test` behaviour, now through `deliver`.
- [ ] **Step 4:** `/usr/bin/python3 -m unittest -v` → every test passes. Also run `/usr/bin/python3 -We -c "import gokapi_notify"` with no warnings.
- [ ] **Step 5:** commit and push: `Part 4 N: notifier reads /api/activity; replay-tested summaries and digests`.

---

### Task S2: names and photos everywhere, user menu, Upload switch

**Files (all S2-owned):**
- Go: `FileBrowser.go` (`browserItem` gets `UploaderPhoto string`; `Initials` from `people.Initials`), `GroupPage.go` (members get `PhotoUrl`), `Users.go` (row gets `ShownName`), `html_header.tmpl` data (`ActiveUserShownName`, `ActiveUserEmail` in `AdminView`).
- Templates: `html_header.tmpl` (user menu), `html_file_browser.tmpl` (`.fgs-initials` → `<img>` when there is a photo), `html_group.tmpl` (avatar stack and members list), `html_users.tmpl` (shown name + email), `html_admin.tmpl` (Upload switch).
- **Your own name on the client:** `media.js:2187-2190` builds your own name by stripping "@" from `data-user-name` (`html_file_browser.tmpl:17`). Feed it the shown name and your photo URL (`data-user-photo`) instead, so a freshly uploaded tile already shows `Anna Muster`, not `anna.m`.
- **Other raw-name spots, each checked:** `html_files.tmpl:5` (PersonName), `html_collection.tmpl:8/:29` (OwnerName), `html_collections_admin.tmpl:41/:108`, `html_api.tmpl:112`, `html_uploadrequest.tmpl:96`. Each either goes through `people.Name` or stays raw on purpose (for example an admin-only page showing the account). Record which in the rulings.
- JS/CSS: `media.js` (`initialsOf` and the live-add badge, which use `UploaderPhoto`), `admin_ui_group.js:316`, `admin_ui_groupupload.js:337`, `admin_ui_groupactions.js:75`, `admin_ui_users.js:334`, `admin_ui_upload.js` (switch), `viewas.js` (decode `+`), `media.css` and `group.css` (round 100% `object-fit: cover` images at the badge sizes).

**Interfaces:**
- Consumes: `people.Name`, `people.Initials`, `people.PhotoUrl`, `people.HasEmail`, `people.WantsDownloadEmails`, and `POST /settings/save` (S1).
- Produces: item JSON field `UploaderPhoto` (feeds and pages), member field `PhotoUrl`.

- [ ] **Step 1: Write the failing tests**
  - **Go:**
    - an item whose uploader has a profile photo carries `UploaderPhoto="./avatar?id=…"`; one with `UseInitials` carries `""` and its `Initials`;
    - the group page members show the display name;
    - the Users page row shows the shown name and the email;
    - the header for a signed-in user renders the shown name, the email and a `./settings` link above Logout;
    - the feed JSON (`/files/list`, `/gl`) includes `UploaderPhoto`.
  - **JS:** extend `build/dev/browser_view_test.mjs` or add a check in E. Live-added items render an `<img>` when `UploaderPhoto` is set and the initials otherwise, built with `textContent` and `setAttribute`. A display name containing `<b>x</b>` renders as text.
- [ ] **Step 2:** run them and see them fail.
- [ ] **Step 3:** implement.
  - Avatars: `<img class="fgs-avatar-img" src="{{.PhotoUrl}}" alt="" loading="lazy" referrerpolicy="no-referrer" width=… height=…>` when the photo URL is set, else today's initials span. The image has an `onerror` fallback that swaps back to the initials (set by JS, never inline with a name).
  - **User menu:** the shown name; then `<small class="text-muted">email</small>` on a second line; `Settings` (`href="./settings"`); a divider; `Logout`.
  - **Upload page:** a third switch in the same flex row, after "Combine each upload into one link":
    - `id="fgs-email-downloads"`, label `Email me when my files are downloaded`, checked from `.EmailDownloads`;
    - `onchange` posts `{emailDownloads: checked}` to `./settings/save` and reverts the switch on failure;
    - hidden when the account has no email.
  - **viewas.js:** `decodeURIComponent(value.replace(/\+/g, " "))`.
- [ ] **Step 4:** run the whole Go suite and the existing e2e on 53900 (only S2 runs in this wave, so it may use the container and Playwright).
  - Expected: `groups_e2e.py` 91, `collections_e2e.py` 56 and `part3_e2e.py` 101 all pass. Some of their assertions may check names: the shown name falls back to the email prefix when there is no profile, so they should pass unchanged. If one fails, report it rather than editing the e2e.
- [ ] **Step 5:** commit (source only) and push: `Part 4 S2: display names and Google photos everywhere; Settings in the user menu; Upload switch`.

---

### Task E: e2e for Part 4, then the walkthrough

**Files:** create `build/e2e/seed_part4.py`, `build/e2e/part4_e2e.py`.

- **`seed_part4.py`** runs `seed_part3.py`, then:
  - gives users A and B display names (`Anna Muster`, `Ben Keller`) via `/settings/save`;
  - gives A a 64×64 JPEG photo through S1's `POST /seed/photo`, which exists only when the container runs with `GOKAPI_E2E_SEED=1`;
  - prints one JSON line with the ids, passwords, the admin API key (created through `/api/auth/create` with `ManageLogs`), and the group.
- **`part4_e2e.py`** checks, against a fresh container started with `-e GOKAPI_E2E_SEED=1`:
  - **Feed:**
    - no-`since` start-from-now;
    - `since: 0` returns the seed's uploads, the group creation and B's join, with group names;
    - downloads by B and by an anonymous `/d` request appear with `actor` B and `0`;
    - `limit` paging;
    - a non-admin key gets 401, and a key without `ManageLogs` gets 401;
    - a `?since=` query string does not route (400 `InvalidUrl`).
  - **Settings:**
    - page copy strings;
    - display name round-trip; 41 characters gets 400;
    - email switch on `/settings` and the Upload page reflect each other;
    - tester sees the no-email note;
    - GET to `/settings/save` gets 405.
  - **Names:** B's view of the group page shows `Anna Muster` and A's photo `<img>` with an `/avatar` URL that B can fetch (200, `image/jpeg`).
  - **Avatar privacy:** a third user C sharing no group gets 404 for A's avatar.
  - **Blocklist:**
    - admin blocks `@<B's domain>` → B's session is gone, B's API key gets 401, B's password login fails;
    - blocking the admin gets 400 with the exact refusal text;
    - unblock lets B log in again;
    - a `signin_blocked` row appears in the feed for the refused password login. A only records OAuth refusals through the guard, and password refusals also go through `runSignInGuards`, so both record.
  - **View as:**
    - start and end produce `viewas_start` and `viewas_end` rows;
    - `/settings` renders read-only under View as, and a POST to `/settings/save` gets 403.
  - **Notifier against the container:**
    - run `deploy/notifier/gokapi_notify.py --once --from-start` with a temp config: `api_url` on 53900, `deliver: dir:~/src/Gokapi-wt/e2e-mail`, `redirect_all_to: franco@goxcoworld.com`, `quiet_minutes: 0`;
    - assert an `.eml` exists for the group-created event and one summary;
    - assert every `.eml`'s `To` is the redirect.
- **Run:** a fresh container (`-e GOKAPI_PORT=53900 -p 127.0.0.1:53900:53900`, fresh volumes, `setup_local.py`, restart). Run `part4_e2e.py`, then `groups_e2e.py`, `collections_e2e.py` and `part3_e2e.py` on the same image. Remove the container and volumes afterwards.
- **Playwright walkthrough** at 1280×800 and 390×844, saved under `build/e2e/screens-part4/`:
  - the user menu with name, email, Settings, Logout;
  - the Settings page;
  - the Upload page switch;
  - the group page with A's photo and B's initials;
  - a tile badge with the photo;
  - the Users page with names, emails and the Blocked section;
  - View as on `/settings`.

---

### Controller: merge, review, preview, deploy

1. **Wave 0:** implement W0 on `feature/part4` and push.
2. **Wave 1:**
   - create worktrees `p4-activity`, `p4-profiles`, `p4-block` and `p4-notifier` from `feature/part4` and dispatch A, S1, K and N in parallel;
   - review each task as it finishes (spec, then quality), with fix rounds by a fresh implementer;
   - merge in the order **A, K, S1, N**; at K's merge, swap `onBlocked` for `activity.SignInBlocked`;
   - after each merge: `build/dev/gotest.sh` (which regenerates), commit `build: regenerate static assets after <task>`, and git-add new min files;
   - run each task's Controller checks.
3. **Wave 2:** S2 in `p4-ui`; review; merge; regenerate.
4. **Wave 3:** E. Then a **whole-branch review** by a fresh reviewer against this plan, the spec and the Part 1–3 rulings, then one fix round.
5. **Preview:**
   - copy the live volumes **read-only** into fresh volumes and switch the copy to internal auth (`Authentication.Method 0`), `ServerUrl` `https://francos-mac-mini.tail2dc5de.ts.net:8443/`;
   - set a temporary password with `/app/run.sh --deployment-password <pw>` once, then recreate without it;
   - create anna and ben via `/api/user/create` + `resetPassword`; seed their display names, and a photo via `GOKAPI_E2E_SEED=1`, which is **only** on the preview;
   - run on 127.0.0.1:53910 with the compose hardening, and expose it with `tailscale serve --bg --https=8443 http://127.0.0.1:53910`; test via the tailnet IP;
   - mount a copy of the branding under `~`, with no secrets;
   - run a **preview notifier**, `--once` by hand, with its own config and state in `~/src/Gokapi-wt/preview-notify/`: `api_url` 53910, `redirect_all_to: franco@goxcoworld.com`, the existing SMTP credentials read from `~/apps/gokapi/notify.config.json` (read-only) into that scratch config;
   - do the Playwright walkthrough on desktop and 390×844, then hand the preview to Franco.
   - **Tell him:** on live, names and photos appear only after each person's next Google sign-in, which can take days with long sessions.
6. **Deploy, only after Franco's yes:**
   1. `launchctl unload ~/Library/LaunchAgents/com.franco.gokapi-notify.plist` (stop the old notifier first, so it doesn't email the restart).
   2. Back up `docker-compose.yml`, `gokapi-notify.py`, `notify.config.json`, `.notify-state.json` and any branding file changed as `.bak.2026-10-<dd>`.
   3. Run `docker --context colima builder prune -a -f`, so the disk watcher's first check doesn't warn about the build cache. Stop gokapi; run the sidecar `sqlite3 .backup` into `~/apps/gokapi/backups/backup-pre-part4-<date>.sqlite`; check `PRAGMA integrity_check`, the schema version (17) and the file count.
   4. **The notifier's API key, created without the secret ever reaching a chat or a log.** Live is Google sign-in only, so nobody can sign in through a script to create the key.
      - While gokapi is still stopped, one Python script on the mini generates the key with `secrets` and the sidecar `INSERT`s it into `ApiKeys`:
        - `Id` = the key: 30 characters (`LengthApiKey`), from `secrets`;
        - `FriendlyName` `Notifier (Part 4)`;
        - `LastUsed` 0 (the column is `INTEGER NOT NULL` with no default);
        - `Permissions` 128 (`ApiPermManageLogs` = 1<<7);
        - `Expiry` 0;
        - `IsSystemKey` **0**: "View as…" deletes system keys when it starts, and would take the notifier's key with them;
        - `UserId` = Franco's admin id (the route is AdminOnly);
        - `PublicId` = a random 35 characters (`LengthPublicId`);
        - `UploadRequestId` `''`.
      - The same script writes only `api_key` into `notify.config.json` (mode 600). It does **not** write `api_url` or `site_url`: their defaults are the exact live values, and the notifier refuses to send if `api_url` differs from `http://127.0.0.1:53842/api/activity` by a single character. It prints only `ok` and the PublicId, never the key.
      - **Fallback** if Franco prefers the UI: he creates the key in the API tab with Manage Logs on. He then pastes it into a `read -s` one-liner on the mini that updates the config, never into chat.
   5. Swap the image to `gokapi-fgs:part4-<date>` and `docker compose up -d`.
   6. Install `deploy/notifier/gokapi_notify.py` as `~/apps/gokapi/gokapi-notify.py`. Run `--once --from-start` by hand, then load the plist.
   7. Read-only smoke tests on the public URL: login page, `/settings` redirects to login, `/avatar` without a session redirects. Then `curl` the feed with the key from the mini. Check `log.txt` for `[warning]`.
   8. Open and link the PR `feature/part4 → feature/collections`.
   9. Remove the preview, its volumes, `tailscale serve` on 8443, and every scratch copy of the live DB and of `notify.config.json`.
   10. Update `~/atlas/memory/gokapi-share-redesign.md` and add the deploy record and rulings below.
   - **Rollback:**
     1. Restore `docker-compose.yml.bak.<date>` and `gokapi-notify.py.bak.<date>`.
     2. **Delete** `.notify-state.json` rather than restoring it. The old notifier then resyncs to the end of `log.txt` without emailing. A restored offset would re-send every line logged since the swap, one email each.
     3. `docker compose up -d`, then reload the plist.
     4. The new tables are ignored by the old image.
   - **Known gap at cutover:** events between step 1 (old notifier unloaded) and the swap are not emailed. That is a few minutes, during which the site is already in its maintenance window.

---

## Rulings during execution

Decisions the controller took while executing; the SDD ledger they came from is scratch and deleted after go-live, so this is the durable record.

- W0 ran as an implementer task, reviewed like the others — cost: none.
- Wave 1 (A, S1, K, N) ran in parallel worktrees, started from W0 before its review finished; merged in completion order (K, N, S1, A), with one asset regeneration after the last Go merge — cost: merge conflicts, resolved keeping both sides (ensure chain profiles → activity → blocklist).
- A: `via` is whatever the existing download tag carries (group/collection/file/zip); API downloads appear as `file`/`zip`, never `api` — cost: the "via API" label never shows.
- A: `?since=` in a query string answers 400 `InvalidUrl` (R-b), not "ignored" — the e2e asserts 400.
- A: recording is best-effort (a recover in `record()` and the pruner), so a failed Activity write never breaks an upload or a zip — cost: a row can be missing, with one stdout line.
- A: a collection→group conversion records `group_created` only once the group survives (M4), with the person converting as actor; viewers added by conversion get no `group_joined` — cost: summaries don't say "X was added".
- A: a preview that kills the process on its final attempt is still warned about (backfill's pending-at-limit branch).
- K: the blocklist fails closed on a DB error, re-checks every session (`IsValidSession` and `PeekSession`, via `sessionmanager.SessionRefused`), is admin-only in the UI, and errors on Redis — cost: two primary-key lookups per request.
- K: header auth runs no sign-in guards (live uses OAuth) — known, documented.
- S1: display names refuse control (Cc) and format (Cf) characters and need a visible rune; the same rule drops an invalid Google name — cost: a legitimate ZWJ emoji sequence is refused.
- S1: a photo fetch is tried at most once a day per person, failed or not (`PhotoTried`), and only one fetch runs for concurrent sign-ins.
- S1: with "Use initials" on, `/avatar` serves the photo only to its owner.
- S2: shown names also replace raw names in the Files tab, file-request feeds and the collections-admin owner badge; `html_api.tmpl` and `html_uploadrequest.tmpl` stay raw (your own account).
- Public collection pages now show the owner's shown name, e.g. the Google full name (Q9) — flagged to Franco.
- N: undeliverable mail is classified by exception type — message errors (`SMTPDataError`, `SMTPRecipientsRefused`) are dropped for friends at once and for `mail_to` after 3 attempts; transport errors back off. While any alarm is queued the backoff is at most 5 minutes; a failing alarm builder falls back to plain `[FGS] <kind>` emails after 3 tries — cost: a 4xx `SMTPDataError` drops a friend digest at once.
- F: the grid badge overlap was older than Part 4 (a caption-relative offset); it is now placed from the thumbnail inside `@supports (width:1cqw)`, so older Safari keeps the old placement.
- Test fix: `TestEditFileInGroupLocked` was order-dependent (another test's async CleanUp dropped its blob-less file); the test now writes its blob. Not a product bug.
- Not fixed, follow-up: two byte-identical files uploaded in one batch into a group can both be kept (`AttachUploadToGroup` check-then-act, no lock; pre-existing, benign, "Check for duplicates" removes it). The first `/groups/item` fetch after an upload 404s until the attach lands; the client retries (pre-existing).
- Final review: one notifier must-fix (a refused email to `mail_to` blocked every later alarm) and the deploy key INSERT (missing `LastUsed`; `IsSystemKey` must be 0) were fixed before go-live; a second narrow notifier fix round followed (5-minute clamp, builder fallback).
- Franco's preview requests (2026-10-24): photo in the user pill, a 40rem Settings card with Back, Unblock wraps at 390 px, a visible checked state on the disabled switch. "Can't manage anna's group under View as" is by design: as anna she can rename, extend and delete; View as is read-only.

## Deployed

Live since 2026-10-24 09:51 (about a minute of downtime) as `gokapi-fgs:part4-2026-10-24` from `5bf6135`. PR #4 into `feature/collections`.

- The old notifier was unloaded first; the new one (`deploy/notifier/gokapi_notify.py` → `~/apps/gokapi/gokapi-notify.py`) ran `--once --from-start` (cursor 0, nothing pending) and was loaded under the same plist. State: `.notify-state-v2.json`.
- The notifier's key ("Notifier (Part 4)", Permissions 128, not a system key, user 1) was inserted while gokapi was stopped and written only into `notify.config.json`; never printed. `api_url` and `site_url` stay at their defaults.
- DB backup `~/apps/gokapi/backups/backup-pre-part4-2026-10-24.sqlite` (integrity ok, schema 17, 222 files). No branding change.
- Smoke tests: `/` 200; `/login` → `/oauth-login`; `/settings`, `/avatar`, `/files` → `/login`; `/api/activity` 401 without a key and OK with it; no `[warning]` lines.

**Rollback:**
1. Restore `docker-compose.yml.bak.2026-10-24` (image `gokapi-fgs:part3-2026-10-22`) and `gokapi-notify.py.bak.2026-10-24`, and `notify.config.json.bak.2026-10-24` if wanted.
2. **Delete** `.notify-state.json` so the old notifier resyncs to the end of `log.txt` instead of re-sending it.
3. `docker compose up -d`, reload the plist. The new tables are ignored by the old image.
