.. _fgsshare:

=====================
What FGS Share adds
=====================

FGS Share is a fork of Gokapi that turns it into a shared photo album for friends and family. Everything in
the rest of this guide still applies. This page covers what the fork adds on top of it, and how each part
behaves.

The fork was built in four parts between 19 and 24 October 2026. The design is in
``docs/design/2026-10-19-group-sharing-design.md``, and each part's plan, with the decisions taken during the
work and its deploy record, is in ``docs/superpowers/plans/``.

.. note::
   The database schema stays at Gokapi's version 17. Every new table (``Previews``, ``Groups``,
   ``GroupMembers``, ``GroupFiles``, ``Downloads``, ``Activity`` and the rest) is created with
   ``CREATE TABLE IF NOT EXISTS`` at start-up. An older image therefore runs on the same database and simply
   ignores them, which makes rolling back an image swap.

.. contents:: On this page
   :local:
   :depth: 1


*********
Previews
*********

A background worker makes display-only copies of every photo and video:

* a 480 px thumbnail, a 1600 px view, and for videos a poster frame;
* the capture time, the rotated dimensions, the average colour (shown while a thumbnail loads) and a
  perceptual hash (used to find near-copies in groups).

Originals are only ever read, never modified. Previews are regenerated once when the generator's version
changes, and existing files are backfilled at start-up.

**Requirements.** The worker runs ``vips``, ``vipsheader``, ``ffmpeg`` and ``ffprobe`` as subprocesses. The
Docker image includes them. A native install needs them on the ``PATH``, otherwise files still upload and
download normally but show a file icon instead of a preview.

**What gets a preview.** Photos and videos on local storage. Files stored on S3 and end-to-end encrypted files
are skipped, because the server cannot read them. Server-side encrypted files are decrypted to a temporary
file for the worker and the temporary file is removed afterwards.

**Who can see a preview.** One rule, ``access.CanView``, decides for every preview route
(``/p/thumb``, ``/p/view``, ``/p/poster``, ``/p/video``, ``/p/info``), in this order:

* the uploader and users with the *list other uploads* permission always can;
* an upload made through a file request is visible to nobody else;
* members of an open group the file is in can;
* on a collection page, visitors can if the collection's password, if any, has been entered;
* otherwise anyone who has the file's link can, unless the file has a password that has not been entered.

Viewing never counts as a download. For that reason a file with a download limit streams as video only to its
uploader and users with *list other uploads*. Everyone else would otherwise watch it without using up the
limit. End-to-end encrypted files never get previews.

The worker runs each tool with a time limit (60 s for photos, 120 s for videos), and ``ffmpeg`` with one thread
and file access only, because uploads are untrusted input.


**********************
The shared file list
**********************

Files, Upload, the Collections admin, File requests, group pages and public collection pages all use the same
list:

* **Grid**, **Board** (in shooting order, by capture time) and **List** views, remembered per page or group.
  A slider sets the number of columns, down to one.
* A full-screen **viewer** with swipe, arrow-key and trackpad navigation, and a *Download original* button.
* A **⋯ menu** on each file: rename, copy link or hotlink, email, QR code, replace, add to a collection or
  group, delete. Deleting can be undone without reloading the page.
* **Select mode** with bulk actions: download as zip, Save to Photos, one link for the selection, add to a
  collection, change expiry and download limits, delete.

Lists render the first 100 files and load the rest in batches as you scroll. Sorting, filtering, searching and
counting are answered by the server over the whole set, so they are correct for files that have not been
loaded yet:

* **Sort & filter**: by person, by type (photos, videos, other), *My files* or *Everything*, and *Hide what
  I've downloaded*;
* **Select all (N)** and **Select everything new to me** cover the whole list, and every bulk action applies
  to the full selection.


******
Groups
******

A group is a shared board that several people upload into, for one trip or one event.

**Creating and joining.** Anyone signed in can create a group from the *Groups* tab. It gets an invite link and
a short code (for example ``QRH-G2R``). Opening the link, or entering the code, joins the group, after signing
in if needed. Joins are rate-limited.

**Lifetime.** A group closes after the number of days chosen at creation, and its creator can extend it.
Files uploaded into a group stay until the group closes.

**Roles.**

* The **creator** can rename, extend, reset the code (old links stop working), remove members, and delete the
  group.
* **Members** can upload, download, and leave.
* **Admins** can do everything the creator can, in every group, without being a member.

