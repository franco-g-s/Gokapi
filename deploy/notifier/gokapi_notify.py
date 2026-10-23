#!/usr/bin/env python3
"""Email notifier for Gokapi, fed by GET /api/activity.

Polls the activity feed and sends a few clear emails:

  * to the admin (mail_to), immediately: new people, new groups by others,
    warnings (all of one poll in one email) and every "View as..." start/stop;
  * to the admin, as a summary: uploads, downloads, joins and blocked sign-ins
    by non-admins, sent after an hour of quiet or three hours after the first
    held event, whichever comes first;
  * to friends who asked for it: one digest a day at 19:00 about downloads of
    their own files.

All decisions are made in pure functions (step, flush_outbox's bookkeeping);
only main() touches the network, the clock and the disk.

  --test        send a test email now (through deliver) and exit
  --once        do a single poll and exit
  --from-start  with no saved cursor, read the feed from its oldest row (cutover)
  --config PATH config file (default: notify.config.json next to this script)

Python 3.9, standard library only.
"""
import http.client
import json
import os
import re
import smtplib
import socket
import sys
import time
import urllib.error
import urllib.request
from datetime import datetime, timezone
from email.message import EmailMessage
from email.utils import formatdate, make_msgid, parseaddr

try:
    from zoneinfo import ZoneInfo
except ImportError:  # pragma: no cover - 3.9 has zoneinfo
    ZoneInfo = None

HERE = os.path.dirname(os.path.abspath(__file__))
LIVE_API_URL = "http://127.0.0.1:53842/api/activity"

DEFAULTS = {
    "api_url": LIVE_API_URL,
    "site_url": "https://share.franco.international",
    "redirect_all_to": "",
    "deliver": "smtp",
    "poll_seconds": 60,
    "quiet_minutes": 60,
    "max_hold_minutes": 180,
    "friends_digest_time": "19:00",
    "timezone": "Europe/Zurich",
    "state_path": "",
    "max_friend_emails_per_day": 100,
}
REQUIRED = ("api_key", "smtp_user", "smtp_app_password", "mail_to")

OUTBOX_CAP = 200
BACKOFF_MINUTES = (1, 5, 15, 60)
MAX_PAGES_PER_POLL = 50

IMMEDIATE_ALWAYS = {"first_signin", "warning_disk", "warning_preview", "viewas_start", "viewas_end"}
HELD_KINDS = {"upload", "download", "group_joined", "signin_blocked"}
VIA_LABELS = {"file": "via link", "collection": "via link", "zip": "via zip",
              "api": "via API", "group": "via group"}
NOUNS = {"image": ("photo", "photos"), "video": ("video", "videos"), "file": ("file", "files")}
ELLIPSIS = "…"
ADDRESS_RE = re.compile(r'^[^@\s<>,;:"()\[\]]+@[^@\s<>,;:"()\[\]]+\.[^@\s<>,;:"()\[\]]+$')
SEND_ORDER = {"immediate": 0, "test": 0, "summary": 1, "friend": 2}
FOOTER = ("You get this because \"Email me when my files are downloaded\" is on. "
          "Turn it off: %s/settings")


class UnsafeConfig(Exception):
    """deliver() refuses: a non-live feed without redirect_all_to could mail real people."""


class FeedError(Exception):
    """The feed could not be read: unreachable, non-200, bad JSON, or not Result OK."""


def log(msg):
    print("%s  %s" % (datetime.now().strftime("%Y-%m-%d %H:%M:%S"), msg), flush=True)


def _nolog(msg):
    pass


# ---------------------------------------------------------------- config/state

def with_defaults(cfg):
    out = dict(DEFAULTS)
    out.update(cfg)
    return out


def load_config(path):
    with open(path) as fh:
        cfg = with_defaults(json.load(fh))
    missing = [k for k in REQUIRED if not cfg.get(k)]
    if missing:
        sys.exit("%s is missing values for: %s" % (path, ", ".join(missing)))
    if "PASTE" in str(cfg["smtp_app_password"]) or "PASTE" in str(cfg["api_key"]):
        sys.exit("%s still has a placeholder - set the real values before running." % path)
    base = os.path.dirname(os.path.abspath(path))
    cfg["state_path"] = os.path.join(base, cfg["state_path"] or ".notify-state-v2.json")
    return cfg


