# Gokapi email notifier

`gokapi_notify.py` polls Gokapi's `GET /api/activity` feed once a minute and
sends a few clear emails instead of one per upload. It is Python 3.9, stdlib
only, and runs on the Mac mini under `/usr/bin/python3`.

## What it sends

To the admin (`mail_to`):

- **Immediately**, in one email per poll:
  - `[FGS] View as…: <admin> viewing as <target>` and `… stopped: <reason>`.
    These are always sent, even though the actor is the admin, because one
    he did not trigger is a break-in signal.
  - `[FGS] Warning: <n> issue(s)` for disk-space and preview failures.
  - `[FGS] New person: <name> (<email>)`.
  - `[FGS] New group: <group> by <name>`, for groups created by non-admins.
  - When several kinds arrive in one poll, they share one subject, joined
    with `; `.
- **A summary**, `[FGS] Activity: <sections>`, of uploads, downloads, joins
  and blocked sign-ins by non-admins.
  - It is sent after `quiet_minutes` without such activity, or
    `max_hold_minutes` after the first held event, whichever comes first.
  - Sections:
    - one per group, in order of first activity;
    - then `Your files`;
    - then `<name>'s files`;
    - then `Blocked sign-ins`.
  - It is plain text.
  - Admin uploads, downloads, joins and groups never appear, and do not
    reset the quiet timer.

To friends: `Your files were downloaded`, at most once a day at
`friends_digest_time` (Europe/Zurich).

- Only for non-admin accounts that have an email address and have
  "Email me when my files are downloaded" on.
- It covers what was collected since their last digest. If nothing was
  collected, no email is sent.
- A person's own downloads never count.
- If someone switches the setting off before 19:00, what was held for them is
  dropped.

The notifier never emails about itself:

- **Feed failures** are logged once when they start and once when they
  recover.
- **SMTP failures** keep the outbox and back off by 1, 5, 15, then 60
  minutes. They are logged once per failure streak.

## Design

`step(state, feed, now_utc, cfg) -> (state, emails)` is a pure function that
folds one feed page into the state.

- `poll_once` fetches every page (`more: true` is followed), runs `step`, and
  **persists state before sending**.
- It then sends from the outbox, removing each email only once it has been
  sent.
- A crash therefore re-sends at most one step's emails and never loses
  events.
- `deliver()` is the only place mail leaves. It applies `redirect_all_to`,
  and refuses (`UnsafeConfig`) to send when `api_url` is not the live feed
  and no redirect is set.

## Config (`notify.config.json`, next to the script by default)

| Key | Default | Meaning |
|---|---|---|
| `api_url` | `http://127.0.0.1:53842/api/activity` | The feed. Anything else requires `redirect_all_to`. |
| `api_key` | required | Admin API key with the `ManageLogs` permission. |
| `site_url` | `https://share.franco.international` | Used in email links (`<site>/settings`). |
| `smtp_user`, `smtp_app_password` | required | Gmail account and app password (SMTP over SSL, port 465). |
| `mail_to` | required | The admin's address. |
| `redirect_all_to` | unset | Rewrites every recipient, header and envelope. The original goes in `X-Original-To`. Set it for previews. |
| `deliver` | `smtp` | `smtp`, or `dir:<path>` to write `.eml` files instead. |
| `poll_seconds` | 60 | |
| `quiet_minutes` | 60 | Summary after this much quiet. |
| `max_hold_minutes` | 180 | Summary at the latest this long after the first held event. |
| `friends_digest_time` | `19:00` | Local time of the friends' daily digest. |
| `timezone` | `Europe/Zurich` | |
| `state_path` | `.notify-state-v2.json` next to the config | |
| `max_friend_emails_per_day` | 100 | Owners over the cap wait for the next day. |

`notify.config.example.json` lists every key.

**The config, the state file and every credential stay on the host and are
never committed.** `.gitignore` here excludes `notify.config.json`,
`.notify-state*.json` and `.eml` output.

## Running

```
/usr/bin/python3 gokapi_notify.py [--once] [--test] [--from-start] [--config PATH]
```

- `--test` sends a test email through `deliver` (so the redirect applies) and
  exits.
- `--once` does one poll and exits.
- `--from-start`: with no saved cursor, the first call asks for `since: 0`,
  meaning every retained row. Without it, a missing cursor means "start from
  now".

On the mini it runs as the launchd agent `com.franco.gokapi-notify`
(`~/Library/LaunchAgents/com.franco.gokapi-notify.plist`):

- `/usr/bin/python3 /Users/franco/apps/gokapi/gokapi-notify.py`;
- `KeepAlive`, `RunAtLoad`, `ThrottleInterval` 30;
- working directory `~/apps/gokapi`;
- stdout and stderr to `~/apps/gokapi/notify.log`.

## Cutover from the log.txt notifier

1. Deploy the Gokapi image that has the `Activity` table and `/api/activity`.
   The table is new, so it holds only events since the swap.
2. Create the admin API key with `ManageLogs` on live, and put it in
   `~/apps/gokapi/notify.config.json` as `api_key`. Leave the SMTP values as
   they are.
3. Back up the old script, then copy this one over it:
   `cp gokapi-notify.py gokapi-notify.py.bak.<date>`, then copy
   `deploy/notifier/gokapi_notify.py` to `~/apps/gokapi/gokapi-notify.py`.
4. `launchctl unload` the agent. Run one poll by hand:
   `/usr/bin/python3 ~/apps/gokapi/gokapi-notify.py --once --from-start`.
   This reads the table from row 1, so nothing between the image swap and
   the start is lost. It writes the new state file, `.notify-state-v2.json`.
   The old `.notify-state.json` is not read.
5. `launchctl load` the agent. It runs without `--from-start`. That flag only
   matters while there is no cursor, but keeping it out of the plist means a
   deleted state file starts from now instead of re-reading 30 days.

## Tests

```
cd deploy/notifier && /usr/bin/python3 -m unittest -v
```

- The replay test drives `step` over `fixtures/busy_evening.json`, with a
  fake clock and a fake transport under `deliver`.
- The fixture is a synthetic busy evening: invented people and files on
  2026-03-14.
- `build_fixture()` in the test generates it. Regenerate with
  `/usr/bin/python3 -c "import test_gokapi_notify as t; t.write_fixture()"`.
- The expected emails are written by hand in the test, not generated.
