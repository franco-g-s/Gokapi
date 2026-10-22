# Part 3 revisions (after Franco's preview, 2026-10-22)

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** these are Franco's change requests from testing the Part 3 preview. Once they are done, Part 3 deploys.

**Base:** `feature/part3` at `d3313b7`. The binding plan is `docs/superpowers/plans/2026-10-21-part3-batching-photos-viewas.md`. Its Global Constraints apply unchanged: Go only through `build/dev/gotest.sh`, textContent for names, POST + JSON mutations, stdout-only logging, schema 17, the commit trailer, source-only commits, and never touching live. Use the Colima docker context explicitly (`docker --context colima`) for every throwaway container on 53900, because Docker Desktop also runs on the mini.

## Decisions (controller, on Franco's requests)

| # | Request | Decision |
|---|---|---|
| V1 | "Sort order options and filters in their own dropdown everywhere" | **Every shared file list** gets one **"Sort & filter"** dropdown button in the toolbar, replacing the loose controls.<br>**Sort:** a radio list of the list's sort keys, plus Ascending/Descending. The keys are Name, Uploaded, Taken and Size, and Expires and Downloads where those columns show. It applies in **every view**, not only List; the List headers stay clickable and stay in sync.<br>**Filters:** the person filter and "Hide what I've downloaded" on group pages; My files / Everything on the Files tab; and a **new "Type" filter** (All · Photos · Videos · Other), answered by the server (`kind=image\|video\|other`, from the preview kind, falling back to the extension, the same rule as `FGS.isPhotoOrVideo`).<br>**Search** stays visible in the toolbar in every view; it no longer clears when you switch views.<br>The button shows a count badge when filters are active, plus a "Reset" entry. The choices are remembered per list (as the view is), except person (never remembered, as before). |
| V2 | "View as" banner covers the sidebar button and user icon | While it is on, the banner's height is applied as an offset: `body` gets `padding-top`, and every fixed top element (the sidebar toggle, the sidebar, the user badge or menu, sticky bars) is moved down by `--fgs-viewas-h`. Nothing is covered at 390 px or on desktop. |
| V3 | Logout into a menu on the user icon | The top-right user badge becomes a button. It opens a small dropdown with the full name and **Logout**. Logout leaves the nav, so the branding sidebar drops it automatically. Part 4 will add Settings to this menu. |
| V4 | "View as…" only on the Users tab | Remove the `/viewas` page, its nav/sidebar link, its template and its route. **Keep** `/viewas/start` and `/viewas/end` and every guard rule. On the Users tab, "View as…" is a small button next to each eligible user's name (non-admin, not yourself). This supersedes the spec's "sidebar switch" (Franco, 2026-10-22). |
| V5 | Users tab cleanup | Per row:<br>• "View files" goes (View as… replaces it);<br>• "User group" is renamed **"User type"** everywhere on the page, column header and edit dialog included;<br>• **permissions** move into a per-row **"Permissions" dropdown/popover** with the same toggles and the same API calls;<br>• the row is name (+ View as…), user type, last online, uploads/size/groups as today, and an actions "⋯".<br>Do not change what an admin is allowed to do. |
| V6 | Ban user/email | **Not built now:** it was a question. Answered in chat. It is a candidate for Part 4. |
| V7 | Select button reachable while scrolled | The **whole toolbar** (count, Sort & filter, search, layout, Select) **and the selection bar** become sticky at the top of the viewport (`position: sticky`, below the View-as offset), compact on phones. |
| V8 | Full-screen preview while selecting | In select mode, each tile and row shows a small **expand** button (`aria-label="Open {name}"`) that opens the viewer without changing the selection. A viewer opened while selecting shows a **"Selected" checkbox** for the current item, which toggles it in the list's selection (both selection modes). |
| V9 | Grid size slider | The slider sets the **number of columns**, not a pixel size:<br>• the left end is the most columns that fit at ≥ 96 px per tile; the **right end is 1 column**, on every device;<br>• `step=1`, so it snaps between steps;<br>• a **default stop** (today's 160 px-equivalent column count for that width, recomputed on resize) is marked with a tick (`<datalist>`), and the thumb snaps to it when released within one step of it;<br>• the remembered value is a column count, clamped on resize.<br>Board's masonry uses the same column count. |
| V10 | Deploy without probe numbers | Franco asked to deploy after these changes. The Save to Photos constants keep their defaults (20 files, 1.5 GB per batch, 500 MB per video), and the comment says "defaults — not yet measured". The probe merge is reverted before the PR. A later measurement is a one-line change. |

## Tasks

| Task | Worktree / branch | Runs | Owns |
|---|---|---|---|
| **R1** Sort & filter dropdown + Type filter (V1) | `p3r-sort` / `p3r/sort` | wave 1 | `FileList.go` (`kind`), `FileListFeeds.go`, `FileBrowser.go`, `html_file_browser.tmpl`, `media.js` (`initFileBrowser` toolbar/query), `media.css`, their tests |
| **R2** Chrome and Users tab (V2–V5) | `p3r-users` / `p3r/users` | wave 1, parallel with R1 | `viewas.js`, `ViewAsPages.go` (+ tests, route test), `html_viewas.tmpl` (delete), `html_header.tmpl`, `html_users.tmpl`, `admin_ui_users.js`, `Webserver.go` (`/viewas` route removal only), the user-menu CSS (inline in the header template or a new `css/usermenu.css`; **not** `media.css`) |
| **R3** Sticky bars, select-mode preview, column slider (V7–V9) | `p3r-ux` / `p3r/ux` | wave 2, after R1 merges | `html_file_browser.tmpl`, `media.js` (toolbar, selection, viewer, slider), `media.css`, `html_media_shared.tmpl` (viewer checkbox) |
| **Release** | controller | after R3 | V10, probe revert, assets, e2e, walkthrough, short whole-branch review, deploy, PR, cleanup, Part 4 prompt |

Wave 1 uses Go tests only for **R2**. **R1** may use the throwaway container on 53900 and the browser (it is the only one doing so in wave 1). **R3** may use both.

### R1 checks (browser, seeded with `build/e2e/seed_part3.py`)

- [ ] **Group page as B:** Sort & filter → Name ascending → the first tile is `p3-000…` in Board and Grid too. Then Type = Photos keeps 250; Type = Videos shows `Files: 0` and "No matching files".
- [ ] **Badge and reset:** person A + Hide gives the badge "2"; Reset clears both, and `Files: 250`.
- [ ] **Remembered choices:** reload. Sort and hide are remembered; person is not.
- [ ] **Files tab:** My files / Everything lives in the dropdown and still works; search works in Grid view.
- [ ] **Tests:** Go `TestListMatchingFilters` is extended for `kind`; the feed answers 400 on `kind=bogus`. e2e groups 91/91, collections 56/56, part3 all PASS.

### R2 checks

- [ ] **Go:**
  - `/viewas` answers 404;
  - `TestViewAsRoutes` is still green;
  - the Users page renders "View as…" only on eligible rows;
  - the header renders no Logout nav link and a user-menu button with Logout.
- [ ] **Controller browser check after merge:**
  - **View-as banner:** with View as… on, at 390×844 and 1280×800, the sidebar toggle and the user icon are fully visible below the banner (`getBoundingClientRect().top` ≥ the banner's bottom);
  - **user menu:** the menu opens and Logout logs out;
  - **Users tab:** has no "View files" button, says "User type" in place of "User group", and the per-row Permissions popover toggles a permission (API call succeeds).

### R3 checks (browser)

- [ ] **Sticky bars:** scrolled to item 200 on the group page, the Select button is visible and works, and the selection bar stays visible.
- [ ] **Preview while selecting:** in select mode, tap a tile's expand button. The viewer opens, the tile's selection is unchanged, and "Selected" in the viewer toggles it.
- [ ] **Slider:**
  - at 390×844 the slider's right end gives 1 column, its left end ≥ 3 columns, and its default stop is the previous default look;
  - dragging snaps to integers, and releasing next to the default snaps to it;
  - on desktop the right end is also 1 column.
- [ ] **No regressions:** e2e all green; Save to Photos and the viewer are unchanged otherwise.

## Rulings during execution

(Controller fills this in.)