def new_state():
    return {
        "cursor": None,
        "held": [],
        "held_first_at": None,
        "held_last_at": None,
        "friends": {},
        "friend_last_digest_date": None,
        "outbox": [],
        "outbox_dropped": 0,
        "failing": {"feed": None, "smtp": None},
        "send_failures": 0,
        "next_send_at": None,
        "users": [],
        "pending_immediate": [],
        "outbox_poisoned": 0,
    }


def load_state(path):
    state = new_state()
    try:
        with open(path) as fh:
            state.update(json.load(fh))
    except FileNotFoundError:
        pass
    return state


def save_state(path, state):
    tmp = path + ".tmp"
    with open(tmp, "w") as fh:
        json.dump(state, fh)
    os.replace(tmp, path)


# ---------------------------------------------------------------- formatting

def size_si(n):
    """helper.ByteCountSI style: base 1000, one decimal."""
    n = int(n or 0)
    if n < 1000:
        return "%d B" % n
    div, exp = 1000, 0
    m = n // 1000
    while m >= 1000:
        div *= 1000
        exp += 1
        m //= 1000
    return "%.1f %sB" % (n / float(div), "kMGTPE"[exp])


def people_phrase(names):
    if not names:
        return ""
    if len(names) == 1:
        return names[0]
    if len(names) <= 3:
        return ", ".join(names[:-1]) + " and " + names[-1]
    return "%s, %s and %d others" % (names[0], names[1], len(names) - 2)


def _capital(s):
    return s[:1].upper() + s[1:]


def _plural(n, kind):
    one, many = NOUNS.get(kind, NOUNS["file"])
    return "%d %s" % (n, one if n == 1 else many)


def _times(n):
    return "once" if n == 1 else "%d times" % n


def _one_line(s):
    """Display names are user input: never let one break a header or a line."""
    return " ".join(str(s).replace("\r", " ").replace("\n", " ").split())


def _users_by_id(users):
    out = {}
    for u in users or []:
        try:
            out[int(u.get("id", 0))] = u
        except (AttributeError, TypeError, ValueError):
            continue
    return out


def _name(uid, users):
    uid = int(uid or 0)
    if uid == 0:
        return "someone"
    u = users.get(uid)
    if not u:
        return "someone (#%d)" % uid
    return _one_line(u.get("name") or (u.get("email") or "").split("@")[0] or "someone (#%d)" % uid)


def _is_admin(uid, users):
    u = users.get(int(uid or 0))
    return bool(u and u.get("admin"))


def valid_address(addr):
    """A plain local@domain.tld address, nothing else (parseaddr must agree)."""
    if not isinstance(addr, str):
        return False
    addr = addr.strip()
    if not ADDRESS_RE.match(addr):
        return False
    return parseaddr(addr) == ("", addr)


def _has_email(u):
    return bool(u) and valid_address(u.get("email"))


def _detail(ev):
    out = {}
    for part in (ev.get("detail") or "").split():
        if "=" in part:
            k, v = part.split("=", 1)
            out[k] = v
    return out


def _tz(cfg):
    return ZoneInfo(cfg.get("timezone") or "Europe/Zurich")


def _hhmm(ts, cfg):
    return datetime.fromtimestamp(ts, _tz(cfg)).strftime("%H:%M")


def _epoch(now_utc):
    if isinstance(now_utc, (int, float)):
        return int(now_utc)
    return int(now_utc.timestamp())


# ---------------------------------------------------------------- step

def step(state, feed, now_utc, cfg, log=_nolog):
    """Fold one feed page into state, then emit; return (state, emails).

    state  -- dict, JSON-serialisable, persisted by the caller after every step
    feed   -- the parsed /api/activity answer (events, next, reset, users)
    emails -- list of dicts {to, subject, body, kind}; nothing is sent here.
              They are also appended to state["outbox"].

    poll_once() calls fold() once per page and emit() once per poll, so a
    multi-page poll still gives one immediate email (Q3) and one timer check.
    """
    fold(state, feed, log=log)
    return state, emit(state, now_utc, cfg, log=log)


