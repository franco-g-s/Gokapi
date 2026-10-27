> **This is a personal fork of [Forceu/Gokapi](https://github.com/Forceu/Gokapi)**, the build behind
> share.franco.international: a private photo and file share for friends and family. The default
> branch, `feature/collections`, holds the changes. `master` tracks upstream untouched. Everything
> below the line is upstream's README.
>
> **What the fork adds** (PRs #1–#4; the design is in `docs/design/`, the plans in `docs/superpowers/plans/`):
> - **Previews.** A hardened vips/ffmpeg worker makes thumbnails, views and video posters. A single access rule, `access.CanView`, governs every preview route. A shared file-list component has grid, board and list views and a full-screen viewer.
> - **Groups.** Join by invite link or code, with creator, member and admin roles. A shared board is ordered by capture time. Exact duplicates are refused and near-copies flagged (perceptual hash). The list ticks off what you have already downloaded.
> - **Large lists.** Keyset-paginated batches. Server-side sort, filter, search and type filters. Selection and bulk actions over the whole set. iPhone "Save to Photos" runs in measured batches. Admins get a read-only "View as…".
> - **People.** Google sign-in profiles, display names and photos. A per-user Settings page. An email/domain blocklist enforced at sign-in, password login and API keys.
> - **Activity feed and notifier.** An admin-only `GET /api/activity`, plus a Python notifier (`deploy/notifier/`) that batches it into quiet summaries and an optional daily digest.
> - **Deployment.** `deploy/` holds the compose file with its hardening, the branding (`deploy/custom/`), the upload-limit script and the notifier's launchd agent.
> - **Tests.** Go unit tests for every change and Playwright end-to-end scripts (`build/e2e/`). These run against seeded, synthetic users. No real data is in this repo.
>
> The database schema stays at upstream's version 17: new tables are created idempotently. Licensed AGPL-3.0, as upstream.

---

# Gokapi

[![Documentation Status](https://readthedocs.org/projects/gokapi/badge/?version=latest)](https://gokapi.readthedocs.io/en/stable/?badge=stable)
[![Go Report Card](https://goreportcard.com/badge/github.com/forceu/gokapi)](https://goreportcard.com/report/github.com/forceu/gokapi)
[![Coverage](https://img.shields.io/badge/Go%20Coverage-83%25-brightgreen.svg?longCache=true&style=flat)](https://github.com/jpoles1/gopherbadger)
[![Docker Pulls](https://img.shields.io/docker/pulls/f0rc3/gokapi.svg)](https://hub.docker.com/r/f0rc3/gokapi/)

**Gokapi** is a modern, self-hosted alternative to Firefox Send that puts you in control of your file sharing. Built with Go, it combines simplicity with powerful features like automatic file expiration, end-to-end encryption, and flexible cloud storage support.

### Available for:

- **Bare Metal** (Linux/macOS/Windows)
- **Docker**: [View on Docker Hub](https://hub.docker.com/r/f0rc3/gokapi)

## Features

- **Expiring file shares:** Automatically removed after a set number of downloads or days
- **User management with roles:** Fine-grained permission control, only registered users can upload
- **File requests:** A shareable URL lets external parties upload files, visible only to the URL’s creator
- **Collections:** Group several files behind a single public link, optionally password protected
- **File deduplication:** Identical files use no extra space
- **Cloud storage support:** AWS S3 (or S3 compatible like Backblaze B2), optional
- **Built-in encryption:** Including end-to-end encrypted uploads
- **OpenID Connect support:** Integrate with identity providers like Authelia or Keycloak
- **REST API:** For automation and integration into other systems
- **Customizable UI:** Adjust look and feel with custom CSS and JavaScript

---

## Getting Started

You can deploy Gokapi in seconds using Docker or directly on your system.

[Installation Guide](https://gokapi.readthedocs.io/en/latest/setup.html)  
[Usage Instructions](https://gokapi.readthedocs.io/en/latest/usage.html)

**Want to give it a try?**

Start Gokapi instantly with Docker:

```bash
docker run --rm \
  --name gokapi \
  -v gokapi-data:/app/data \
  -v gokapi-config:/app/config \
  -p 127.0.0.1:53842:53842 \
  -e TZ=UTC \
  docker.io/f0rc3/gokapi:latest
```

Then visit ``http://localhost:53842/setup`` and follow the setup wizard.



## Screenshots
**Main Menu**

<a href="https://github.com/user-attachments/assets/d805a88b-dc74-4c39-bed6-ec31b9c3e17f" target="_blank">
  <img width="300" alt="image" src="https://github.com/user-attachments/assets/d805a88b-dc74-4c39-bed6-ec31b9c3e17f" />

</a>

**File Requests**

<a href="https://github.com/user-attachments/assets/a6565cf8-bd2d-4027-a150-673aa93d4502" target="_blank">
 <img width="300"  alt="image" src="https://github.com/user-attachments/assets/a6565cf8-bd2d-4027-a150-673aa93d4502" />
</a>



**User Overview**

<a href="https://github.com/user-attachments/assets/cbc738e4-75ae-4647-8178-da735f74a86f" target="_blank">
  <img width="300" alt="image" src="https://github.com/user-attachments/assets/cbc738e4-75ae-4647-8178-da735f74a86f" />
</a>


**API Overview**

<a href="https://github.com/user-attachments/assets/c480af8e-772c-4f8b-9f0e-28c8aceb9b49" target="_blank">
  <img width="300" alt="image" src="https://github.com/user-attachments/assets/c480af8e-772c-4f8b-9f0e-28c8aceb9b49" />
</a>

**Status Overview**

<a href="https://github.com/user-attachments/assets/70d5ab07-e60f-48d5-8739-fa038129e5ae" target="_blank">
<img width="300" alt="image" src="https://github.com/user-attachments/assets/70d5ab07-e60f-48d5-8739-fa038129e5ae" />
</a>


**Download Link**

<a href="https://github.com/user-attachments/assets/fd9c032b-733d-4657-9f42-f751b2634e02" target="_blank">
  <img width="300" alt="image" src="https://github.com/user-attachments/assets/fd9c032b-733d-4657-9f42-f751b2634e02" />

</a>

---

## System Requirements

### Minimum
- **CPU**: 1 core
- **RAM**: 256 MB
- **Storage**: 100 MB + file storage
- **OS**: Linux, macOS, Windows

### Recommended  
- **CPU**: 2+ cores
- **RAM**: 512 MB+
- **Storage**: SSD strongly recommended

## Development

### Building from Source

```bash
# Clone the repository
git clone https://github.com/Forceu/gokapi.git
cd gokapi

# Build the binary
make build

# Run tests
make test

# Build Docker image
docker build -t gokapi:local .
```

## License

This project is licensed under the AGPL3 - see the [LICENSE.md](LICENSE.md) file for details

## Contributors
<a href="https://github.com/forceu/gokapi/graphs/contributors">
  <img src="https://contributors-img.web.app/image?repo=forceu/gokapi" />
</a>



## Donations

As with all Free software, the power is less in the finances and more in the collective efforts. I really appreciate every pull request and bug report offered up by our users! If however, you're not one for coding/design/documentation, and would like to contribute financially, you can do so with the link below. Every help is very much appreciated!

[![paypal](https://img.shields.io/badge/Donate-PayPal-green.svg)](https://www.paypal.com/cgi-bin/webscr?cmd=_donations&business=donate@bulling.mobi&lc=US&item_name=BarcodeBuddy&no_note=0&cn=&currency_code=EUR&bn=PP-DonationsBF:btn_donateCC_LG.gif:NonHosted) [![LiberaPay](https://img.shields.io/badge/Donate-LiberaPay-green.svg)](https://liberapay.com/MBulling/donate)

Powered by [Jetbrains](https://jb.gg/OpenSourceSupport)





