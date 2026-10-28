# Contributing to FGS Share

FGS Share is a personal fork of [Gokapi](https://github.com/Forceu/Gokapi), maintained for one
instance. Pull requests are welcome, but the scope is narrow: changes to what the fork adds
(previews, groups, profiles, the blocklist, the activity feed, the notifier, `deploy/`).

**A change that would help every Gokapi user belongs in [Gokapi](https://github.com/Forceu/Gokapi)
itself.** It reaches FGS Share when upstream is merged in.

## Security first

Do **not** open a public issue for a vulnerability. Report it privately; see [SECURITY.md](SECURITY.md).

## Getting started

1. Fork the repository and clone it.
2. Use Go 1.25+ (or the version in `go.mod`), or run everything in Docker with `build/dev/gotest.sh`.
3. Create a branch for your change.

## Standards

- **Style:** `go fmt ./...`, standard Go idioms, and code that reads like the code around it.
- **Tests:** every change comes with a test. Run `build/dev/gotest.sh ./...`. `go test` needs
  `-tags=test`, or it reports "no test files" and looks like a pass. Behaviour you can see in a browser
  also gets a line in the relevant `build/e2e/` script.
- **Static assets:** run `go generate ./...` (the test script does) and commit the regenerated minified files.
- **Schema:** never bump the database schema. New tables are created with `CREATE TABLE IF NOT EXISTS`,
  so an older image still runs on the same database.
- **Documentation:** update `README.md` and `docs/` when behaviour or procedures change.
- **No real data:** this repository is public. Never commit secrets, real people's names, emails or files, or
  copies of a live database. Tests and screenshots use made-up people and generated images.

## Submitting a PR

- Keep each PR to one change, and describe what changed, why, and how you tested it.
- Make sure the tests pass.