def fold(state, feed, log=_nolog):
    """Fold one feed page into state. No emails are built here."""
    if feed.get("users"):
        state["users"] = feed["users"]
    users = _users_by_id(state.get("users"))
    if feed.get("reset"):
        old = state.get("cursor")
        state["cursor"] = feed.get("next")
        log("feed reset: cursor %s -> %s" % (old, state["cursor"]))
        return state
    for ev in feed.get("events") or []:
        try:
            ev = normalise_event(ev)
            actions = _classify(ev, users)
        except Exception as e:
            ev_id = ev.get("id") if isinstance(ev, dict) else None
            log("skipped malformed event id=%r: %r" % (ev_id, e))
            continue
        _apply(state, ev, actions)
    if feed.get("next") is not None:
        state["cursor"] = feed["next"]
    return state


def emit(state, now_utc, cfg, log=_nolog):
    """Build the immediate email, a due summary and due digests; enqueue them."""
    now = _epoch(now_utc)
    users = _users_by_id(state.get("users"))
    emails = []
    pending = state.get("pending_immediate") or []
    state["pending_immediate"] = []
    if pending:
        emails.append(_immediate_email(pending, users, cfg))
    summary = _summary_if_due(state, now, users, cfg)
    if summary:
        emails.append(summary)
    emails.extend(_friend_digests_if_due(state, now, users, cfg, log))
    enqueue(state, emails, log)
    return emails


def normalise_event(ev):
    """A clean, typed copy of one feed event; raises on anything malformed."""
    if not isinstance(ev, dict):
        raise ValueError("event is not an object")

    def text(v):
        if v is None:
            return ""
        if not isinstance(v, (str, int, float)):
            raise ValueError("expected text, got %r" % (v,))
        return str(v)

    out = {"id": int(ev["id"]), "at": int(ev["at"]), "kind": text(ev.get("kind")),
           "actor": int(ev.get("actor") or 0), "owner": int(ev.get("owner") or 0),
           "file": None, "group": None,
           "via": text(ev.get("via")), "detail": text(ev.get("detail"))}
    f = ev.get("file")
    if f is not None:
        if not isinstance(f, dict):
            raise ValueError("file is not an object")
        out["file"] = {"id": text(f.get("id")), "name": text(f.get("name")),
                       "size": int(f.get("size") or 0), "kind": text(f.get("kind")) or "file"}
    g = ev.get("group")
    if g is not None:
        if not isinstance(g, dict):
            raise ValueError("group is not an object")
        out["group"] = {"id": text(g.get("id")), "name": text(g.get("name"))}
    return out


def _classify(ev, users):
    """Which lists this event goes to. Pure: state is not touched until _apply."""
    kind, actor, owner = ev["kind"], ev["actor"], ev["owner"]
    actor_admin = _is_admin(actor, users)
    actions = []
    if kind in IMMEDIATE_ALWAYS or (kind == "group_created" and not actor_admin):
        actions.append("immediate")
    elif kind in HELD_KINDS and not actor_admin:
        actions.append("held")
    if kind == "download" and owner and owner != actor:
        u = users.get(owner)
        if u and u.get("emailDownloads") and _has_email(u) and not u.get("admin"):
            actions.append("friend")
    return actions


def _apply(state, ev, actions):
    if "immediate" in actions:
        state.setdefault("pending_immediate", []).append(ev)
    if "held" in actions:
        state["held"].append(ev)
        if state.get("held_first_at") is None:
            state["held_first_at"] = ev["at"]      # the event's time, not the poll's
        state["held_last_at"] = ev["at"]
    if "friend" in actions:
        state["friends"].setdefault(str(ev["owner"]), []).append(ev)


def enqueue(state, emails, log=_nolog):
    for e in emails:
        state["outbox"].append(e)
    over = len(state["outbox"]) - OUTBOX_CAP
    if over > 0:
        del state["outbox"][:over]
        state["outbox_dropped"] = state.get("outbox_dropped", 0) + over
        log("outbox full: dropped %d oldest email(s), %d in total"
            % (over, state["outbox_dropped"]))


