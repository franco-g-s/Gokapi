.. _index:

===========================
FGS Share
===========================

FGS Share is a self-hosted photo and file share for friends and family. It is a fork of
`Gokapi <https://github.com/Forceu/Gokapi>`_, a lightweight server for files that expire after a set number of
downloads or days, similar to the discontinued Firefox Send: only registered users upload, and anyone with a
link can download.

The fork adds what a shared photo album needs: previews and a viewer, groups that several people upload into,
display names and photos, a blocklist, and an activity feed with quiet email notifications. It runs
share.franco.international. The source is at https://github.com/franco-g-s/fgs-share.

What FGS Share adds (see :ref:`fgsshare`):

* **Previews** — thumbnails, a full-screen viewer and video posters, made with ``vips`` and ``ffmpeg``
* **Groups** — a shared board joined by link or code, with duplicate and near-copy detection
* **Large lists** — batches of 100 with server-side sort, filter, search and selection
* **iPhone** — Save to Photos in measured batches
* **People** — display names, Google photos, a Settings page and a blocklist
* **View as…** — admins see the site as another user, read-only
* **Activity feed** — ``GET /api/activity`` and a notifier that emails only what matters

Inherited from Gokapi:

* **Expiring links** — files are deleted after a configurable number of downloads or after a set number of days
* **File Requests** — generate a link that lets external users upload files to your server
* **Multi-user support** — multiple accounts with granular per-user and per-API-key permissions
* **Deduplication** — identical files are stored only once
* **End-to-end encryption** — optional client-side encryption so even a compromised server cannot read file contents
* **S3-compatible cloud storage** — store files on AWS S3, Backblaze B2, or any S3-compatible provider instead of locally
* **CLI tool** — upload and download files directly from the command line
* **REST API** — full API for scripting and third-party integrations
* **Easy customisation** — change the look with plain CSS and JavaScript, no recompilation needed

Most of this guide is Gokapi's own documentation and applies unchanged. Where FGS Share behaves differently,
the page says so.


Contents
========

.. toctree::
   :maxdepth: 2

   setup
   usage
   fgs-share
   update
   advanced
   troubleshooting
   examples
   contributions
   changelog
