"""Replay and unit tests for gokapi_notify (Python 3.9 stdlib only).

Run:  cd deploy/notifier && /usr/bin/python3 -m unittest -v

The busy-evening fixture is synthetic (ruling R-c): invented people, files and
date (Saturday 2026-03-14, CET, +01:00). Only the *shape* of the 22:02-22:44
download bursts is modelled on a real evening. `build_fixture()` below is the
deterministic generator; `fixtures/busy_evening.json` is its committed output
and `test_fixture_file_matches_generator` keeps the two identical.

Fixture design (local times, 2026-03-14 unless marked):
  18:40-21:55  Anna uploads 10 photos to "Iceland trip" every 5 minutes, plus a
               video on every 4th burst (bursts 0, 4, ... 36).  40 bursts.
  18:45-18:55  Ben downloads 12 of Anna's photos      -> Anna's day-1 digest
  19:00        friends' digest time (no download on that page)
  19:05        first_signin Elif                      -> immediate
  19:20        Carla joins "Iceland trip"
  19:30        ADMIN: Franco uploads 5 scans + creates group "Family" (never held)
  20:00        ADMIN: Franco downloads 3 of Anna's photos (counts for Anna only)
  20:15        warning_disk + 3 warning_preview       -> ONE immediate email
  20:30        Carla downloads 10 of Anna's photos
  20:45        someone downloads Anna's "Recipe.pdf" twice (file, collection)
  21:00/21:07  viewas_start / viewas_end by the admin -> two immediate emails
  21:40        3-hour cap fires (first held event 18:40)   -> summary 1
  21:50        Ben uploads "Route map.pdf" outside any group
  22:00        Carla creates "Ski weekend"            -> immediate
  22:02-22:44  Ben (90 events, 87 distinct) and Carla (40) download Iceland
               photos in bursts at the notify.log minutes
  22:06        Carla uploads 6 photos + 1 video to "Ski weekend"
  22:10        Ben joins "Ski weekend"; 22:41 downloads 2 of Carla's ski photos
  22:12/25/40  someone downloads Franco's "CV.pdf" via link
  22:20/21     signin_blocked x@spam.example
  22:30        Carla downloads Ben's "Route map.pdf" via zip
  23:10        first_signin Femi                      -> immediate
  23:30        ADMIN: Franco uploads 3 photos to "Family" (no quiet-timer reset)
  23:44        quiet hour fires (last held 22:44)     -> summary 2
  2026-03-15 19:00  Anna's day-2 digest; Ben and Carla have the setting off.

Invariants the expectations rely on: no held event before 18:40, and every
held event after 21:40 is at or before 22:44 (admin activity is never held).
"""
import email
import json
import os
import shutil
import smtplib
import tempfile
import unittest
import urllib.error
from datetime import datetime, timedelta, timezone

import gokapi_notify as gn

HERE = os.path.dirname(os.path.abspath(__file__))
FIXTURE_PATH = os.path.join(HERE, "fixtures", "busy_evening.json")

FRANCO = "franco@example.com"
CET = timezone(timedelta(hours=1))
SITE = "https://share.franco.international"

PHOTO = 3000000
VIDEO = 50000000


def L(hh, mm, day=14):
    """Local (CET) wall time on the fixture date -> aware datetime."""
    return datetime(2026, 3, day, hh, mm, tzinfo=CET)


def iso_utc(dt):
    return dt.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


USERS = [
    {"id": 1, "email": "franco@example.com", "name": "Franco", "admin": True, "emailDownloads": False},
    {"id": 2, "email": "anna@example.com", "name": "Anna", "admin": False, "emailDownloads": True},
    {"id": 3, "email": "ben@example.com", "name": "Ben", "admin": False, "emailDownloads": False},
    {"id": 4, "email": "carla@example.com", "name": "Carla", "admin": False, "emailDownloads": False},
    {"id": 5, "email": "elif@example.com", "name": "Elif", "admin": False, "emailDownloads": False},
    {"id": 6, "email": "femi@example.com", "name": "Femi", "admin": False, "emailDownloads": True},
]

ICELAND = {"id": "g1", "name": "Iceland trip"}
SKI = {"id": "g2", "name": "Ski weekend"}
FAMILY = {"id": "g3", "name": "Family"}

BURST_MINUTES = [(22, 2), (22, 3), (22, 4), (22, 5), (22, 9), (22, 18),
                 (22, 35), (22, 37), (22, 41), (22, 44)]