# ---- immediate

def _immediate_email(events, users, cfg):
    viewas, warnings, persons, groups = [], [], [], []
    lines = []
    for ev in events:
        kind = ev["kind"]
        at = _hhmm(ev["at"], cfg)
        actor = _name(ev.get("actor"), users)
        if kind in ("viewas_start", "viewas_end"):
            d = _detail(ev)
            target = _name(int(d.get("target", 0) or 0), users) if d.get("target", "").isdigit() else "someone"
            part = "View as%s: %s viewing as %s" % (ELLIPSIS, actor, target)
            if kind == "viewas_end":
                reason = _one_line(d.get("reason") or "unknown")
                part += " stopped: " + reason
                lines.append("%s  %s stopped viewing as %s (%s)." % (at, actor, target, reason))
            else:
                lines.append("%s  %s started viewing as %s." % (at, actor, target))
            viewas.append(part)
        elif kind == "warning_disk":
            warnings.append(ev)
            detail = (ev.get("detail") or "").strip()
            free = size_si(int(detail)) if detail.isdigit() else _one_line(detail or "unknown")
            lines.append("%s  Low disk space: %s free." % (at, free))
        elif kind == "warning_preview":
            warnings.append(ev)
            fname = _one_line((ev.get("file") or {}).get("name") or "a file")
            lines.append("%s  Preview failed for \"%s\"." % (at, fname))
        elif kind == "first_signin":
            u = users.get(int(ev.get("actor") or 0)) or {}
            part = "New person: %s (%s)" % (actor, _one_line(u.get("email") or "no email"))
            persons.append(part)
            lines.append("%s  %s" % (at, part))
        elif kind == "group_created":
            gname = _one_line((ev.get("group") or {}).get("name") or "a group")
            groups.append("New group: %s by %s" % (gname, actor))
            lines.append("%s  %s created the group \"%s\"." % (at, actor, gname))
    parts = list(viewas)
    if warnings:
        parts.append("Warning: %d issue(s)" % len(warnings))
    parts += persons + groups
    body = "\n".join(lines) + "\n\n" + cfg["site_url"] + "\n"
    return {"to": cfg["mail_to"], "subject": "[FGS] " + "; ".join(parts),
            "body": body, "kind": "immediate"}


# ---- summary for the admin

def _summary_if_due(state, now, users, cfg):
    if not state["held"]:
        return None
    quiet = now - state["held_last_at"] >= int(cfg["quiet_minutes"]) * 60
    capped = now - state["held_first_at"] >= int(cfg["max_hold_minutes"]) * 60
    if not (quiet or capped):
        return None
    subject, body = build_summary(state["held"], users)
    state["held"] = []
    state["held_first_at"] = None
    state["held_last_at"] = None
    return {"to": cfg["mail_to"], "subject": subject, "body": body, "kind": "summary"}


def _new_person():
    return {"joined": False, "up": {}, "up_bytes": 0, "down": []}


