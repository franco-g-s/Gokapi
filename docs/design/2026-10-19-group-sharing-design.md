# Group sharing, previews and quieter notifications — design

Status: approved in conversation 2026-10-19, awaiting review of this write-up.
Branch base: `feature/collections` (the build running as `gokapi-fgs:collections`).

## Why

One person sending a few files to another already works well. The hard case is
a group trip or an event. Today everyone signs in, uploads their own photos and
posts a link to their own batch in the chat. That leaves recipients with:

- no single place to see all the photos;
- no previews, so they download everything and delete what they did not want;
- no record of what they have already downloaded;
- duplicates when someone re-uploads a photo they got from someone else;
- "Download all" on iPhone Safari saving only the first file.

At the same time the admin gets flooded with notification emails: every upload
is emailed on its own, and downloads arrive in batches of 50.

Gokapi's back-end handles storage, deduplication, expiry, chunked uploads
through Cloudflare and Google sign-in well, and all of that stays as it is.
This work is mostly about the front-end flow. Back-end additions are limited to:

- the preview worker;
- tables for groups, members, downloads, "shared with me" and settings;
- one shared access check;
- an activity feed.

## Ground rules

- **Originals are never altered.** Uploads are stored and served byte for byte.
  Thumbnails and previews are extra copies, used for display only, and no
  download path ever serves one.
- **Everyone who uploads or joins a group signs in with Google.** Collection
  and single-file links keep working without sign-in, as today.
- **Groups are unlisted.** You learn a group exists only from its link or code.
  The only exception is the admin, who sees all groups.
- **New code lives in its own files**, as collections did, so rebasing onto
  upstream stays cheap. Upstream has no preview or gallery work; the only
  related item is the open feature request forceu/gokapi#281.
- **One access rule.** Every new endpoint, whether preview, board feed or zip,
  answers "may this request see file X?" through one function,
  `access.CanView(request, fileId)`. Nothing new is reachable by guessing a
  hash or a path.

## Build order

There are four parts. Each has its own branch, implementation plan and deploy,
and each is useful before the next one starts.

1. **Previews, the Files tab, the viewer, board layout for collections, and
   fixing "Download all" on iPhone.**
2. **Groups.**
3. **Save to Photos (batching, big videos) and "View as…".**
4. **Notifications and the Settings page.**

**Must be built before the revamp is finished** (Franco, 2026-10-20). The
board's batches of about 100 and its server-side filters ("Board layout",
items 3 and 4) were built in neither part 1 nor part 2: every list still
renders all its items at once, which is fine up to about 2,000 items. They
are scheduled for **part 3**.

## Navigation

Sidebar order (the custom `admin.js` sidebar):

Upload · **Files** · **Groups** · Collections · File requests ·
Users *(admin)* · API · Status *(admin)* · **Settings** · Logout

At the bottom there is a "View as…" switch, shown to the admin only. The
site-wide Users page keeps its name; "members" refers only to the people in a
group.

The Upload page's "Combine each upload into one link" toggle defaults to
**on** when no preference has been saved yet.

From part 2, the Upload page also has an **"Upload to"** selector: *My files*
(the default) or any group you belong to. Uploading into a group from here is
the same as uploading on the group page.

---

## Part 1 — Previews

### Preview worker

A single background goroutine reads a queue. Jobs are added:

- when an upload finishes;
- at startup, for any file whose fingerprint has no preview row yet. This is
  how the ~228 existing files are covered.

The worker runs external commands with per-call timeouts: `vips` (with
libheif) for images, and `ffmpeg` / `ffprobe` for video. Running them as
separate processes means a malformed file can kill only that one process, never
Gokapi. Both tools are added to the Docker image.

The worker processes one job at a time. That keeps it inside the container's
existing `mem_limit: 1g` and `cpus: 1.5` limits, so it cannot starve SplitPro.

For each fingerprint (SHA1) the worker produces:

| Output | Spec |
|---|---|
| Thumbnail | WebP, ~480 px wide (revised from 640 on 2026-10-19 to save storage). Used for grid icons, list icons and boards. |
| Preview | JPEG, ~1600 px on the long side, quality ~75 (revised from 2000 px). Used in the viewer, which labels it as a lower-resolution preview. |
| Poster | Video only: one frame, sized like the thumbnail. |
| Metadata | Width and height **after EXIF rotation**, taken-at timestamp (EXIF `DateTimeOriginal`, or the QuickTime creation date), duration for video, average colour, perceptual hash. |

Storage and status:

