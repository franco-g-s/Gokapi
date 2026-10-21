# Part 2 revisions: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Apply Franco's feedback from the Part 2 preview (2026-10-21):
- **Near-copies:** compare a pair full screen; better matching; creators and admins see every pair.
- **Group page:** a members panel with an avatar stack; a smaller drop zone on phones.
- **Collections:** a cleaner Collections table; collection pages that show who made them and give their owner controls; a richer "Turn into a group".
- **Groups tab:** a "Copy join link" button.
- **Save to Photos:** in one tap where possible, from the viewer and the "⋯" menu as well, keeping the date the photo was taken; a "⋯" menu that matches the site's look.

**Architecture:** small edits to Part 2's files, grouped so that parallel tasks never share a file:
- **R1** owns the group page.
- **R2** owns the Collections pages and the Groups tab.
- **R3** owns the shared file list and the viewer.
- **R4** runs after R1 and R2 merge, because it touches both.

There are no schema bumps. R4's new table is created with `CREATE TABLE IF NOT EXISTS`, as Part 2's were.

**Tech Stack:** Go, run only via `build/dev/gotest.sh` in Docker; html/template; vanilla JS; Bootstrap 5.

**Spec:** `docs/design/2026-10-19-group-sharing-design.md`, Part 2 and the Part 1 sections "Viewer" and "Download all, and zip". Read it together with the decisions table and "Rulings during execution" at the end of `docs/superpowers/plans/2026-10-20-part2-groups.md`.

## Global Constraints

Every Global Constraint in `docs/superpowers/plans/2026-10-20-part2-groups.md` still applies: Docker-only Go, literal `requireLogin` routes, POST and JSON-only mutations, stdout-only logging, `textContent` for names, `displayName`, never touching live, no background processes, and no subagents. The process contract is `.superpowers/sdd/2026-10-20-part2-groups/implementer-contract.md`.

- **Branch** `feature/groups`. Parallel tasks run in worktrees `~/src/Gokapi-wt/<task>` on branches `r/<task>`, and commit source only.
- **Exact copy:**
  - "Copy share link" (it replaces "Copy link" in the Collections table)
  - "Copy join link"
  - "by anna" on a collection page
  - "Save to Photos"
  - "Ready — tap to save" (with " N photos" when there are several)
  - "Compare"
  - "Members"
  - "Also shared as a link"
  - "Stop the link"
  - "This is now a group — sign in to join and add your own"
  - "Join"
- **Owner controls on a collection page** appear only for its owner or an admin (`IsAdmin() && Id != 0`), identified by `authentication.SessionIdentity`. Anonymous visitors see exactly what they see today, plus "by …".

---

### Task R1: Group page — near-copies, members panel, phone drop zone

**Files:**
- `internal/storage/NearCopies.go` and its tests
- `internal/webserver/GroupPage.go` and its tests
- `templates/html_group.tmpl`
- `static/js/admin_ui_group.js`
- `static/css/group.css`

**Near-copy matching.** Franco's test matched two different Apple wallpapers: the same design in blue and in pink. dHash works on grayscale, so colour variants of one layout collide. A pair now counts as a near-copy only when **all** of these hold:
- dHash distance ≤ `nearCopyMaxDistance` (6);
- average colours within `nearCopyMaxColour` = 40, measured as Euclidean RGB distance on the previews' `Color` `#rrggbb` values;
- aspect ratios (width / height after rotation) within 3%.

Add these test cases to `TestNearCopies`:
- same dHash but colours `#3060d0` vs `#d060a0` → not a near-copy;
- same dHash, colours 10 apart, same aspect → near-copy;
- same dHash and colour, aspect 4:3 vs 3:4 → not.

**Who sees a pair.** The uploader of the newer item sees it, as now. **The group's creator and admins see every pair in the group**, labelled "Looks like Anna's photo from 14:32 (uploaded by Ben)".
- Their actions are **Keep both**, which dismisses the pair for everyone (the same `GroupNearKept` row, keyed by the newer file), and **Remove newer**, which removes the newer item from the group. Removing from the group is allowed for creators by D4.
- `NearCopies` gains a mode: `NearCopies(files, viewerId, kept, all bool)`. Update callers and tests accordingly.
- Tests: the creator, who uploaded neither item, sees the pair; a plain member who uploaded neither does not.

**Compare.** Both banner thumbnails become buttons, and a **Compare** button sits beside Keep both. Each opens the full-screen viewer (`FGS.openViewer`) on the pair `[older, newer]`, starting at the tapped one, so Franco can swipe between the two at full preview size.
- Build the items with `FGS.itemFromEl` on the two tiles, which are in the page.
- If a tile is filtered out, build the item from the banner's own `data-*` (view URL, name, download).

**Members panel.**
- **Replaces** the inline member list in the header.
- **The stack:** an overlapping stack of up to 5 initials avatars (the existing colour rule), then "+N" when there are more, then "N members". It is one button.
- **Tapping it** opens a Bootstrap dropdown panel titled "Members". The panel lists each person with avatar, name and role badge ("Creator").
  - For the creator and admins, each non-creator row has the remove button, with the existing dialog and reset checkbox.
  - The panel scrolls past about 8 people.