def build_fixture():
    """Deterministic generator for fixtures/busy_evening.json (no randomness needed)."""
    by_minute = {}

    def add(dt, kind, actor, owner=0, file=None, group=None, via="", detail=""):
        by_minute.setdefault(dt, []).append({
            "kind": kind, "actor": actor, "owner": owner, "file": file,
            "group": group, "via": via, "detail": detail})

    def f(fid, name, size, kind):
        return {"id": fid, "name": name, "size": size, "kind": kind}

    # Anna's Iceland uploads: burst b at 18:40 + 5b minutes, b = 0..39.
    photos, videos = [], []
    for b in range(40):
        dt = L(18, 40) + timedelta(minutes=5 * b)
        for k in range(10):
            p = f("p%03d" % len(photos), "IMG_%04d.HEIC" % (1000 + len(photos)), PHOTO, "image")
            photos.append(p)
            add(dt, "upload", 2, 2, p, ICELAND)
        if b % 4 == 0:
            v = f("v%02d" % len(videos), "VID_%04d.MOV" % (500 + len(videos)), VIDEO, "video")
            videos.append(v)
            add(dt, "upload", 2, 2, v, ICELAND)

    # 18:45-18:55 Ben downloads 12 of Anna's photos (before the 19:00 digest).
    for (hh, mm), idx in (((18, 45), range(0, 4)), ((18, 50), range(4, 8)), ((18, 55), range(10, 14))):
        for i in idx:
            add(L(hh, mm), "download", 3, 2, photos[i], ICELAND, "group")

    add(L(19, 5), "first_signin", 5)
    add(L(19, 20), "group_joined", 4, 0, None, ICELAND)

    # 19:30 admin activity: never in a summary.
    for i in range(5):
        add(L(19, 30), "upload", 1, 1, f("s%02d" % i, "Scan_%02d.pdf" % i, 400000, "file"))
    add(L(19, 30), "group_created", 1, 0, None, FAMILY)

    # 20:00 the admin downloads 3 of Anna's photos: not held, but counts for Anna.
    for i in range(20, 23):
        add(L(20, 0), "download", 1, 2, photos[i], ICELAND, "group")

    # 20:15 one disk warning and three preview warnings in one poll.
    add(L(20, 15), "warning_disk", 0, 0, None, None, "", "18400000000")
    for p in (photos[300], photos[301], videos[9]):
        add(L(20, 15), "warning_preview", 0, 2, p, ICELAND)

    for i in range(50, 60):
        add(L(20, 30), "download", 4, 2, photos[i], ICELAND, "group")

    recipe = f("r1", "Recipe.pdf", 120000, "file")
    add(L(20, 45), "download", 0, 2, recipe, None, "file")
    add(L(20, 45), "download", 0, 2, recipe, None, "collection")

    add(L(21, 0), "viewas_start", 1, 0, None, None, "", "target=3")
    add(L(21, 7), "viewas_end", 1, 0, None, None, "", "target=3 reason=exit")

    route = f("m1", "Route map.pdf", 2345678, "file")
    add(L(21, 50), "upload", 3, 3, route)

    add(L(22, 0), "group_created", 4, 0, None, SKI)

    ski = [f("k%d" % i, "DSC_%04d.JPG" % (200 + i), PHOTO, "image") for i in range(6)]
    ski_video = f("kv", "DSC_0300.MP4", VIDEO, "video")
    for p in ski + [ski_video]:
        add(L(22, 6), "upload", 4, 4, p, SKI)
    add(L(22, 10), "group_joined", 3, 0, None, SKI)

    # 22:02-22:44 download bursts: Ben 9 per burst, Carla 4 per burst.
    ben_seq = photos[100:187] + photos[100:103]          # 90 events, 87 distinct
    carla_seq = photos[200:240]                          # 40 events, 40 distinct
    for k, (hh, mm) in enumerate(BURST_MINUTES):
        for p in ben_seq[9 * k:9 * k + 9]:
            add(L(hh, mm), "download", 3, 2, p, ICELAND, "group")
        for p in carla_seq[4 * k:4 * k + 4]:
            add(L(hh, mm), "download", 4, 2, p, ICELAND, "group")
        if (hh, mm) == (22, 41):
            for p in ski[:2]:
                add(L(hh, mm), "download", 3, 4, p, SKI, "group")

    cv = f("c1", "CV.pdf", 98000, "file")
    for mm in (12, 25, 40):
        add(L(22, mm), "download", 0, 1, cv, None, "file")
    add(L(22, 20), "signin_blocked", 0, 0, None, None, "", "x@spam.example")
    add(L(22, 21), "signin_blocked", 0, 0, None, None, "", "x@spam.example")
    add(L(22, 30), "download", 4, 3, route, None, "zip")

    add(L(23, 10), "first_signin", 6)
    for i in range(3):
        add(L(23, 30), "upload", 1, 1, f("fa%d" % i, "Family_%d.jpg" % i, PHOTO, "image"), FAMILY)

    # Pages: every minute 18:30 -> 01:30, then the empty middle, then 19:01 next day.
    pages = []
    next_id = 1
    minute = L(18, 30)
    end = L(1, 30, day=15)
    minutes = []
    while minute <= end:
        minutes.append(minute)
        minute += timedelta(minutes=1)
    minutes.append(L(19, 1, day=15))
    assert set(by_minute) <= set(minutes)
    for m in minutes:
        events = []
        for e in by_minute.get(m, []):
            ev = {"id": next_id, "at": int(m.timestamp())}
            ev.update(e)
            events.append(ev)
            next_id += 1
        pages.append({"at_poll": iso_utc(m), "events": events})
    return {"users": USERS, "pages": pages}


# ---- Hand-written expectations (from the design above, not from running code) ----

EXPECTED_IMMEDIATE = [
    ("2026-03-14T19:05:00+01:00", "[FGS] New person: Elif (elif@example.com)"),
    ("2026-03-14T20:15:00+01:00", "[FGS] Warning: 4 issue(s)"),
    ("2026-03-14T21:00:00+01:00", "[FGS] View as…: Franco viewing as Ben"),
    ("2026-03-14T21:07:00+01:00", "[FGS] View as…: Franco viewing as Ben stopped: exit"),
    ("2026-03-14T22:00:00+01:00", "[FGS] New group: Ski weekend by Carla"),
    ("2026-03-14T23:10:00+01:00", "[FGS] New person: Femi (femi@example.com)"),
]

EXPECTED_FRIEND_DIGESTS = [
    ("2026-03-14T19:00:00+01:00", "anna@example.com"),
    ("2026-03-15T19:00:00+01:00", "anna@example.com"),
]

EXPECTED_SUMMARY_SUBJECTS = [
    "[FGS] Activity: Iceland trip, Anna's files",
    "[FGS] Activity: Iceland trip, Ski weekend, Your files and 2 more",
]

# 370 photos x 3 MB + 10 videos x 50 MB = 1.61e9 B -> 1.6 GB.
EXPECTED_SUMMARY_1 = (
    "Iceland trip\n"
    "Anna uploaded 370 photos, 10 videos (1.6 GB) · Ben downloaded 12 · Carla joined, downloaded 10\n"
    "\n"
    "Anna's files\n"
    "\"Recipe.pdf\" downloaded 2 times (someone, via link)\n"
)