def build_summary(held, users):
    groups = {}       # gid -> {"name", "people": {actor: tally}}
    group_order = []
    owners = {}       # "your" or owner id -> {"up": {actor: tally}, "files": {fid: ...}}
    owner_order = []
    blocked = {}
    blocked_order = []

    for ev in held:
        kind = ev["kind"]
        actor = int(ev.get("actor") or 0)
        if kind == "signin_blocked":
            who = _one_line(ev.get("detail") or "unknown")
            if who not in blocked:
                blocked[who] = 0
                blocked_order.append(who)
            blocked[who] += 1
            continue
        g = ev.get("group")
        f = ev.get("file") or {}
        if g:
            gid = str(g.get("id"))
            if gid not in groups:
                groups[gid] = {"name": _one_line(g.get("name") or gid), "people": {}, "order": []}
                group_order.append(gid)
            sec = groups[gid]
            if actor not in sec["people"]:
                sec["people"][actor] = _new_person()
                sec["order"].append(actor)
            p = sec["people"][actor]
            if kind == "group_joined":
                p["joined"] = True
            elif kind == "upload":
                k = f.get("kind") or "file"
                p["up"][k] = p["up"].get(k, 0) + 1
                p["up_bytes"] += int(f.get("size") or 0)
            elif kind == "download":
                fid = f.get("id")
                if fid not in p["down"]:
                    p["down"].append(fid)
            continue
        if kind not in ("upload", "download"):
            continue
        owner = int(ev.get("owner") or 0)
        key = "your" if _is_admin(owner, users) else str(owner)
        if key not in owners:
            owners[key] = {"owner": owner, "up": {}, "up_order": [], "files": {}, "file_order": []}
            owner_order.append(key)
        sec = owners[key]
        if kind == "upload":
            if actor not in sec["up"]:
                sec["up"][actor] = _new_person()
                sec["up_order"].append(actor)
            p = sec["up"][actor]
            k = f.get("kind") or "file"
            p["up"][k] = p["up"].get(k, 0) + 1
            p["up_bytes"] += int(f.get("size") or 0)
        else:
            fid = f.get("id")
            if fid not in sec["files"]:
                sec["files"][fid] = {"name": _one_line(f.get("name") or "a file"), "n": 0,
                                     "people": [], "vias": []}
                sec["file_order"].append(fid)
            fr = sec["files"][fid]
            fr["n"] += 1
            who = _name(actor, users)
            if who not in fr["people"]:
                fr["people"].append(who)
            via = VIA_LABELS.get(ev.get("via") or "")
            if via and via not in fr["vias"]:
                fr["vias"].append(via)

    sections = []
    for gid in group_order:
        sec = groups[gid]
        clauses = []
        for actor in sec["order"]:
            p = sec["people"][actor]
            parts = []
            if p["joined"]:
                parts.append("joined")
            if p["up"]:
                parts.append("uploaded " + _uploads_phrase(p))
            if p["down"]:
                parts.append("downloaded %d" % len(p["down"]))
            clauses.append(_name(actor, users) + " " + ", ".join(parts))
        sections.append((sec["name"], [" · ".join(clauses)]))

    ordered = [k for k in owner_order if k == "your"] + [k for k in owner_order if k != "your"]
    for key in ordered:
        sec = owners[key]
        title = "Your files" if key == "your" else "%s's files" % _name(sec["owner"], users)
        lines = []
        for actor in sec["up_order"]:
            lines.append("%s uploaded %s" % (_name(actor, users), _uploads_phrase(sec["up"][actor])))
        for fid in sec["file_order"]:
            fr = sec["files"][fid]
            lines.append("\"%s\" downloaded %s (%s)" % (fr["name"], _times(fr["n"]),
                                                       ", ".join(fr["people"] + fr["vias"])))
        sections.append((title, lines))

    if blocked_order:
        sections.append(("Blocked sign-ins", [
            "%s (%d attempt%s)" % (w, blocked[w], "" if blocked[w] == 1 else "s")
            for w in blocked_order]))

    names = [t for t, _ in sections]
    shown = ", ".join(names[:3])
    if len(names) > 3:
        shown += " and %d more" % (len(names) - 3)
    body = "\n\n".join(t + "\n" + "\n".join(lines) for t, lines in sections) + "\n"
    return "[FGS] Activity: " + shown, body


def _uploads_phrase(p):
    counts = [_plural(p["up"][k], k) for k in ("image", "video", "file") if p["up"].get(k)]
    for k in sorted(p["up"]):
        if k not in NOUNS:
            counts.append(_plural(p["up"][k], "file"))
    return "%s (%s)" % (", ".join(counts), size_si(p["up_bytes"]))


# ---- friends' digests