**Uploading.** Drop files onto the group page, choose *Upload to: <group>* on the Upload page, or use *Add to
group…* from any file list. You can add only your own files (admins can add anyone's).

**Duplicates.** A byte-identical copy of a file already in the group is refused before it uploads. Near-copies
(the same photo re-encoded, or a burst) are flagged by perceptual hash, with *Compare*, *Keep both* and
*Remove*. *Check for duplicates* runs the comparison over the whole group.

**Downloads.** Ticks mark what you have already downloaded, *Hide what I've downloaded* filters it out, and
*Select everything new to me* selects the rest.

**Collections and groups.** *Turn into a group* converts an existing collection. The collection is hidden,
people who had opened it are added to the group, and its old link either invites people or asks them to join.
The conversion can be stopped. The *Shared with me* list shows the collections you have opened.


**********************
iPhone: Save to Photos
**********************

Safari cannot save many files to Photos at once, so *Save to Photos* plans the selection into batches and
fetches one batch at a time. The default limits are 20 files and 1.5 GB per batch, and 500 MB per video;
these are defaults, not measured limits. Larger videos go to a separate *Download those as .zip*, with an
explanation, and a retry fetches only the files that failed.

*Download all* on a collection works through a zip or Save to Photos, with a help sheet.

.. note::
   iOS drops the date a photo was taken when importing through Save to Photos or the Files app, even though
   the originals carry it. The help sheet says so.


******
People
******

**Display names.** Everyone has a shown name instead of their email prefix. It comes from Google at sign-in
and can be changed on the *Settings* page. Names must contain a visible character, and control and formatting
characters are refused.

**Photos.** With Google sign-in, the profile photo (the ``picture`` claim) is fetched at sign-in, at most once
a day per person, and served by ``/avatar`` only to the person themself, to admins and to their group
co-members. *Use initials instead of my Google photo* hides it from everyone but its owner. Names and photos
fill in only after each person's next sign-in.

**Settings** (``/settings``, from the user menu): display name, photo or initials, and *Email me when my files
are downloaded*. The email switch also appears on the Upload page.


*********
Blocklist
*********

Admins can block an exact email address or a whole domain (``@example.com``) from the Users page. A blocked
address is refused:

* at Google sign-in, before an account is created;
* at password login;
* on every API key belonging to that user;
* on every request of an existing session.

The check fails closed: if the blocklist cannot be read, the request is refused.

.. note::
   With Google sign-in and *only registered users* off, any Google account can sign in, so deleting a user is
   not a ban. Use the blocklist for that.


************
View as…
************

Admins can see the site as another user, from the *View as…* button next to each user on the Users tab. It is
read-only: a deny-by-default guard refuses every change and every download. A banner shows whose view is
active. The session ends on exit, logout, switching user, one hour idle, or a restart, and every start and end
is logged as an ``[auth]`` line on the Status page and in the activity feed.


*********************************
Activity feed and notifications
*********************************

The ``Activity`` table records uploads, downloads (with how the file was reached: group, collection, file or
zip), groups created and joined, first sign-ins, blocked sign-ins, low disk space, failed previews, and View as
start and end. Recording is best-effort: a failed write never breaks an upload or a download.

The feed is read with ``GET /api/activity``. It is admin-only and needs an API key with the *Manage logs*
permission. The cursor is passed in a ``since`` header, not in the query string. The request and response are
in the API documentation (the *API* menu, or ``openapi.json``).

The **notifier** in ``deploy/notifier/`` is a small Python program that polls the feed and sends email:

* new people, new groups, warnings and View as are emailed to the admin at once;
* everything else goes into one summary after an hour of quiet, or at most every three hours, grouped by group
  and person;
* people who turned on *Email me when my files are downloaded* get at most one email a day, at 19:00, and only
  if someone downloaded something.

Its configuration, credentials and how to run it are in ``deploy/notifier/README.md``.


*****************
Branding
*****************

The look of share.franco.international is plain Gokapi customisation (see :ref:`customising`), kept in
``deploy/custom/``: ``custom.css``, ``admin.js``, ``landing.html`` (a public sign-in page, served at
``/custom/landing.html``) and ``version.txt``. The fork added ``landing.html`` to the files ``/custom/`` serves. ``deploy/set-max-filesize.sh`` changes the upload limit and keeps the figure shown under the
drop zone in step with it.


********
Security
********

On top of Gokapi's own protections:

* group and collection endpoints accept only ``POST`` with ``Content-Type: application/json``;
* the group join form checks the request's ``Origin``;
* ``/custom/`` serves only the named branding files, never a directory listing or other files;
* a refused file in *Add to group* returns no name unless it is your own (fixed 2026-10-27).

Report vulnerabilities as described in ``SECURITY.md``.