# 30 photos x 3 MB = 90.0 MB; ski: 6 x 3 MB + 50 MB = 68.0 MB; route map 2345678 B = 2.3 MB.
EXPECTED_SUMMARY_2 = (
    "Iceland trip\n"
    "Anna uploaded 30 photos (90.0 MB) · Ben downloaded 87 · Carla downloaded 40\n"
    "\n"
    "Ski weekend\n"
    "Carla uploaded 6 photos, 1 video (68.0 MB) · Ben joined, downloaded 2\n"
    "\n"
    "Your files\n"
    "\"CV.pdf\" downloaded 3 times (someone, via link)\n"
    "\n"
    "Ben's files\n"
    "Ben uploaded 1 file (2.3 MB)\n"
    "\"Route map.pdf\" downloaded once (Carla, via zip)\n"
    "\n"
    "Blocked sign-ins\n"
    "x@spam.example (2 attempts)\n"
)

FOOTER = ("You get this because \"Email me when my files are downloaded\" is on. "
          "Turn it off: " + SITE + "/settings\n")

EXPECTED_ANNA_DAY1 = (
    "Ben downloaded 12 of your photos from Iceland trip.\n"
    "\n" + FOOTER)

# Franco 3 + Carla 10 + Ben 87 + Carla 40 distinct photos = 140, first seen Franco, Carla, Ben.
EXPECTED_ANNA_DAY2 = (
    "Franco, Carla and Ben downloaded 140 of your photos from Iceland trip.\n"
    "Someone downloaded \"Recipe.pdf\" 2 times.\n"
    "\n" + FOOTER)


def base_cfg(**over):
    cfg = gn.with_defaults({
        "api_key": "test-key",
        "smtp_user": "notifier@example.com",
        "smtp_app_password": "x",
        "mail_to": FRANCO,
    })
    cfg.update(over)
    return cfg


def local_iso(ts):
    return datetime.fromtimestamp(ts, CET).isoformat()


class FakeTransport(object):
    """Stands in for SMTP *under* deliver(), so redirect and safety checks still run."""

    def __init__(self, clock, fail_window=None, refuse=()):
        self.clock = clock
        self.fail_window = fail_window
        self.refuse = set(refuse)
        self.sent = []

    def __call__(self, msg, from_addr, rcpts):
        now = self.clock()
        bad = [r for r in rcpts if r in self.refuse]
        if bad:
            raise smtplib.SMTPRecipientsRefused({r: (550, b"5.1.1 no such user") for r in bad})
        if self.fail_window and self.fail_window[0] <= now < self.fail_window[1]:
            raise smtplib.SMTPAuthenticationError(535, b"5.7.8 Username and Password not accepted")
        body = msg.get_content()
        for r in rcpts:
            self.sent.append((local_iso(now), r, str(msg["Subject"]), body))


def ts(iso):
    return int(datetime.fromisoformat(iso).timestamp())