def _friend_digests_if_due(state, now, users, cfg, log=_nolog):
    tz = _tz(cfg)
    local = datetime.fromtimestamp(now, tz)
    today = local.strftime("%Y-%m-%d")
    hh, mm = [int(x) for x in str(cfg["friends_digest_time"]).split(":")]
    if (local.hour, local.minute) < (hh, mm) or state.get("friend_last_digest_date") == today:
        return []
    emails = []
    keep = {}
    cap = int(cfg["max_friend_emails_per_day"])
    for owner, items in state["friends"].items():
        if not items:
            continue
        u = users.get(int(owner))
        if not (u and u.get("emailDownloads") and not u.get("admin")):
            continue                     # switched off (or gone): the items are dropped
        if not _has_email(u):
            log("no digest for user #%s: %r is not a valid address" % (owner, u.get("email")))
            continue
        if len(emails) >= cap:
            keep[owner] = items          # over today's cap: wait for tomorrow
            continue
        emails.append({"to": u["email"], "subject": "Your files were downloaded",
                       "body": build_friend_body(items, users, cfg), "kind": "friend"})
    state["friends"] = keep
    state["friend_last_digest_date"] = today
    return emails


def build_friend_body(items, users, cfg):
    groups, group_order = {}, []
    loose = {"people": [], "files": [], "kinds": set(), "n": 0}
    for ev in items:
        f = ev.get("file") or {}
        who = _name(ev.get("actor"), users)
        g = ev.get("group")
        if g:
            gid = str(g.get("id"))
            if gid not in groups:
                groups[gid] = {"name": _one_line(g.get("name") or gid), "people": [],
                               "files": [], "kinds": set()}
                group_order.append(gid)
            sec = groups[gid]
        else:
            sec = loose
            sec["n"] += 1
        if who not in sec["people"]:
            sec["people"].append(who)
        if f.get("id") not in sec["files"]:
            sec["files"].append(f.get("id"))
            sec["kinds"].add(f.get("kind") or "file")
            sec.setdefault("names", []).append(_one_line(f.get("name") or "a file"))

    lines = []
    for gid in group_order:
        sec = groups[gid]
        lines.append("%s downloaded %d of your %s from %s." % (
            _capital(people_phrase(sec["people"])), len(sec["files"]),
            _noun_for(sec["kinds"]), sec["name"]))
    if loose["files"]:
        who = _capital(people_phrase(loose["people"]))
        if len(loose["files"]) == 1:
            lines.append("%s downloaded \"%s\" %s." % (who, loose["names"][0], _times(loose["n"])))
        else:
            lines.append("%s downloaded %d of your %s." % (who, len(loose["files"]),
                                                          _noun_for(loose["kinds"])))
    return "\n".join(lines) + "\n\n" + FOOTER % cfg["site_url"].rstrip("/") + "\n"


def _noun_for(kinds):
    if kinds == {"image"}:
        return "photos"
    if kinds == {"video"}:
        return "videos"
    return "files"


# ---------------------------------------------------------------- sending

def _build_message(cfg, mail, rcpts):
    msg = EmailMessage()
    msg["From"] = cfg["smtp_user"]
    msg["To"] = ", ".join(rcpts)
    if rcpts != [mail["to"]]:
        msg["X-Original-To"] = _one_line(mail["to"])
    msg["Subject"] = _one_line(mail["subject"])
    msg["Date"] = formatdate(localtime=True)
    msg["Message-ID"] = make_msgid(domain="gokapi-notify.local")
    msg.set_content(mail["body"])
    return msg


def _smtp_transport(cfg):
    def send(msg, from_addr, rcpts):
        with smtplib.SMTP_SSL("smtp.gmail.com", 465, timeout=30) as s:
            s.login(cfg["smtp_user"], cfg["smtp_app_password"])
            s.send_message(msg, from_addr=from_addr, to_addrs=rcpts)
    return send


def _dir_transport(path):
    def send(msg, from_addr, rcpts):
        os.makedirs(path, exist_ok=True)
        name = "%s-%s.eml" % (datetime.now().strftime("%Y%m%dT%H%M%S"),
                              str(msg["Message-ID"]).strip("<>").split("@")[0])
        with open(os.path.join(path, name), "wb") as fh:
            fh.write(msg.as_bytes())
    return send


