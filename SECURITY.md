# Security Policy

FGS Share is a personal fork of [Gokapi](https://github.com/Forceu/Gokapi), maintained for one
instance, share.franco.international. Only the current `main` branch is supported; there are no
releases.

## Reporting a vulnerability

Please **do not open a public issue**. Use GitHub's
[**Report a vulnerability**](https://github.com/franco-g-s/fgs-share/security/advisories/new)
on this repository (the **Security** tab), which keeps the report private.

Include what you found, how to reproduce it, and what it exposes. Please do not test against
share.franco.international: it holds real people's files. Run your own copy instead (see the README).

Reports are handled on a best-effort basis by one person.

## Which project to report to

- **In FGS Share's own changes** (previews, groups, collections pages, profiles, the blocklist,
  View as…, the activity feed, the notifier, `deploy/`): report here.
- **In code FGS Share inherits unchanged from Gokapi**: report to
  [Gokapi](https://github.com/Forceu/Gokapi/security/advisories/new). You are welcome to tell this
  repository too, so the fix can be merged in sooner.

Vulnerabilities in third-party dependencies should go to their maintainers.

## Known history

- **2026-10-27:** `POST /groups/add` returned the name of another user's file when refusing to add it,
  to a signed-in user who already knew that file's id. Fixed in `0aae65f` and deployed the same
  day, before the repository was made public.