class ReplayBusyEvening(unittest.TestCase):
    maxDiff = None

    @classmethod
    def setUpClass(cls):
        with open(FIXTURE_PATH) as fh:
            cls.fixture = json.load(fh)

    def replay(self, crash_at=None, smtp_fail=None, users_at=None, refuse=()):
        """Drive step() over every fixture minute with a fake clock and a fake sender.

        crash_at: ISO time after which state is round-tripped through JSON (restart).
        smtp_fail: (start, end) ISO window in which every send raises SMTPAuthenticationError.
        users_at: optional function(now_ts, users) -> users, to change settings mid-replay.
        """
        cfg = base_cfg()
        pages = {}
        for p in self.fixture["pages"]:
            pages[ts(p["at_poll"].replace("Z", "+00:00"))] = p["events"]
        start, end = min(pages), max(pages)
        clock = {"now": start}
        fail = None
        if smtp_fail:
            fail = (ts(smtp_fail[0]), ts(smtp_fail[1]))
        transport = FakeTransport(lambda: clock["now"], fail, refuse)
        crash = ts(crash_at) if crash_at else None
        state = gn.new_state()
        last_id = 0
        now = start
        while now <= end:
            clock["now"] = now
            events = pages.get(now, [])
            if events:
                last_id = events[-1]["id"]
            users = self.fixture["users"]
            if users_at:
                users = users_at(now, users)
            feed = {"Result": "OK", "events": events, "next": last_id,
                    "more": False, "reset": False, "users": users}
            now_dt = datetime.fromtimestamp(now, timezone.utc)
            state, _ = gn.step(state, feed, now_dt, cfg)
            persisted = json.dumps(state)          # the caller persists before sending
            if crash is not None and now >= crash:
                state = json.loads(persisted)
                crash = None
            gn.flush_outbox(state, now_dt, cfg, transport=transport, log=lambda m: None)
            json.dumps(state)
            now += 60
        self.assertEqual(state["outbox"], [], "outbox must drain by the end")
        self.last_state = state
        return transport.sent

    def test_refused_recipient_is_dropped_and_alarms_still_arrive(self):
        sent = self.replay(refuse={"anna@example.com"})
        immediate = [(at, subj) for at, to, subj, _ in sent
                     if not subj.startswith("[FGS] Activity") and to == FRANCO]
        self.assertEqual(immediate, EXPECTED_IMMEDIATE)        # 21:00 and 21:07 on time
        self.assertEqual([to for _, to, _, _ in sent if to != FRANCO], [])
        self.assertEqual(self.last_state["outbox_poisoned"], 2)  # Anna's two digests
        self.assertIsNone(self.last_state["failing"]["smtp"])

    def summary_bodies(self):
        return [b for _, _, s, b in self.replay() if s.startswith("[FGS] Activity")]

    def test_fixture_file_matches_generator(self):
        self.assertEqual(self.fixture, build_fixture())
        n = sum(len(p["events"]) for p in self.fixture["pages"])
        self.assertTrue(550 <= n <= 650, n)

    def test_exact_immediate_emails(self):
        sent = self.replay()
        immediate = [(at, subj) for at, to, subj, _ in sent
                     if not subj.startswith("[FGS] Activity") and to == FRANCO]
        self.assertEqual(immediate, EXPECTED_IMMEDIATE)

    def test_summaries_three_hour_cap_and_quiet_hour(self):
        sent = self.replay()
        summaries = [at for at, to, subj, _ in sent if subj.startswith("[FGS] Activity")]
        self.assertEqual(summaries, ["2026-03-14T21:40:00+01:00", "2026-03-14T23:44:00+01:00"])
        subjects = [subj for _, _, subj, _ in sent if subj.startswith("[FGS] Activity")]
        self.assertEqual(subjects, EXPECTED_SUMMARY_SUBJECTS)

    def test_summary_body_groups_by_group_and_person(self):
        body = self.summary_bodies()[1]
        self.assertIn("Iceland trip\nAnna uploaded ", body)
        self.assertIn("Ben downloaded 87", body)
        self.assertIn("Your files\n\"CV.pdf\" downloaded 3 times (someone, via link)", body)
        self.assertNotIn("**", body)

    def test_summary_bodies_exact(self):
        self.assertEqual(self.summary_bodies(), [EXPECTED_SUMMARY_1, EXPECTED_SUMMARY_2])

    def test_admin_activity_never_summarised(self):
        for body in self.summary_bodies():
            self.assertNotIn("Franco", body)
            self.assertNotIn("Family", body)
            self.assertNotIn("Scan_", body)

    def test_restart_mid_replay_no_resend_no_skip(self):
        self.assertEqual(self.replay(crash_at="2026-03-14T22:30:00+01:00"), self.replay())

    def test_smtp_failure_keeps_outbox_and_never_mails_itself(self):
        sent = self.replay(smtp_fail=("2026-03-14T22:00:00+01:00", "2026-03-14T22:40:00+01:00"))
        self.assertEqual(sorted(s for _, _, s, _ in sent), sorted(s for _, _, s, _ in self.replay()))
        self.assertFalse(any("notifier" in s.lower() or "error" in s.lower() for _, _, s, _ in sent))
        # Backoff 1, 5, 15, 60 min from the 22:00 failure: 22:01, 22:06, 22:21 fail, 23:21 sends.
        late = [at for at, _, s, _ in sent if s == "[FGS] New group: Ski weekend by Carla"]
        self.assertEqual(late, ["2026-03-14T23:21:00+01:00"])

    def test_friend_digest_once_a_day_across_midnight(self):
        friend = [(at, to) for at, to, subj, _ in self.replay() if subj == "Your files were downloaded"]
        self.assertEqual(friend, EXPECTED_FRIEND_DIGESTS)

    def test_friend_digest_bodies_exact(self):
        bodies = [b for _, _, subj, b in self.replay() if subj == "Your files were downloaded"]
        self.assertEqual(bodies, [EXPECTED_ANNA_DAY1, EXPECTED_ANNA_DAY2])

    def test_friend_digest_skipped_when_setting_turned_off(self):
        # Anna switches the setting off at 12:00 on day 2: her held items are dropped.
        off_from = ts("2026-03-15T12:00:00+01:00")

        def users_at(now, users):
            if now < off_from:
                return users
            return [dict(u, emailDownloads=False) if u["id"] == 2 else u for u in users]

        friend = [(at, to) for at, to, subj, _ in self.replay(users_at=users_at)
                  if subj == "Your files were downloaded"]
        self.assertEqual(friend, EXPECTED_FRIEND_DIGESTS[:1])

    def test_someone_for_anonymous(self):
        cfg = base_cfg()
        users = [dict(u) for u in USERS]
        now = L(10, 0)
        recipe = {"id": "r1", "name": "Recipe.pdf", "size": 1, "kind": "file"}
        events = [
            {"id": 1, "at": int(now.timestamp()), "kind": "download", "actor": 0, "owner": 2,
             "file": recipe, "group": None, "via": "file", "detail": ""},
            {"id": 2, "at": int(now.timestamp()), "kind": "download", "actor": 99, "owner": 2,
             "file": recipe, "group": None, "via": "api", "detail": ""},
        ]
        state, _ = gn.step(gn.new_state(), {"events": events, "next": 2, "users": users}, now, cfg)
        later = now + timedelta(minutes=61)
        state, emails = gn.step(state, {"events": [], "next": 2, "users": users}, later, cfg)
        summary = [e for e in emails if e["kind"] == "summary"][0]
        self.assertIn("\"Recipe.pdf\" downloaded 2 times (someone, someone (#99), via link, via API)",
                      summary["body"])
        evening = L(19, 0)
        state, emails = gn.step(state, {"events": [], "next": 2, "users": users}, evening, cfg)
        friend = [e for e in emails if e["kind"] == "friend"][0]
        self.assertTrue(friend["body"].startswith(
            "Someone and someone (#99) downloaded \"Recipe.pdf\" 2 times.\n"), friend["body"])