def deliver(cfg, mail, transport=None):
    """The ONLY place mail leaves. Applies redirect_all_to and the live-feed check."""
    redirect = (cfg.get("redirect_all_to") or "").strip()
    if cfg.get("api_url") != LIVE_API_URL and not redirect:
        raise UnsafeConfig("api_url is %r, not the live feed, and redirect_all_to is unset; "
                           "refusing to send" % cfg.get("api_url"))
    rcpts = [redirect] if redirect else [_one_line(mail["to"])]
    msg = _build_message(cfg, mail, rcpts)
    if transport is None:
        mode = cfg.get("deliver") or "smtp"
        if mode.startswith("dir:"):
            transport = _dir_transport(mode[4:])
        elif mode == "smtp":
            transport = _smtp_transport(cfg)
        else:
            raise UnsafeConfig("unknown deliver mode %r" % mode)
    transport(msg, cfg["smtp_user"], rcpts)


def _is_poison(e, mail, cfg):
    """True when retrying this one email can never succeed, so it must not block the rest."""
    if isinstance(e, smtplib.SMTPRecipientsRefused):
        return True
    if isinstance(e, (smtplib.SMTPAuthenticationError, smtplib.SMTPSenderRefused)):
        return False                     # our login or sender: every email would fail
    if isinstance(e, smtplib.SMTPResponseException):
        return 500 <= e.smtp_code < 600 and mail.get("to") != cfg.get("mail_to")
    if isinstance(e, (smtplib.SMTPException, OSError)):
        return False                     # transport trouble: keep and back off
    return True                          # building or sending this email is broken


def _next_index(outbox):
    """Immediate email first (alarms must not queue behind digests), else oldest first."""
    return min(range(len(outbox)),
               key=lambda k: (SEND_ORDER.get(outbox[k].get("kind") if isinstance(outbox[k], dict) else None, 1), k))


def flush_outbox(state, now_utc, cfg, transport=None, log=log):
    """Send from the outbox; remove each email only after it was sent.

    Immediate email goes first. An email that can never be delivered (refused
    recipient, a 5xx for anyone but mail_to, or a non-SMTP error building or
    sending it) is dropped, logged and counted in outbox_poisoned. A login or
    transport failure keeps the outbox, logs once per failure streak and backs
    off next_send_at by 1, 5, 15, then 60 minutes. Never emails about itself.
    """
    now = _epoch(now_utc)
    if state.get("next_send_at") and now < state["next_send_at"]:
        return state
    outbox = state["outbox"]
    while outbox:
        i = _next_index(outbox)
        mail = outbox[i]
        try:
            deliver(cfg, mail, transport=transport)
        except UnsafeConfig:
            raise
        except Exception as e:
            if _is_poison(e, mail if isinstance(mail, dict) else {}, cfg):
                outbox.pop(i)
                state["outbox_poisoned"] = state.get("outbox_poisoned", 0) + 1
                to = mail.get("to") if isinstance(mail, dict) else None
                subj = mail.get("subject") if isinstance(mail, dict) else None
                log("dropped undeliverable email to %s: %s (%r)" % (_one_line(to), _one_line(subj), e))
                continue
            n = state.get("send_failures", 0)
            if state["failing"].get("smtp") is None:
                state["failing"]["smtp"] = now
                log("smtp failing since %s: %r (outbox %d)" % (_stamp(now), e, len(outbox)))
            state["send_failures"] = n + 1
            state["next_send_at"] = now + 60 * BACKOFF_MINUTES[min(n, len(BACKOFF_MINUTES) - 1)]
            return state
        outbox.pop(i)
        log("sent: %s" % _one_line(mail["subject"]))
        if state["failing"].get("smtp") is not None:
            log("smtp recovered after %d failed attempt(s)" % state.get("send_failures", 0))
            state["failing"]["smtp"] = None
        state["send_failures"] = 0
        state["next_send_at"] = None
    return state


def _stamp(ts):
    return datetime.fromtimestamp(ts).strftime("%Y-%m-%d %H:%M:%S")


# ---------------------------------------------------------------- polling