- **Accessibility:** `aria-label="Members: N"` on the button; the list is a `ul`.
- **Tests:** the markup has the stack button and the "+N" overflow when there are more than 5 members; the panel lists every member.

**Drop zone on phones.** At widths up to 576px, the group page's drop zone is compact:
- vertical padding about 1rem;
- the title at body size;
- the subtitle on one line;
- the "Files uploaded to a group stay…" note as `small`.

The goal is that the header plus the drop zone fit in roughly half the first screen of a 390×844 viewport. This is CSS only, in `group.css`, scoped to `.fgs-group-upload`.

- [ ] Tests first: the `NearCopies` cases and the page markup. RED, implement, GREEN. `node --check` the JS. Run the full `./internal/storage/ ./internal/webserver/`.
- [ ] Commit `groups: near-copies match on colour and shape, compare full screen, creators see all; members panel; compact drop zone on phones`, then push.

---

### Task R2: Collections table, collection page owner controls, Groups tab join link

**Files:**
- `internal/webserver/Collections.go` and its tests
- `templates/html_collections_admin.tmpl`
- `templates/html_collection.tmpl`
- the collections admin JS: whichever `admin_ui_*.js` handles the Collections table; grep for `collections/modify`
- a new small `static/js/collection_owner.js` for the collection page (see below)
- `internal/webserver/GroupsTab.go`
- `templates/html_groups.tmpl`
- `static/js/admin_ui_groups.js`

Do **not** change "Turn into a group" behaviour; that is R4. R2 only adds a second button that opens the existing dialog.

**Collections table, Actions column:**
- Remove the "list files" button and the "Open" button: the name already opens the collection.
- Rename "Copy link" to **"Copy share link"**.
- Keep, as one compact group of icon buttons with visible tooltips and `aria-label`s: Copy share link, Rename, Password, **Turn into a group**, Delete.
- Turn into a group gets a clear icon (`bi-people-fill`) and the label text "Group" visible at ≥ md widths, so it can be found.
- Read the current template (rows around lines 36–84) and keep every behaviour you are not told to change.
- Where the removed "list files" row/toggle carried the per-collection file browser (`files-<id>` row), keep the browser reachable only if something still needs it. Otherwise remove the row and its JS cleanly, and say what you removed.

**The collection page (`/c?id=`):**
- **For everyone:** under the title, **"by <displayName of the owner>"**.
- **For the owner or an admin** (signed in; check with `authentication.SessionIdentity`), an owner bar with three actions:
  - **Rename:** inline, through the existing `/collections/modify` `{id, name}`.
  - **Set password / Change password / Remove password:** through `/collections/modify` `{id, password}`. Keep the rule that a password change revokes unlock tokens. The owner's own view must not lock itself out: the owner bypasses the password gate already, as `CanView` grants owners. Verify, and test.
  - **Turn into a group:** opens a copy of the existing dialog, then navigates to the new group.
- **Wiring:** the public page does not load the admin bundle, so put the owner bar's JS in `collection_owner.js`. Load it only when the viewer is the owner or an admin, via a template flag. It must send JSON with `Content-Type: application/json` (I1).
- **Tests:**
  - anonymous: "by …" is present and the owner bar is absent;
  - owner: the bar is present;
  - a stranger who is signed in: absent;
  - admin: present;
  - a password set by the owner protects the page for others, while the owner still sees it.

**Groups tab:** each row gets a **"Copy join link"** button, which copies `GroupInviteUrl(g)`.
- Only members see a row's link. The admin's "not a member" rows get it too: the admin may manage every group.
- Use the existing ClipboardJS `.copyurl` pattern with the toast.
- **Test:** the row markup has the link.

- [ ] Tests first, then RED, implement, GREEN. `node --check`. Run the full `./internal/webserver/...`.
- [ ] Commit in focused commits (table / collection page / groups tab), then push.

---

### Task R3: Save to Photos — one tap, everywhere, with the date; "⋯" menu look

**Files:**
- `media.js`: `FGS.saveToPhotos`, the viewer, and the menu handling in `initFileBrowser`
- `html_file_browser.tmpl`: the menu
- the viewer markup (find `media_viewer`)
- `media.css`
- `FileBrowser.go` and its test, if the menu gains an entry

**What Franco saw on his iPhone:**
1. The first tap only downloads (the button shows "Preparing…" and then goes back to "Save to Photos"); a second tap opens the share sheet.
2. It is only in select mode.
3. Saved photos get today's date, not the date taken.
4. The "⋯" menu doesn't match the site's look.

**One tap where possible, and an honest second step where not:**
- After the downloads finish, if the tap's user activation is still alive (`navigator.userActivation && navigator.userActivation.isActive`), call `navigator.share` immediately.
- Otherwise, turn the button into a visibly different, highlighted state: **"Ready — tap to save"**, plus " N photos" for several, and a subtle pulse. The next tap shares.
- The state resets on selection change, as now.
- Apply the same to the viewer and menu entries.