class FixRound1(unittest.TestCase):
    """Review fix round 1: poison mail, per-poll emit, crash window, poison events."""

    def users(self):
        return [dict(u) for u in USERS]

    def upload(self, i, at, group=ICELAND):
        return {"id": i, "at": at, "kind": "upload", "actor": 2, "owner": 2,
                "file": {"id": "f%d" % i, "name": "IMG_%d.HEIC" % i, "size": PHOTO, "kind": "image"},
                "group": group, "via": "", "detail": ""}

    def test_non_smtp_error_on_one_email_is_dropped_not_blocking(self):
        cfg = base_cfg()
        state = gn.new_state()
        state["outbox"] = [
            {"to": "anna@example.com", "subject": "Your files were downloaded", "body": "x", "kind": "friend"},
            {"to": FRANCO, "subject": "[FGS] View as\u2026: Franco viewing as Ben", "body": "x",
             "kind": "immediate"},
            {"to": FRANCO, "subject": "[FGS] Activity: x", "body": "x", "kind": "summary"},
        ]
        order, logs = [], []

        def transport(msg, f, rcpts):
            order.append(str(msg["Subject"]))
            if rcpts == ["anna@example.com"]:
                raise ValueError("cannot encode this message")

        gn.flush_outbox(state, L(10, 0), cfg, transport=transport, log=logs.append)
        self.assertEqual(order, ["[FGS] View as\u2026: Franco viewing as Ben", "[FGS] Activity: x",
                                 "Your files were downloaded"])     # immediate first
        self.assertEqual(state["outbox"], [])
        self.assertEqual(state["outbox_poisoned"], 1)
        self.assertTrue(any("dropped undeliverable email to anna@example.com" in m for m in logs), logs)
        self.assertIsNone(state["next_send_at"])

    def test_5xx_for_friend_is_poison_but_auth_and_5xx_for_admin_back_off(self):
        cfg = base_cfg()
        friend = {"to": "anna@example.com", "subject": "s", "body": "x", "kind": "friend"}
        admin = {"to": FRANCO, "subject": "t", "body": "x", "kind": "summary"}
        data_err = smtplib.SMTPDataError(554, b"5.7.1 rejected")
        auth_err = smtplib.SMTPAuthenticationError(535, b"bad login")
        self.assertTrue(gn._is_poison(data_err, friend, cfg))
        self.assertFalse(gn._is_poison(data_err, admin, cfg))
        self.assertFalse(gn._is_poison(auth_err, friend, cfg))
        self.assertFalse(gn._is_poison(smtplib.SMTPServerDisconnected("gone"), friend, cfg))
        self.assertFalse(gn._is_poison(OSError("network down"), friend, cfg))
        self.assertTrue(gn._is_poison(smtplib.SMTPRecipientsRefused({FRANCO: (550, b"no")}), admin, cfg))
        self.assertTrue(gn._is_poison(KeyError("to"), friend, cfg))
        with self.assertRaises(gn.UnsafeConfig):
            gn.flush_outbox({"outbox": [friend], "failing": {"feed": None, "smtp": None}},
                            L(10, 0), base_cfg(api_url="http://x/api/activity"),
                            transport=lambda *a: None, log=lambda m: None)

    def test_invalid_friend_address_never_enqueued(self):
        cfg = base_cfg()
        users = self.users()
        users[1] = dict(users[1], email="anna@localhost")          # no dot in the domain
        users[5] = dict(users[5], email="Femi <femi@example.com>")  # not a bare address
        at = int(L(10, 0).timestamp())
        f = {"id": "x", "name": "x.pdf", "size": 1, "kind": "file"}
        events = [{"id": 1, "at": at, "kind": "download", "actor": 3, "owner": 2, "file": f,
                   "group": None, "via": "file", "detail": ""},
                  {"id": 2, "at": at, "kind": "download", "actor": 3, "owner": 6, "file": f,
                   "group": None, "via": "file", "detail": ""}]
        state, _ = gn.step(gn.new_state(), {"events": events, "next": 2, "users": users}, L(10, 0), cfg)
        self.assertEqual(state["friends"], {})
        self.assertTrue(gn.valid_address("anna@example.com"))
        for bad in ("anna@localhost", "a b@example.com", "a@b@example.com", "<a@example.com>", "", None):
            self.assertFalse(gn.valid_address(bad), bad)

    def test_backlog_over_two_pages_gives_one_summary_and_one_immediate(self):
        cfg = base_cfg()
        t0 = int(L(6, 0).timestamp())                     # outage from 06:00, back at 10:00
        page1 = [self.upload(i, t0 + i) for i in range(1, 500)]
        page1.append({"id": 500, "at": t0 + 500, "kind": "warning_preview", "actor": 0, "owner": 2,
                      "file": page1[0]["file"], "group": ICELAND, "via": "", "detail": ""})
        page2 = [self.upload(i, t0 + i) for i in range(501, 600)]
        page2.append({"id": 600, "at": t0 + 600, "kind": "warning_disk", "actor": 0, "owner": 0,
                      "file": None, "group": None, "via": "", "detail": "1000"})
        page2.append({"id": 601, "at": t0 + 601, "kind": "first_signin", "actor": 5, "owner": 0,
                      "file": None, "group": None, "via": "", "detail": ""})
        answers = [
            {"Result": "OK", "events": page1, "next": 500, "more": True, "users": self.users()},
            {"Result": "OK", "events": page2, "next": 601, "more": False, "users": self.users()},
        ]
        calls, got = [], []

        def fetch(since):
            calls.append(since)
            return answers[len(calls) - 1]

        state = gn.new_state()
        state["cursor"] = 0
        state = gn.poll_once(state, cfg, L(10, 0), fetch,
                             transport=lambda m, f, r: got.append(str(m["Subject"])),
                             log=lambda m: None, persist=lambda s: None)
        self.assertEqual(calls, [0, 500])
        self.assertEqual(got, ["[FGS] Warning: 2 issue(s); New person: Elif (elif@example.com)",
                               "[FGS] Activity: Iceland trip"])
        self.assertEqual(state["cursor"], 601)

    def test_state_is_persisted_before_each_send(self):
        cfg = base_cfg()
        at = int(L(10, 0).timestamp())
        events = [{"id": 9, "at": at, "kind": "first_signin", "actor": 5, "owner": 0, "file": None,
                   "group": None, "via": "", "detail": ""}]
        snapshots = []

        def persist(s):
            snapshots.append(json.loads(json.dumps(s)))

        seen = []      # recorded, not asserted here: flush_outbox would treat an exception as poison

        def transport(msg, f, rcpts):
            last = snapshots[-1] if snapshots else {"outbox": [], "cursor": None}
            seen.append((str(msg["Subject"]) in [m["subject"] for m in last["outbox"]], last["cursor"]))

        fetch = lambda since: {"Result": "OK", "events": events, "next": 9, "more": False,
                               "users": self.users()}
        state = gn.poll_once(gn.new_state(), cfg, L(10, 0), fetch, transport=transport,
                             log=lambda m: None, persist=persist)
        self.assertEqual([(len(s["outbox"]), s["cursor"]) for s in snapshots], [(1, 9), (0, 9)])
        self.assertEqual(seen, [(True, 9)], "the email being sent was not in the persisted state")
        self.assertEqual(state["outbox"], [])

    def test_malformed_event_is_skipped_once_and_cursor_advances(self):
        cfg = base_cfg()
        at = int(L(10, 0).timestamp())
        good1 = self.upload(1, at)
        bad = {"id": 2, "at": at, "kind": "download", "actor": 3, "owner": "not-a-number",
               "file": {"id": "x", "name": "x", "size": 1, "kind": "image"}, "group": ICELAND,
               "via": "group", "detail": ""}
        bad2 = {"id": 3, "at": None, "kind": "upload", "actor": 2, "owner": 2, "file": "oops",
                "group": None, "via": "", "detail": ""}
        good2 = {"id": 4, "at": at, "kind": "download", "actor": 3, "owner": 2,
                 "file": good1["file"], "group": ICELAND, "via": "group", "detail": ""}
        logs = []
        state, _ = gn.step(gn.new_state(), {"events": [good1, bad, bad2, good2], "next": 4,
                                            "users": self.users()}, L(10, 0), cfg, log=logs.append)
        self.assertEqual([e["id"] for e in state["held"]], [1, 4])
        self.assertEqual([e["id"] for e in state["friends"]["2"]], [4])
        self.assertEqual(state["cursor"], 4)
        self.assertEqual(len([m for m in logs if m.startswith("skipped malformed event")]), 2, logs)
        self.assertTrue(any("id=2" in m for m in logs) and any("id=3" in m for m in logs), logs)
        # The next poll does not fold them again.
        state, _ = gn.step(state, {"events": [], "next": 4, "users": self.users()},
                           L(10, 1), cfg, log=logs.append)
        self.assertEqual([e["id"] for e in state["held"]], [1, 4])
        json.dumps(state)

    def test_fetch_maps_incomplete_read_to_feed_error(self):
        import http.client
        import unittest.mock as mock
        with mock.patch("urllib.request.urlopen",
                        side_effect=http.client.IncompleteRead(b"partial")):
            with self.assertRaises(gn.FeedError):
                gn.make_fetch(base_cfg())(5)

    def test_once_logs_unsent_outbox(self):
        tmp = tempfile.mkdtemp()
        try:
            path = os.path.join(tmp, "c.json")
            with open(path, "w") as fh:
                json.dump({"api_key": "k", "smtp_user": "s@example.com", "smtp_app_password": "p",
                           "mail_to": FRANCO}, fh)
            at = int(datetime.now(timezone.utc).timestamp())
            feed = {"Result": "OK", "next": 1, "more": False, "users": self.users(),
                    "events": [{"id": 1, "at": at, "kind": "first_signin", "actor": 5, "owner": 0,
                                "file": None, "group": None, "via": "", "detail": ""}]}
            import contextlib
            import io
            import unittest.mock as mock
            out = io.StringIO()
            refused = smtplib.SMTPAuthenticationError(535, b"bad login")
            with mock.patch.object(gn, "make_fetch", return_value=lambda since: feed), \
                    mock.patch.object(gn, "_smtp_transport",
                                      return_value=mock.Mock(side_effect=refused)), \
                    contextlib.redirect_stdout(out):
                gn.main(["--once", "--config", path])             # returns: exit code 0
            self.assertIn("outbox 1 unsent", out.getvalue())
        finally:
            shutil.rmtree(tmp)

    def test_held_times_are_event_times_not_poll_times(self):
        cfg = base_cfg()
        early, late = int(L(9, 0).timestamp()), int(L(9, 20).timestamp())
        state = gn.fold(gn.new_state(), {"events": [self.upload(1, early), self.upload(2, late)],
                                         "next": 2, "users": self.users()})
        self.assertEqual((state["held_first_at"], state["held_last_at"]), (early, late))
        # Arriving late at 12:00 after an outage: the 3-hour cap counts from 09:00, so it fires now.
        emails = gn.emit(state, L(12, 0), cfg)
        self.assertEqual([e["kind"] for e in emails], ["summary"])

    def test_admin_owner_with_setting_on_gets_no_digest(self):
        cfg = base_cfg()
        users = self.users()
        users[0] = dict(users[0], emailDownloads=True)            # Franco, admin
        at = int(L(10, 0).timestamp())
        events = [{"id": 1, "at": at, "kind": "download", "actor": 3, "owner": 1,
                   "file": {"id": "c1", "name": "CV.pdf", "size": 1, "kind": "file"},
                   "group": None, "via": "file", "detail": ""}]
        state, _ = gn.step(gn.new_state(), {"events": events, "next": 1, "users": users}, L(10, 0), cfg)
        self.assertEqual(state["friends"], {})
        state, emails = gn.step(state, {"events": [], "next": 1, "users": users}, L(19, 0), cfg)
        self.assertEqual([e for e in emails if e["kind"] == "friend"], [])