- Files go in `data/previews/<sha1>-{thumb,view,poster}.*`.
- Metadata goes in a new `Previews` table keyed by SHA1, with a `status`
  column: `pending`, `ready`, `failed` or `unsupported`.
- Previews are keyed by fingerprint, so identical files are processed once.
- Previews are deleted when the last file with that fingerprint is deleted.
- A generator version is recorded. When the sizes or quality change, existing
  previews are regenerated once, automatically, in the background.

Supported formats:

- **Images:** JPEG, HEIC/HEIF, PNG, WebP, GIF.
- **Video:** MOV, MP4.

Everything else gets a Finder-style icon for its file type. There are no
PDF or document thumbnails in this scope.

Failure handling: a failed or unsupported file shows its type icon. Preview
generation never blocks or fails an upload. Repeated failures are reported as a
warning, which part 4 emails.

End-to-end encrypted files cannot be read by the server. They get no preview
and show the type icon.

### Access

`access.CanView` grants access to:

- the uploader;
- the admin, or anyone with `PERM_LIST`;
- members of a group the file belongs to;
- holders of an unlocked session for a collection that contains the file,
  having passed the password if the collection has one;
- holders of the file's own download link (`/d?id=`), whose page now shows the
  preview image as well.

Preview routes:

- `/p/thumb?id=<fileId>`
- `/p/view?id=<fileId>`
- `/p/poster?id=<fileId>`

They are addressed by **file ID, not by SHA1**, and are checked through
`CanView`.

Responses are sent with `Cache-Control: private, max-age=31536000, immutable`.
A fingerprint's preview never changes, so the browser can keep it. Cloudflare
cannot cache it, because the pages are private.

### Files tab (new page `/files`)

Revised 2026-10-19 after Franco tried the preview build.

- **View switch:** **My files** (the default) or **Everything**. For the admin,
  Everything means every file from every person. For others it means their own
  files plus those in their groups.
- **Grid view:**
  - Thumbnails, with an icon-size slider. Each tile shows the uploader and the
    upload date.
  - A **Square / True shape** toggle. True shape shows the whole picture,
    letterboxed inside each square cell, so orientation and aspect ratio are
    visible.
- **List view:**
  - Columns are a small thumbnail plus name · uploaded by · uploaded · taken ·
    size · expires · downloads · actions.
  - There is no icon-size slider; it shows only in grid view.
- **Opening and selecting:**
  - A **Select** button toggles select mode.
  - With select mode off, there are no checkboxes. Clicking a tile, or a name
    in the list, opens the full-screen viewer.
  - With select mode on, checkboxes appear and clicking toggles selection.
- **Per-file actions:**
  - One "⋯" menu, identical in grid and list. It holds Open, Download original,
    Copy link, Share…, QR code, Edit, Replace content, Add to collection… and
    Delete. From part 2 it also holds **Add to group…**.
  - Edit covers the name, expiry, download limit and password. Renaming is a
    small server addition, because upstream cannot rename.
  - In the grid, the menu sits on the tile corner: always visible on touch
    screens, shown on hover on desktop.
- **Bulk actions (select mode):**
  - Download, Download as .zip, Save to Photos on iOS/iPadOS.
  - Create one link, Add to collection…, Change expiry & limits.
  - Delete, which asks for confirmation.
  - From part 2, also **Add to group…**.
- **The Upload page** goes back to being only for uploading. The file table
  moves here.

### One file list everywhere

Decided 2026-10-19. Every file list on the site uses the same shared
component, built from the Files tab: the same views, thumbnails, "⋯" menu,
Select mode and selection bar, viewer, Square / True shape and size slider.
Each page differs only in what it lists and which actions it allows.

- **Views:** Grid · Board · List on every list. Board is the masonry layout
  that keeps true proportions.
- **Default view per page:**
  - Files and Upload: Grid.
  - Collection link page: List.
  - Group page: Board.
  - Collections admin member lists and File requests: List.
- **Per-page content and actions:**

| Page | Lists | ⋯ menu | Selection bar |
|---|---|---|---|
| Files | all your files (Everything for PERM_LIST) | Open, Download original, Copy link, Share…, QR code, Edit, Replace content, Add to collection…, Delete | Download, zip, Save to Photos, Create one link, Add to collection…, Change expiry & limits, Delete |
| Upload | files uploaded in this visit, added live as each finishes | as Files | as Files |
| Collections (admin), per collection | that collection's members | as Files, plus Remove from collection | as Files, plus Remove from collection |
| File requests, per request | files uploaded to the request | Open, Download, Delete (no share link exists) | Download, zip, Save to Photos, Delete |
| Collection link page (visitors) | the collection's live members | Open, Download | Download, zip, Save to Photos |