def make_fetch(cfg, timeout=30):
    def fetch(since):
        headers = {"apikey": cfg["api_key"], "limit": "500"}
        if since is not None:
            headers["since"] = str(int(since))
        req = urllib.request.Request(cfg["api_url"], headers=headers)
        try:
            with urllib.request.urlopen(req, timeout=timeout) as r:
                if r.status != 200:
                    raise FeedError("HTTP %d" % r.status)
                data = json.loads(r.read().decode("utf-8"))
        except (urllib.error.URLError, http.client.HTTPException, socket.timeout, OSError, ValueError) as e:
            raise FeedError(e)
        if not isinstance(data, dict) or data.get("Result") != "OK":
            raise FeedError("unexpected answer: %.200r" % (data,))
        return data
    return fetch


def poll_once(state, cfg, now_utc, fetch, transport=None, log=log, from_start=False, persist=None):
    """One poll: fold every feed page, emit once, persist, then send the outbox.

    Emitting once per poll (not per page) keeps Q3 -- one immediate email per
    poll -- and runs the summary and 19:00 timers once, after the whole backlog.
    """
    now = _epoch(now_utc)
    for _ in range(MAX_PAGES_PER_POLL):
        since = state.get("cursor")
        if since is None and from_start:
            since = 0
        try:
            feed = fetch(since)
        except FeedError as e:
            if state["failing"].get("feed") is None:
                state["failing"]["feed"] = now
                log("feed unreachable since %s: %s" % (_stamp(now), e))
            break
        if state["failing"].get("feed") is not None:
            log("feed recovered (down since %s)" % _stamp(state["failing"]["feed"]))
            state["failing"]["feed"] = None
        fold(state, feed, log=log)
        if not feed.get("more"):
            break
    # Timers (quiet hour, cap, 19:00) run even while the feed is down.
    emit(state, now_utc, cfg, log=log)
    if persist:
        persist(state)
    flush_outbox(state, now_utc, cfg, transport=transport, log=log)
    if persist:
        persist(state)
    return state


def main(argv=None):
    argv = list(sys.argv[1:] if argv is None else argv)
    path = os.path.join(HERE, "notify.config.json")
    if "--config" in argv:
        i = argv.index("--config")
        if i + 1 >= len(argv):
            sys.exit("--config needs a path")
        path = argv[i + 1]
    cfg = load_config(path)

    if "--test" in argv:
        deliver(cfg, {"to": cfg["mail_to"], "subject": "[FGS] test email",
                      "body": "If you are reading this, Gokapi notifications are working.\n\n"
                              "Sent by gokapi-notify.py on the Mac mini.\n",
                      "kind": "test"})
        log("sent: [FGS] test email")
        return

    from_start = "--from-start" in argv
    state_path = cfg["state_path"]
    state = load_state(state_path)
    fetch = make_fetch(cfg)
    persist = lambda s: save_state(state_path, s)

    def now():
        return datetime.now(timezone.utc)

    if "--once" in argv:
        state = poll_once(state, cfg, now(), fetch, log=log, from_start=from_start, persist=persist)
        if state["outbox"]:
            log("outbox %d unsent" % len(state["outbox"]))
        if state["failing"].get("feed") is not None or state.get("cursor") is None:
            # A cutover run (--once --from-start) that read nothing must not look
            # like success: loading the agent afterwards would "start from now".
            sys.exit("feed not read; cursor is %r - not done" % state.get("cursor"))
        return

    log("gokapi-notify started (feed=%s, poll=%ss, quiet=%smin, cap=%smin, digest=%s %s%s)" % (
        cfg["api_url"], cfg["poll_seconds"], cfg["quiet_minutes"], cfg["max_hold_minutes"],
        cfg["friends_digest_time"], cfg["timezone"],
        ", redirect=%s" % cfg["redirect_all_to"] if cfg["redirect_all_to"] else ""))
    while True:
        try:
            state = poll_once(state, cfg, now(), fetch, log=log, from_start=from_start, persist=persist)
        except UnsafeConfig as e:
            sys.exit("refusing to run: %s" % e)
        except Exception as e:
            log("poll error: %r" % e)
        time.sleep(int(cfg["poll_seconds"]))


if __name__ == "__main__":
    main()