class FriendCap(unittest.TestCase):
    def test_over_daily_cap_waits_for_tomorrow(self):
        cfg = base_cfg(max_friend_emails_per_day=1)
        users = [dict(u) for u in USERS]
        users[2] = dict(users[2], emailDownloads=True)       # Ben on as well
        now = L(10, 0)
        at = int(now.timestamp())
        f = {"id": "x", "name": "x.pdf", "size": 1, "kind": "file"}
        events = [
            {"id": 1, "at": at, "kind": "download", "actor": 4, "owner": 2, "file": f,
             "group": None, "via": "file", "detail": ""},
            {"id": 2, "at": at, "kind": "download", "actor": 4, "owner": 3, "file": f,
             "group": None, "via": "file", "detail": ""},
            {"id": 3, "at": at, "kind": "download", "actor": 2, "owner": 2, "file": f,
             "group": None, "via": "file", "detail": ""},        # own download: never counts
        ]
        state, _ = gn.step(gn.new_state(), {"events": events, "next": 3, "users": users}, now, cfg)
        self.assertEqual(list(state["friends"]), ["2", "3"])
        self.assertEqual(len(state["friends"]["2"]), 1)
        state, emails = gn.step(state, {"events": [], "next": 3, "users": users}, L(19, 0), cfg)
        self.assertEqual([e["to"] for e in emails if e["kind"] == "friend"], ["anna@example.com"])
        state, emails = gn.step(state, {"events": [], "next": 3, "users": users}, L(19, 30), cfg)
        self.assertEqual([e for e in emails if e["kind"] == "friend"], [])
        state, emails = gn.step(state, {"events": [], "next": 3, "users": users}, L(19, 0, day=15), cfg)
        self.assertEqual([e["to"] for e in emails if e["kind"] == "friend"], ["ben@example.com"])


