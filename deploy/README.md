# Deployment

How FGS Share runs on share.franco.international: a Docker container on a Mac mini (Colima), bound to
loopback and reached from outside only through a Cloudflare Tunnel.

| Path | What it is |
|---|---|
| `docker-compose.yml` | The service: hardening (`no-new-privileges`, `cap_drop: ALL`, pids, memory and CPU caps), loopback-only port, Cloudflare-safe chunk size. The image is built locally from this repo's `Dockerfile` and tagged `gokapi-fgs:<part>-<date>`. |
| `custom/` | Branding, mounted at `/app/custom`: `custom.css`, `admin.js`, the public `landing.html`, and `version.txt` for cache busting. Everything here is served to the world, so it must never hold secrets. |
| `set-max-filesize.sh` | Changes the per-file upload limit in `config.json`, updates the figure `custom.css` shows, bumps `version.txt` and restarts. |
| `notifier/` | The email notifier that reads `/api/activity`. See its own README. `com.franco.gokapi-notify.plist.example` is its launchd agent. Replace `/Users/YOU`. |

Not in the repo, on purpose: `config.json` and the data volumes, `notify.config.json` (SMTP
credentials and API key), the notifier's state and log, and database backups.