- **Retired:** the custom upload-page "Create one link" bar in the branding
  `admin.js`. It is removed at deploy, because the shared selection bar
  replaces it.
- **Access rule change:** previews of file-request uploads are visible to
  their owner (the request's creator) and to PERM_LIST users. Everyone else
  is still denied, as with `/d`.

### Viewer

A full-screen overlay that opens from any grid, board or collection page.

- **Navigation:** arrow keys, swipe on touch screens, or a horizontal
  trackpad swipe / horizontal scroll on desktop.
- **Photos:** shows the ~1600 px preview, labelled "Lower-resolution preview —
  download the original for full quality".
- **Stacking:** the viewer covers everything on the page, including the
  sidebar and user badge from the custom branding.
- **Videos:** play from the original file using range requests, which
  `http.ServeContent` already supports. If the browser cannot play the video
  (for example HEVC in Firefox), the poster is shown with a download button.
- **Info panel:** uploader, date taken, size, **Download original**.

### Board layout

The board is a masonry layout. It loads as follows:

1. **Layout first.** The server sends each item's width, height and average
   colour, so every tile is placed at its final size immediately, filled with
   that colour. Nothing jumps when images arrive.
2. **First screen at high priority.** About the first 12 thumbnails are
   requested immediately with `fetchpriority="high"`. The rest load through an
   IntersectionObserver a little ahead of the scroll position.
3. **Continuous scroll.** There are no pages and no "load more" button. The item
   list is fetched in batches of about 100 whenever the reader is within a
   couple of screens of the bottom.
4. **Server-side filters.** Filters (by person, "hide what I've downloaded")
   and "Select all (N)" all act on the full set, including items not yet
   scrolled to.

Default layout:

| Page | Default | Toggle |
|---|---|---|
| Collection page | List | Grid / Board / List |
| Group page | Board | Grid / Board / List |

Collection pages use the full width on desktop rather than a fixed narrow card;
phones are unchanged. In the Collections tab, a collection's name opens its
page.

### Download all, and zip

"Download all" on a collection currently starts each file's download in turn.
iPhone Safari allows one download per tap and silently drops the rest, which is
why only one file arrives.

The old reason for not zipping was the memory ceiling of zipping in the
browser. That does not apply to a zip built by the server.

**New endpoint:** `/zip`. It streams a **store-only zip** (no compression,
since media is already compressed):

- Files are written as they are read, so there is no temporary file and no
  whole-archive buffer.
- Each entry's modified time is set to the taken-at date, falling back to the
  upload date, so Finder sorts the unzipped files correctly.
- Access to every file is checked through `CanView`.

**Buttons by device:**

| Device | Primary | Secondary |
|---|---|---|
| Computer | Download all (separate files, as today) | Download as .zip |
| iPhone / iPad | Save to Photos (share sheet; see part 3) | Download as .zip |

This applies to collection pages, group pages and Files tab multi-select.

**Help on phones.** On iPhone and iPad, a small **?** button sits next to the
download buttons. It opens a short sheet titled "Downloaded to Files? Here's
how to get them into Photos":

1. Open the **Files** app and go to **Downloads**.
2. If it's a zip, tap it once to unzip it into a folder, then open the folder.
3. Tap **⋯** → **Select**, then select the photos and videos you want.
4. Tap **Share** → **Save N Images** (or **Save N Videos**).

It also explains that the photos keep their original date and time, so they
land in the right place in the Photos timeline.

### Upload button

The file input is configured in the way Apple documents for receiving original
files rather than converted JPEGs or re-compressed video. No separate
on-device test is planned (Franco's call).

### Testing

- **Go unit tests:**
  - `CanView` for every grant path, plus one denial case per path.
  - The worker, run against fixtures: an iPhone HEIC, a rotated JPEG, an iPhone
    MOV and a corrupt file. Checks dimensions after rotation, taken-at and
    status.
- **Zip test:** byte-for-byte equality of the zip entries with the originals.
- **Browser test (Playwright on the mini, before deploy):**
  - Files grid and list.
  - The viewer.
  - A collection board.
  - "Download as .zip".
  - The existing collections E2E script stays green.

---

## Part 2 — Groups

### Data

New tables (SQLite; the Redis provider gets matching stubs, as collections
did):

| Table | Columns |
|---|---|
| `Groups` | id, name, creator user id, created, expires (0 = never), join code, invite token |
| `GroupMembers` | group id, user id, joined, role (`creator` / `member`) |
| `GroupFiles` | group id, file id |
| `Downloads` | file id, user id (nullable for anonymous), at, via (`group` / `collection` / `file` / `zip`) |
| `CollectionsSeen` | user id, collection id, first opened |

Files uploaded into a group are ordinary Gokapi files with these settings:

- unlimited downloads;
- expiry equal to the group's expiry, or unlimited if the group never expires.

Extending a group extends its files.

**Adding existing files.** Files you already uploaded can be added to a group,
from the Files tab (per file or multi-select) or by picking the group in the
Upload page's "Upload to" selector. When a file joins a group:

- it gets unlimited downloads;
- its expiry is extended to the group's if that is later. It is never
  shortened.

`GroupFiles` is a join table, so a file can be in several groups. Its expiry is
the latest of its own and all its groups'. Removing a file from a group does not
shorten its expiry. Adding a file goes through the same duplicate check as
uploading. Only your own files can be added; the admin can add anyone's.

### Creating and joining

- **Who can create:** anyone signed in (Franco's call; abuse is handled by
  asking, then kicking, and part 4 makes group creation an immediate email).
- **Create form:** a name and an expiry. Options are 30 days, **60 days
  (suggested default)**, 90 days and never.
- **What a new group gets:**
  - an **invite link**: `/g/join?t=<token>`;
  - a **short code**, such as `K7P-4QX`, from an alphabet without look-alike
    characters.

  Both are shown with copy buttons.

  Revised 2026-10-20: the code is stored as is, not as a hash. The invite
  token beside it is stored in plain text and grants the same access, and
  both are shown to members again, so a hash protected nothing. Guessing is
  stopped by rate limiting and the size of the code space; a leaked code is
  undone with a reset.
- **Joining:** open the link, or type the code under **Join** on the Groups
  tab, then sign in with Google if not already signed in. Join attempts are
  rate-limited per IP and per account through the existing rate limiter.
- **From an existing collection:** a collection's owner can **Turn into a
  group**. The new group starts with the collection's files and its name. The
  owner picks the expiry as for a new group.
- **Roles:**
  - **Creator:** can rename, extend, reset the code (which invalidates the old
    code and link), remove members, remove any item, and delete the group.
  - **Members:** can delete only their own uploads.
  - **Editing** (revised 2026-10-20): anyone who may edit a file can still
    rename it or replace its content from a group page. While a file is in a
    live group, its expiry, download limit and password follow the group and
    cannot be edited anywhere.
  - **Admin:** can do everything, in every group.

### Groups tab (`/groups`)

- **Lists the groups you are a member of.** If there are none, the page shows
  an empty state with just **Create group** and **Join with a code**.
- **For the admin:** every group, with the ones Franco is not in marked "not a
  member". He can open any of them.

### Group page (`/g?id=`)

- **Header:**
  - name;
  - member avatars and names;
  - "closes 12 Jan";
  - invite link and code;
  - the creator's controls.
- **Upload:** an upload area on the page itself.
- **Board or list (board is the default):**
  - sorted by date taken, so everyone's photos interleave in shooting order;
  - each tile shows the uploader's name or initial.
- **Filters:** everyone or one person; "Hide what I've downloaded".
- **Downloaded markers:** a tick on every item you have downloaded, through any
  route, from the `Downloads` table.
- **Selection:**
  - tap to select;
  - **Select all (N)**;
  - **Select everything new to me**.

  Then the device-appropriate buttons from part 1.

### Duplicates

- **Exact copies.**
  1. Before uploading, the browser computes each file's SHA1 (the fingerprint
     Gokapi already stores) and asks the server whether the group has it.
  2. If it does, the file is not uploaded and the user sees "Already here,
     uploaded by Anna".
  3. This also avoids pushing large videos through Cloudflare a second time.
- **Near-copies.**
  1. After the preview worker runs, the new item's perceptual hash is compared
     with the rest of the group's.
  2. A close match is flagged to its uploader on the group page: "Looks like
     Anna's photo from 14:32".
  3. The uploader picks **Keep both** or **Remove mine**.

### Expiry

When a group expires, its files expire through Gokapi's normal cleanup. Storage
is shared by fingerprint, so bytes stay on disk while any other live file still
uses them.

### Collections: "Shared with me"

When a signed-in person opens a collection link (`/c?id=`), a row is recorded
in `CollectionsSeen`. Their Collections tab gains a **Shared with me** section
listing those collections. Anonymous visitors are unaffected, and nothing is
recorded for them.

### Users page

The existing Users page, admin only, keeps its name and gains:

- each person's file count, total size and groups;
- a per-person view of everything that person uploaded, using the Files grid.

### Testing

- Go tests: membership and role rules, code reset, rate limiting on join,
  expiry propagation when a group is extended, adding an existing file
  (never shortens its expiry, lifts its download limit), the duplicate lookup,
  and that
  `CanView` grants access to group members and only to them.
- Playwright tests:
  - create a group, then join it as a second user by link and by code;
  - upload a duplicate;
  - downloaded ticks;
  - select all and zip;
  - the empty state.

---

## Part 3 — Save to Photos, and "View as…"

### Save to Photos (iOS / iPadOS)

1. The button fetches the selected originals as Blobs.
2. It calls `navigator.share({ files })`, which opens the share sheet. "Save N
   Images" puts them straight into Photos, with their original metadata and
   capture date.
3. Large selections are split into batches sized to what Safari accepts. Each
   batch needs its own tap, because the share sheet requires one.
4. Videos above a size threshold are handed off to "Download as .zip" with an
   explanation, rather than risking Safari running out of memory.

The batch size and the video threshold are measured during implementation.

**On a Mac,** a web page cannot write into Photos. Desktop gets the original
files (or a zip), and the Photos app's import keeps their metadata.

### "View as…" (admin only)

- Choose a person from the Users page or from the sidebar switch.
- The server then renders every page with that person's visibility: their
  sidebar, files, groups and collections. A banner reads **"Viewing as Anna ·
  read-only · Exit"**.
- **The server rejects every change while it is on.** Rejected requests
  include:
  - upload, delete, edit;
  - join, leave;
  - settings;
  - group administration;
  - **downloads**, which would otherwise write `Downloads` rows that change
    the person's ticks and emails.
- Previews and the viewer work as normal.
- Each start and end of a "View as…" session is written to the Status log.

### Testing

Go tests that every route which changes something, and every download route,
rejects a request while "View as…" is active.

---

## Part 4 — Notifications and Settings

### Activity feed

Gokapi gains `GET /api/activity?since=<cursor>`. It requires an admin API key
and returns structured events:

- **upload** (who, file, size, group or collection);
- **download** (who or anonymous, file, owner, route, group);
- **group created / joined;**
- **first sign-in;**
- **warnings:** free disk under 10 GB, preview failures.

It also returns each user's notification preference.

`~/apps/gokapi/gokapi-notify.py` changes as follows:

- It reads this feed instead of `docker exec cat`-ing `log.txt` and parsing
  log lines.
- It keeps its existing Gmail SMTP sender.
- The email credentials stay on the host, outside Gokapi.

### Franco's emails

| Event | Sent |
|---|---|
| First sign-in of a new person | Immediately |
| Group created | Immediately |
| Warning (disk < 10 GB, preview failures) | Immediately |
| Everything else | Held until **one hour with no activity**, then one summary. During continuous activity a summary still goes out at least every 3 hours. |

Summaries are grouped by group and person:

> **Iceland trip** — Anna uploaded 143 photos, 6 videos (2.1 GB) · Ben
> downloaded 87 · Carla joined
> **Your files** — "CV.pdf" downloaded 3 times (someone, via link)

### Friends' emails

- **"Email me when my files are downloaded"**, off by default.
- It is one per-person setting, shown in **two places**:
  - the Settings page;
  - a switch on the Upload page, mirroring the same setting.
- When on, they get at most one email a day, and only if something happened:
  "Ben and Carla downloaded 41 of your photos from Iceland trip."
- Downloads by people who were not signed in appear as "someone".
- These emails are sent from the notifier's existing Gmail account (limit about
  500 a day).

### Settings page (`/settings`, per person)

- the email switch above;
- a display name, defaulting to the Google name, used on tiles and in emails.
- a profile photo, taken from the Google sign-in, replacing the initials
  that part 2 shows for members (Franco, 2026-10-20).

### Failure handling

- If the feed is unreachable or SMTP login is rejected, the notifier logs the
  error and retries.
- It does not email about its own failures in a loop.
- The feed cursor is persisted, so a restart neither re-sends old events nor
  skips new ones.

### Testing

- A notifier test that replays a recorded busy evening of events. It asserts
  the exact set of immediate emails and summaries, including both the one-hour
  quiet rule and the three-hour cap.
- Go tests for the feed's authentication and cursor behaviour.

---

## Out of scope

- PDF and document thumbnails.
- Anonymous viewing of groups.
- Server-side image editing, albums inside groups, comments and likes.
- Changes to upload storage, encryption, expiry semantics outside groups, or
  file requests.