class Delivery(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()

    def tearDown(self):
        shutil.rmtree(self.tmp)

    def mail(self):
        return {"to": "anna@example.com", "subject": "Your files were downloaded",
                "body": "hello\n", "kind": "friend"}

    def test_redirect_rewrites_every_recipient(self):
        got = []
        cfg = base_cfg(redirect_all_to="franco+preview@example.com",
                       api_url="http://127.0.0.1:53900/api/activity")
        gn.deliver(cfg, self.mail(), transport=lambda m, f, r: got.append((m, f, r)))
        msg, from_addr, rcpts = got[0]
        self.assertEqual(rcpts, ["franco+preview@example.com"])
        self.assertEqual(msg["To"], "franco+preview@example.com")
        self.assertIsNone(msg["Cc"])
        self.assertIsNone(msg["Bcc"])
        self.assertEqual(msg["X-Original-To"], "anna@example.com")

    def test_refuses_non_live_api_without_redirect(self):
        cfg = base_cfg(api_url="http://127.0.0.1:53900/api/activity")
        called = []
        with self.assertRaises(gn.UnsafeConfig):
            gn.deliver(cfg, self.mail(), transport=lambda *a: called.append(a))
        self.assertEqual(called, [])
        # The live URL without a redirect is fine.
        gn.deliver(base_cfg(), self.mail(), transport=lambda *a: called.append(a))
        self.assertEqual(len(called), 1)

    def test_dir_sink_writes_eml(self):
        cfg = base_cfg(deliver="dir:" + self.tmp)
        gn.deliver(cfg, self.mail())
        files = [n for n in os.listdir(self.tmp) if n.endswith(".eml")]
        self.assertEqual(len(files), 1)
        with open(os.path.join(self.tmp, files[0]), "rb") as fh:
            msg = email.message_from_bytes(fh.read())
        self.assertEqual(msg["To"], "anna@example.com")
        self.assertEqual(msg["Subject"], "Your files were downloaded")

    def test_header_injection_is_flattened(self):
        got = []
        m = self.mail()
        m["subject"] = "[FGS] New person: Eve\r\nBcc: x@evil.example (e@x)"
        gn.deliver(base_cfg(), m, transport=lambda msg, f, r: got.append((msg, r)))
        self.assertEqual(got[0][1], ["anna@example.com"])
        self.assertIsNone(got[0][0]["Bcc"])
        self.assertNotIn("\n", str(got[0][0]["Subject"]))

    def test_outbox_capped_at_200(self):
        state = gn.new_state()
        logs = []
        for i in range(205):
            gn.enqueue(state, [{"to": FRANCO, "subject": "s%d" % i, "body": "", "kind": "summary"}],
                       log=logs.append)
        self.assertEqual(len(state["outbox"]), 200)
        self.assertEqual(state["outbox_dropped"], 5)
        self.assertEqual(state["outbox"][0]["subject"], "s5")


class Feed(unittest.TestCase):
    def users(self):
        return [dict(u) for u in USERS]

    def test_reset_moves_cursor_keeps_held(self):
        cfg = base_cfg()
        now = L(10, 0)
        ev = {"id": 41, "at": int(now.timestamp()), "kind": "upload", "actor": 2, "owner": 2,
              "file": {"id": "a", "name": "a.jpg", "size": 1, "kind": "image"},
              "group": ICELAND, "via": "", "detail": ""}
        state = gn.new_state()
        state, _ = gn.step(state, {"events": [ev], "next": 41, "users": self.users()}, now, cfg)
        logs = []
        state, emails = gn.step(state, {"events": [], "next": 7, "reset": True, "users": self.users()},
                                now + timedelta(minutes=1), cfg, log=logs.append)
        self.assertEqual(state["cursor"], 7)
        self.assertEqual(len(state["held"]), 1)
        self.assertEqual(emails, [])
        self.assertEqual(logs, ["feed reset: cursor 41 -> 7"])

    def test_from_start_sends_since_zero_once(self):
        cfg = base_cfg()
        calls = []

        def fetch(since):
            calls.append(since)
            return {"Result": "OK", "events": [], "next": 12, "more": False,
                    "reset": False, "users": self.users()}

        state = gn.new_state()
        sink = lambda *a: None
        state = gn.poll_once(state, cfg, L(10, 0), fetch, transport=sink, log=sink,
                             from_start=True, persist=sink)
        state = gn.poll_once(state, cfg, L(10, 1), fetch, transport=sink, log=sink,
                             from_start=True, persist=sink)
        self.assertEqual(calls, [0, 12])
        # Without the flag, no cursor means no since header at all ("start from now").
        calls[:] = []
        gn.poll_once(gn.new_state(), cfg, L(10, 0), fetch, transport=sink, log=sink,
                     from_start=False, persist=sink)
        self.assertEqual(calls, [None])

    def test_feed_failure_logged_once_per_streak(self):
        cfg = base_cfg()
        outcome = {"fail": True}

        def fetch(since):
            if outcome["fail"]:
                raise gn.FeedError(urllib.error.URLError("connection refused"))
            return {"Result": "OK", "events": [], "next": 3, "more": False,
                    "reset": False, "users": self.users()}

        logs = []
        sent = []
        state = gn.new_state()
        t = L(10, 0)
        for i in range(5):
            state = gn.poll_once(state, cfg, t + timedelta(minutes=i), fetch,
                                 transport=lambda *a: sent.append(a), log=logs.append,
                                 persist=lambda s: None)
        outcome["fail"] = False
        for i in range(5, 8):
            state = gn.poll_once(state, cfg, t + timedelta(minutes=i), fetch,
                                 transport=lambda *a: sent.append(a), log=logs.append,
                                 persist=lambda s: None)
        unreachable = [m for m in logs if m.startswith("feed unreachable since")]
        recovered = [m for m in logs if m.startswith("feed recovered")]
        self.assertEqual(len(unreachable), 1, logs)
        self.assertIn("connection refused", unreachable[0])
        self.assertEqual(len(recovered), 1, logs)
        self.assertEqual(sent, [])
        self.assertIsNone(state["failing"]["feed"])

    def test_once_exits_nonzero_when_feed_unread(self):
        tmp = tempfile.mkdtemp()
        try:
            path = os.path.join(tmp, "c.json")
            with open(path, "w") as fh:
                json.dump({"api_key": "k", "smtp_user": "s@example.com", "smtp_app_password": "p",
                           "mail_to": FRANCO, "api_url": "http://127.0.0.1:9/api/activity",
                           "redirect_all_to": FRANCO, "deliver": "dir:" + os.path.join(tmp, "out")}, fh)
            import contextlib
            import io
            with contextlib.redirect_stdout(io.StringIO()):
                with self.assertRaises(SystemExit) as cm:
                    gn.main(["--once", "--from-start", "--config", path])
            self.assertNotIn(cm.exception.code, (0, None))
            with open(os.path.join(tmp, ".notify-state-v2.json")) as fh:
                self.assertIsNone(json.load(fh)["cursor"])
        finally:
            shutil.rmtree(tmp)

    def test_more_pages_are_drained_in_one_poll(self):
        cfg = base_cfg()
        calls = []
        answers = [
            {"Result": "OK", "events": [], "next": 500, "more": True, "users": self.users()},
            {"Result": "OK", "events": [], "next": 650, "more": False, "users": self.users()},
        ]

        def fetch(since):
            calls.append(since)
            return answers[len(calls) - 1]

        state = gn.new_state()
        state["cursor"] = 10
        sink = lambda *a: None
        state = gn.poll_once(state, cfg, L(10, 0), fetch, transport=sink, log=sink, persist=sink)
        self.assertEqual(calls, [10, 500])
        self.assertEqual(state["cursor"], 650)


class Formatting(unittest.TestCase):
    def test_size_si(self):
        self.assertEqual(gn.size_si(999), "999 B")
        self.assertEqual(gn.size_si(2100000000), "2.1 GB")
        self.assertEqual(gn.size_si(12300000), "12.3 MB")
        self.assertEqual(gn.size_si(2345678), "2.3 MB")

    def test_people_list(self):
        self.assertEqual(gn.people_phrase(["Ben"]), "Ben")
        self.assertEqual(gn.people_phrase(["Ben", "Carla"]), "Ben and Carla")
        self.assertEqual(gn.people_phrase(["Ben", "Carla", "Dario"]), "Ben, Carla and Dario")
        self.assertEqual(gn.people_phrase(["Ben", "Carla", "D", "E", "F"]), "Ben, Carla and 3 others")

    def test_immediate_subjects_joined_by_priority(self):
        cfg = base_cfg()
        now = L(10, 0)
        at = int(now.timestamp())
        events = [
            {"id": 1, "at": at, "kind": "first_signin", "actor": 5, "owner": 0, "file": None,
             "group": None, "via": "", "detail": ""},
            {"id": 2, "at": at, "kind": "warning_disk", "actor": 0, "owner": 0, "file": None,
             "group": None, "via": "", "detail": "1"},
            {"id": 3, "at": at, "kind": "viewas_start", "actor": 1, "owner": 0, "file": None,
             "group": None, "via": "", "detail": "target=2"},
        ]
        _, emails = gn.step(gn.new_state(), {"events": events, "next": 3, "users": USERS}, now, cfg)
        self.assertEqual(len(emails), 1)
        self.assertEqual(emails[0]["subject"],
                         "[FGS] View as…: Franco viewing as Anna; Warning: 1 issue(s); "
                         "New person: Elif (elif@example.com)")


def write_fixture():
    with open(FIXTURE_PATH, "w") as fh:
        fx = build_fixture()
        fh.write('{"users": ' + json.dumps(fx["users"]) + ',\n "pages": [\n')
        fh.write(",\n".join("  " + json.dumps(p, ensure_ascii=False) for p in fx["pages"]))
        fh.write("\n]}\n")


if __name__ == "__main__":
    unittest.main()