**Everywhere:**
- **Viewer:** a "Save to Photos" button next to "Download original", shown only where `FGS.isIOS()` and `navigator.canShare` hold, and only for images and videos. It saves the item on screen.
- **"⋯" menu:** a "Save to Photos" entry (`bi-box-arrow-down`), with the same conditions, in every context that has "download". It is hidden until the JS decides it applies, as the selection-bar button is.
- Add the key `photos` to the Menu lists in `browserActionTable` for every context that has `download`, and update `TestFileBrowserActionTable`.

**The date:**
- When building each `File`, set `lastModified` to the item's date taken (`data-taken`, in seconds; use ×1000), falling back to the upload date.
- The originals' EXIF is intact: verified on the server, every photo has `DateTimeOriginal` and `OffsetTimeOriginal`. Whether iOS honours either is being tested on Franco's phone (https://…:8443/phototest/). Record his result in the report. If none of A–C keeps the date, say so; the help sheet's Files route (D) then becomes the recommended path for dates, and its text must say so.

**The "⋯" menu look:**
- Match the site's dialogs and cards: rounded corners, the dark translucent surface, the border and blur, the type size, and the item padding and hover state.
- Take the values from the site's branding stylesheet. A copy is at `~/src/gokapi-grp-custom/custom.css`, which is outside git; read it and do not edit it. Look for the modal, `.card` and button styles.
- Implement the styles in `media.css` under `.fgs-menu .dropdown-menu`, so they apply to every list.
- Keep keyboard focus styles visible.

- [ ] There is no browser test harness. Keep `node --check` green, and extend `TestFileBrowserActionTable` and the menu-markup tests. Report exactly what needs Franco's phone.
- [ ] Commit, then push.

---

### Task R4: "Turn into a group" — hide the collection, promote its viewers, old-link modes, stop the link (after R1 and R2 merge)

**Franco's choices (2026-10-21):**
- **The collection.** It is hidden from the owner's Collections list; the link keeps working.
- **The dialog** offers:
  - **"Add the people who opened this link while signed in"**: on by default, listing their names, from `CollectionsSeen`, excluding the owner;
  - **"Old link"**, a radio:
    - **Show the photos and invite to join** (default);
    - **Require signing in and joining.**
- **The group page** shows **"Also shared as a link"** to the creator and admins, with the current mode, a switch between the two modes, and **"Stop the link"**, which deletes the collection behind it.

**Data:** a new table `CollectionGroups(CollectionId TEXT PRIMARY KEY, GroupId TEXT NOT NULL, LinkMode TEXT NOT NULL)`, where `LinkMode` is `invite` or `require`.
- Create it with `CREATE TABLE IF NOT EXISTS` in `ensureGroupTables`.
- Add it to the DB interface, with Redis stubs per D15.
- When the group is deleted, delete its rows. Also delete the collection: the link was only living on as the group's.

**Behaviour:**
- **`showCollectionsAdmin`** hides collections that have a `CollectionGroups` row.
- **`/groups/fromcollection`** accepts `{collectionId, days, addViewers bool, linkMode}`. It creates the group, adds the files (as now), adds the viewers as members, records the row, and returns `Added` and `MembersAdded`.
- **`/c?id=`** for a converted collection:
  - **`invite`:** the page renders as today, plus a banner, "This is now a group — sign in to join and add your own", with a **Join** button. For a signed-in non-member it POSTs the join with the group's current token. Anonymous visitors get a "Sign in to join" link that returns to `/join?t=<token>` through `SetReturnTo`.
  - **`require`:** a signed-in member is redirected to `g?id=`. Everyone else goes to `/join?t=<current token>`, which handles sign-in and return.
  - The group's current token is read at request time, so a code reset keeps the old link working.
- **New endpoint `POST /groups/linkmode`** `{id, mode}`, creator and admins only.
- **New endpoint `POST /groups/stoplink`** `{id}`, creator and admins only. It deletes the collection and the row.

**Tests:**
- conversion with `addViewers` adds exactly the signed-in viewers;
- the collection is hidden from the list;
- `/c` in invite mode shows the banner, and joining works;
- `/c` in require mode redirects as specified (anonymous → login with the return cookie, a member → the group);
- linkmode and stoplink are refused for plain members;
- after stoplink, `/c` answers like a deleted collection;
- deleting the group removes the collection;
- the tables are still created idempotently at schema 17.

- [ ] Tests first, then RED, implement, GREEN. Run the full suite.
- [ ] Commit, then push.

---

### Controller

- **Branding**, applied to the preview copy now and to live at deploy: the Users icon becomes `bi-person-gear`, so it no longer mirrors Groups' `bi-people-fill`; bump `version.txt`.
- **Merge and rebuild:** merge R1–R3, then run R4. Then regenerate the assets, run the full suite and the e2e scripts, rebuild the preview, and do a browser pass on what changed.
- **Ask Franco for a phone pass on:**
  - Save to Photos from the viewer, the menu and the selection bar;
  - the dates;
  - Compare;
  - the members panel;
  - the phone drop zone.
