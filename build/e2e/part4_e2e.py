#!/usr/bin/env python3
"""End-to-end test of Part 4 (activity feed, Settings, names and photos,
blocklist, View as… rows, the notifier) against a local Gokapi fork on
127.0.0.1:53900, started with -e GOKAPI_E2E_SEED=1 and set up by
setup_local.py.

The fixture is build/e2e/seed_part4.py, run as a subprocess: seed_part3's
users A and B (with email names, "Anna Muster" and "Ben Keller"), group G with
230 + 20 PNGs, A's photo, user C in no group, a second admin with an email,
and an admin API key with ManageLogs.

  1. feed         start-from-now, since 0, downloads, paging, keys, ?since=
  2. settings     copy, display name, the email switch on two pages, 405
  3. names        B sees "Anna Muster" and A's photo; no email leaks
  4. avatar       C, who shares no group, gets 404
  5. blocklist    a domain block ends B's session, key and login; admins refused
  6. View as…     rows in the feed; /settings read-only, save refused
  7. notifier     --once --from-start into a directory, every To redirected

The feed is read with a bare urllib request (no cookies). Every View-as and
every block is undone in a finally, so a failure cannot leak into the suites
that run after this one. Exit status 1 on any FAIL.
"""
import email, email.policy, glob, html, importlib.util, json, os, re, shutil, subprocess, sys, time
import urllib.error, urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.dirname(os.path.dirname(HERE))
spec = importlib.util.spec_from_file_location("seed_part3", os.path.join(HERE, "seed_part3.py"))
seed = importlib.util.module_from_spec(spec)
spec.loader.exec_module(seed)
Client, BASE = seed.Client, seed.BASE
spec4 = importlib.util.spec_from_file_location("seed_part4", os.path.join(HERE, "seed_part4.py"))
seed4 = importlib.util.module_from_spec(spec4)
spec4.loader.exec_module(seed4)
REDIRECT = os.environ.get("GOKAPI_E2E_REDIRECT", "owner@example.com")
MAIL_DIR = os.path.expanduser("~/src/Gokapi-wt/e2e-mail")
CODE_INVALID_URL, CODE_INVALID_KEY, CODE_ADMIN_ONLY = 1, 2, 3

ok = True
counts = {"pass": 0, "fail": 0}


def check(label, cond, detail=""):
    global ok
    counts["pass" if cond else "fail"] += 1
    if not cond:
        ok = False
    print("  %-74s %s %s" % (label, "PASS" if cond else "FAIL", detail), flush=True)


def api(path, key, headers=None, method="GET"):
    """A cookie-less API request: (status, parsed JSON or raw bytes)"""
    h = {"apikey": key} if key else {}
    h.update(headers or {})
    r = urllib.request.Request(BASE + "/api" + path, method=method, headers=h)
    try:
        x = urllib.request.urlopen(r)
        s, b = x.status, x.read()
    except urllib.error.HTTPError as e:
        s, b = e.code, e.read()
    try:
        return s, json.loads(b)
    except ValueError:
        return s, b


def feed(key, since=None, limit=None):
    h = {}
    if since is not None:
        h["since"] = str(since)
    if limit is not None:
        h["limit"] = str(limit)
    return api("/activity", key, h)


def walk_feed(key, since, limit=1000):
    """Every page after since; returns (events, pages)"""
    events, pages = [], []
    while True:
        s, j = feed(key, since, limit)
        if s != 200:
            raise RuntimeError("feed %s -> %s %s" % (since, s, j))
        pages.append(j)
        events += j["events"]
        since = j["next"]
        if not j["more"] or len(pages) > 100:
            return events, pages


def login(name, pw):
    c = Client(name)
    return c, c.login(name, pw)


def key_of(client, perm_grant=None, basic=True, friendly="e2e"):
    """A persistent API key of the client's (with basic permissions), plus perm_grant"""
    mod = client.token("PERM_API_MOD")
    s, b, _ = client.req("POST", "/api/auth/create", b"",
                         {"apikey": mod, "friendlyName": friendly, "basicPermissions": "true" if basic else "false"})
    key = json.loads(b)["Id"]
    if perm_grant:
        s, b, _ = client.req("POST", "/api/auth/modify", b"",
                             {"apikey": mod, "targetKey": key, "permission": perm_grant, "permissionModifier": "GRANT"})
        if s != 200:
            raise RuntimeError("auth/modify %s -> %s %s" % (perm_grant, s, b[:200]))
    return key


def body_json(b):
    try:
        return json.loads(b)
    except ValueError:
        return {}


# ---- fixture ------------------------------------------------------------------
proc = subprocess.run([sys.executable, os.path.join(HERE, "seed_part4.py")],
                      stdout=subprocess.PIPE, stderr=sys.stderr, text=True)
if proc.returncode != 0:
    print("seed failed"); sys.exit(1)
fx = json.loads(proc.stdout.strip().splitlines()[-1])
KEY, GID, GNAME = fx["apikey"], fx["group"], fx["groupName"]
ids_a, ids_b = fx["ids"]["A"], fx["ids"]["B"]
admin, s = login(seed.ADMIN, seed.ADMIN_PW)
A, _ = login(fx["A"]["name"], fx["A"]["password"])
B, _ = login(fx["B"]["name"], fx["B"]["password"])
C, _ = login(fx["C"]["name"], fx["C"]["password"])
A.id, B.id, C.id = fx["A"]["id"], fx["B"]["id"], fx["C"]["id"]
ADMIN_ID = 1

# ---- 1. feed -----------------------------------------------------------------
print("1. feed")
s, now = feed(KEY)
everything, pages = walk_feed(KEY, 0)
newest = everything[-1]["id"] if everything else -1
check("no since header: 200, no events, next is the newest id",
      s == 200 and now.get("events") == [] and now.get("next") == newest and not now.get("reset"),
      "%s next=%s newest=%s" % (s, now.get("next"), newest))
check("users lists A with name, email and emailDownloads",
      any(u["id"] == A.id and u["name"] == "Anna Muster" and u["email"] == fx["A"]["name"] and not u["admin"]
          and "emailDownloads" in u for u in now.get("users", [])))
uploads = {e["file"]["id"]: e for e in everything if e["kind"] == "upload" and e.get("file")}
seeded = set(ids_a) | set(ids_b)
check("since 0: an upload row for each of the 250 seeded files", seeded <= set(uploads), "%d of 250" % len(seeded & set(uploads)))
check("  every seeded upload carries the group's name",
      all((uploads[i].get("group") or {}).get("name") == GNAME for i in seeded & set(uploads)))
check("  A's uploads have actor and owner A, B's actor and owner B",
      all(uploads[i]["actor"] == uploads[i]["owner"] == A.id for i in set(ids_a) & set(uploads)) and
      all(uploads[i]["actor"] == uploads[i]["owner"] == B.id for i in set(ids_b) & set(uploads)))
check("  upload rows say image, with name and size",
      all(uploads[i]["file"]["kind"] == "image" and uploads[i]["file"]["name"].startswith("p3-") and uploads[i]["file"]["size"] > 0
          for i in seeded & set(uploads)))
created = [e for e in everything if e["kind"] == "group_created" and (e.get("group") or {}).get("id") == GID]
check("since 0: group_created by A, with the group's name",
      len(created) == 1 and created[0]["actor"] == A.id and created[0]["group"]["name"] == GNAME, str(created)[:120])
joined = [e for e in everything if e["kind"] == "group_joined" and (e.get("group") or {}).get("id") == GID]
check("since 0: group_joined by B, with the group's name",
      len(joined) == 1 and joined[0]["actor"] == B.id and joined[0]["group"]["name"] == GNAME, str(joined)[:120])
check("since 0: ids strictly increase", all(a["id"] < b["id"] for a, b in zip(everything, everything[1:])))

# paging with limit 100
paged, ppages = walk_feed(KEY, 0, limit=100)
check("limit 100: pages of at most 100, more until the last page",
      all(len(p["events"]) <= 100 for p in ppages) and all(p["more"] for p in ppages[:-1]) and not ppages[-1]["more"]
      and len(ppages) == (len(everything) + 99) // 100, "%d pages for %d events" % (len(ppages), len(everything)))
check("limit 100: next is the last id of each page",
      all(p["next"] == p["events"][-1]["id"] for p in ppages if p["events"]))
check("limit 100: the walk equals the one-call list, nothing repeated",
      [e["id"] for e in paged] == [e["id"] for e in everything])
s, j = feed(KEY, 0, 1)
check("limit 1 from 0: one event, more", s == 200 and len(j["events"]) == 1 and j["more"], str(s))
s, j = feed(KEY, newest + 50)
check("since above the newest id: reset, no events, next = newest",
      s == 200 and j["reset"] and j["events"] == [] and j["next"] == newest, str(j.get("reset")))

# downloads
mark = newest
fa, fb = ids_a[0], ids_a[1]
s, body, h = B.get("/gd?id=%s&file=%s" % (GID, fa))
check("B downloads one of A's files from the group (/gd): 200", s == 200 and len(body) > 0, str(s))
anon = Client("anonymous")
s, body, h = anon.get("/downloadFile?id=" + fb)
check("an anonymous /downloadFile of another of A's files: 200", s == 200 and len(body) > 0, str(s))
bkey_dl = key_of(B, "PERM_DOWNLOAD", basic=False, friendly="e2e download")
s, body = api("/files/download/" + ids_b[0], bkey_dl)
check("B downloads one of B's own through the API (/api/files/download/): 200", s == 200 and isinstance(body, bytes), str(s))
s, body, _ = B.req("GET", "/api/files/downloadzip", headers={"apikey": bkey_dl, "ids": ids_b[1]})
check("B downloads another of B's as an API zip (/api/files/downloadzip): 200", s == 200, str(s))
time.sleep(0.5)
new, _ = walk_feed(KEY, mark)
dl = [e for e in new if e["kind"] == "download"]
by = {e["file"]["id"]: e for e in dl}
check("download by B via group: actor B, owner A, the group's name",
      fa in by and by[fa]["actor"] == B.id and by[fa]["owner"] == A.id and by[fa]["via"] == "group"
      and (by[fa].get("group") or {}).get("name") == GNAME, str(by.get(fa))[:160])
check("anonymous download: actor 0, via file", fb in by and by[fb]["actor"] == 0 and by[fb]["via"] == "file",
      str(by.get(fb))[:160])
check("API download: actor B, via file (not api)", ids_b[0] in by and by[ids_b[0]]["actor"] == B.id
      and by[ids_b[0]]["via"] == "file", str(by.get(ids_b[0]))[:160])
check("API zip download: actor B, via zip (not api)", ids_b[1] in by and by[ids_b[1]]["actor"] == B.id
      and by[ids_b[1]]["via"] == "zip", str(by.get(ids_b[1]))[:160])
check("no download row says via api", all(e["via"] != "api" for e in dl))

# keys
s, j = api("/user/modify", admin.token("PERM_MANAGE_USERS"),
           {"userid": str(C.id), "userpermission": "PERM_LOGS", "permissionModifier": "GRANT"}, "POST")
check("C (not an admin) is given the Manage logs permission", s == 200, "%s %s" % (s, j))
ckey = key_of(C, "PERM_MANAGE_LOGS", friendly="e2e non-admin logs")
s, j = feed(ckey, 0)
check("a non-admin key with ManageLogs: 401 AdminOnly",
      s == 401 and isinstance(j, dict) and j.get("ErrorCode") == CODE_ADMIN_ONLY, "%s %s" % (s, j))
plain = key_of(admin, None, friendly="e2e no logs")
s, j = feed(plain, 0)
check("an admin key without ManageLogs: 401 InvalidApiKey",
      s == 401 and isinstance(j, dict) and j.get("ErrorCode") == CODE_INVALID_KEY, "%s %s" % (s, j))
s, j = api("/activity?since=0", KEY)
check("?since= in the query string does not route: 400 InvalidUrl",
      s == 400 and isinstance(j, dict) and j.get("ErrorCode") == CODE_INVALID_URL, "%s %s" % (s, j))
s, j = api("/activity", None, {"since": "0"})
check("no key at all: 401", s == 401, str(s))

# ---- 2. settings -----------------------------------------------------------------
print("2. settings")
s, page = A.text("/settings")
copy = ["Settings", "Your name", "Your photo", "Emails", "Display name",
        "Shown to the people you share with. Leave empty to use your Google name.",
        "Use initials instead of my Google photo", "Email me when my files are downloaded",
        "At most one email a day, at 19:00, and only if someone downloaded something.", ">Save<", 'data-default="Saved"']
missing = [c for c in copy if c not in page]
check("A's /settings: 200 with every copy string", s == 200 and not missing, "missing %s" % missing)
check("  A's page has no no-email note", "Your account has no email address" not in page)
check("  A's page shows A's photo", re.search(r'<img src="\./avatar\?id=%d&amp;v=\d+"' % A.id, page) is not None)


def shown(client):
    s, page = client.text("/settings")
    m = re.search(r'id="settingsDisplayName"[^>]*value="([^"]*)"', page)
    n = re.search(r'id="settingsShownName"[^>]*>([^<]*)<', page)
    return (html.unescape(m.group(1)) if m else None), (html.unescape(n.group(1)) if n else None)


s, b, _ = A.post_json("/settings/save", {"displayName": "  Anna M.  "})
check("display name 'Anna M.' saves (trimmed) and answers the shown name",
      s == 200 and body_json(b).get("Name") == "Anna M.", "%s %s" % (s, b[:80]))
check("  /settings shows it", shown(A) == ("Anna M.", "Anna M."), str(shown(A)))
s, b, _ = A.post_json("/settings/save", {"displayName": "x" * 40})
check("40 characters: 200", s == 200, str(s))
s, b, _ = A.post_json("/settings/save", {"displayName": "y" * 41})
check("41 characters: 400 and nothing changes", s == 400 and shown(A)[0] == "x" * 40, "%s %s" % (s, b[:80]))
s, b, _ = A.post_json("/settings/save", {"displayName": ""})
check("empty: back to the default (the part before @)",
      s == 200 and body_json(b).get("Name") == fx["A"]["name"].split("@")[0], "%s %s" % (s, b[:80]))
s, b, _ = A.post_json("/settings/save", {"displayName": "Anna Muster"})
check("restored to 'Anna Muster'", s == 200 and shown(A) == ("Anna Muster", "Anna Muster"))


def switch_on(page, element_id):
    m = re.search(r'<input[^>]*id="%s"[^>]*>' % element_id, page, re.S)
    return None if not m else (" checked" in m.group(0))


s, b, _ = A.post_json("/settings/save", {"emailDownloads": True})
st, ad = A.text("/settings")[1], A.text("/admin")[1]
check("email switch on via /settings/save: on in /settings and on the Upload page",
      s == 200 and switch_on(st, "settingsEmailDownloads") is True and switch_on(ad, "fgs-email-downloads") is True,
      "%s %s %s" % (s, switch_on(st, "settingsEmailDownloads"), switch_on(ad, "fgs-email-downloads")))
check("  the Upload page says 'Email me when my files are downloaded'", "Email me when my files are downloaded" in ad)
check("  the feed reports emailDownloads true for A",
      any(u["id"] == A.id and u["emailDownloads"] is True for u in feed(KEY)[1]["users"]))
s, b, _ = A.post_json("/settings/save", {"emailDownloads": False})
st, ad = A.text("/settings")[1], A.text("/admin")[1]
check("switched off: off on both pages",
      s == 200 and switch_on(st, "settingsEmailDownloads") is False and switch_on(ad, "fgs-email-downloads") is False)
s, b, _ = A.post_json("/settings/save", {"emailDownloads": True})   # on, for the notifier's users list
s, page = admin.text("/settings")
check("tester (no email) sees the no-email note, the switch disabled",
      s == 200 and "Your account has no email address, so it cannot get emails." in page
      and re.search(r'id="settingsEmailDownloads"[^>]*disabled', page) is not None)
s, page = admin.text("/admin")
check("  and no switch on the Upload page", 'id="fgs-email-downloads"' not in page)
s, b, _ = admin.post_json("/settings/save", {"emailDownloads": True})
check("  saving emailDownloads true for tester: 400", s == 400, "%s %s" % (s, b[:100]))
check("GET /settings/save: 405", A.get("/settings/save")[0] == 405)
s, _, _ = A.req("POST", "/settings/save", b"displayName=x", {"Content-Type": "application/x-www-form-urlencoded"})
check("a form POST to /settings/save is refused (not JSON)", s in (400, 415), str(s))

# ---- 3. names and photos -------------------------------------------------------
print("3. names and photos")
s, page = B.text("/g?id=" + GID)
check("B's /g shows 'Anna Muster'", s == 200 and "Anna Muster" in page, str(s))
m = re.search(r'<img[^>]*src="(\./avatar\?id=%d&amp;v=\d+)"' % A.id, page) or \
    re.search(r'data-[\w-]*photo[\w-]*="(\./avatar\?id=%d&amp;v=\d+)"' % A.id, page)
check("  with A's photo as an /avatar URL", m is not None)
avatar = html.unescape(m.group(1))[1:] if m else "/avatar?id=%d&v=0" % A.id
s, body, h = B.get(avatar)
check("  B fetches it: 200 image/jpeg, the seeded bytes",
      s == 200 and h.get("Content-Type") == "image/jpeg" and body == seed4.PHOTO_JPEG,
      "%s %s" % (s, h.get("Content-Type")))
check("  B's own initials BK appear (B has no photo)", re.search(r'>\s*BK\s*<', page) is not None)
leaks = []
for path in ("/g?id=" + GID, "/groups", "/admin", "/settings", "/gl?id=%s&limit=all" % GID,
             "/groups/item?id=%s&file=%s" % (GID, ids_a[0])):
    s, text = B.text(path)
    if fx["A"]["name"].lower() in text.lower():
        leaks.append(path)
check("A's email appears nowhere in B's pages and feeds", not leaks, str(leaks))
s, page = admin.text("/users")
check("the admin's Users page shows 'Anna Muster' and A's email",
      "Anna Muster" in page and fx["A"]["name"] in page)

# ---- 4. avatar privacy ---------------------------------------------------------
print("4. avatar privacy")
check("C, sharing no group with A, gets 404 for A's avatar", C.get(avatar)[0] == 404)
check("the admin gets 200", admin.get(avatar)[0] == 200)
check("A gets 200 for their own", A.get(avatar)[0] == 200)
check("anonymous: no photo (redirect to login or 404)", Client("anon2").get(avatar)[0] in (302, 303, 307, 404))

# ---- 5. blocklist ----------------------------------------------------------------
print("5. blocklist")
bkey = key_of(B, None, friendly="e2e B view")
s, _ = api("/files/list", bkey)
check("before: B's key lists files (200)", s == 200, str(s))
check("before: B's session reaches /groups", B.get("/groups")[0] == 200)
mark = feed(KEY)[1]["next"]
s, b, _ = B.post_json("/users/block", {"entry": "@example.org"})
check("a non-admin cannot block (403)", s == 403, str(s))
domain = fx["bDomain"]
try:
    s, b, _ = admin.post_json("/users/block", {"entry": domain})
    check("admin blocks %s: 200" % domain, s == 200 and body_json(b).get("Result") == "OK", "%s %s" % (s, b[:100]))
    s, page = admin.text("/users")
    check("  the Users page lists it under Blocked", "<h5>Blocked</h5>" in page and 'data-fgs-unblock="%s"' % domain in page)
    s, _, h = B.get("/groups")
    check("  B's session is gone (/groups no longer 200)", s != 200, "%s %s" % (s, h.get("Location")))
    s, j = api("/files/list", bkey)
    check("  B's API key gets 401", s == 401, str(s))
    B2, s = login(fx["B"]["name"], fx["B"]["password"])
    check("  B's password login fails", s not in (302, 303, 307) and B2.get("/groups")[0] != 200, str(s))
    s, b, _ = admin.post_json("/users/block", {"entry": fx["admin2"]["name"]})
    check("blocking an admin's email: 400 'You cannot block an admin.'",
          s == 400 and body_json(b).get("ErrorMessage") == "You cannot block an admin.", "%s %s" % (s, b[:100]))
    s, b, _ = admin.post_json("/users/block", {"entry": "@" + fx["admin2"]["name"].split("@")[1]})
    check("blocking an admin's domain: 400 'You cannot block an admin.'",
          s == 400 and body_json(b).get("ErrorMessage") == "You cannot block an admin.", "%s %s" % (s, b[:100]))
    check("GET /users/block: 405", admin.get("/users/block")[0] == 405)
finally:
    s, b, _ = admin.post_json("/users/unblock", {"entry": domain})
check("unblock: 200", s == 200, str(s))
B, s = login(fx["B"]["name"], fx["B"]["password"])
B.id = fx["B"]["id"]
check("  B logs in again", s in (302, 303, 307) and B.get("/groups")[0] == 200, str(s))
s, _ = api("/files/list", bkey)
check("  B's key works again", s == 200, str(s))
new, _ = walk_feed(KEY, mark)
blocked = [e for e in new if e["kind"] == "signin_blocked"]
check("one signin_blocked row, actor 0, detail = B's email",
      len(blocked) == 1 and blocked[0]["actor"] == 0 and blocked[0]["detail"] == fx["B"]["name"], str(blocked)[:200])

# ---- 6. View as… ---------------------------------------------------------------
print("6. View as…")
mark = feed(KEY)[1]["next"]
try:
    s, b, _ = admin.post_json("/viewas/start", {"userId": A.id})
    check("start View as A: 200", s == 200 and body_json(b).get("Result") == "OK", "%s %s" % (s, b[:80]))
    s, page = admin.text("/settings")
    check("  /settings renders A's settings read-only",
          s == 200 and 'data-readonly="true"' in page and 'id="settingsSave"' not in page
          and re.search(r'id="settingsDisplayName"[^>]*disabled', page) is not None
          and re.search(r'id="settingsEmailDownloads"[^>]*disabled', page) is not None
          and "Anna Muster" in page, str(s))
    s, b, _ = admin.post_json("/settings/save", {"displayName": "Hijacked"})
    check("  POST /settings/save: 403", s == 403, "%s %s" % (s, b[:80]))
    check("  and A's name is unchanged", shown(A)[1] == "Anna Muster")
    s, b, _ = admin.req("GET", avatar)
    check("  /avatar is allowed (200)", s == 200, str(s))
finally:
    s, b, _ = admin.post_json("/viewas/end", {})
check("end View as: 200", s == 200 and body_json(b).get("Result") == "OK", "%s %s" % (s, b[:80]))
new, _ = walk_feed(KEY, mark)
starts = [e for e in new if e["kind"] == "viewas_start"]
ends = [e for e in new if e["kind"] == "viewas_end"]
check("a viewas_start row: actor admin, detail target=A",
      len(starts) == 1 and starts[0]["actor"] == ADMIN_ID and starts[0]["detail"] == "target=%d" % A.id, str(starts)[:160])
check("a viewas_end row: actor admin, detail target=A reason=…",
      len(ends) == 1 and ends[0]["actor"] == ADMIN_ID and ends[0]["detail"].startswith("target=%d reason=" % A.id), str(ends)[:160])
s, page = admin.text("/settings")
check("after the end, tester's own /settings is editable again", 'data-readonly="false"' in page and 'id="settingsSave"' in page)

# ---- 7. the notifier against the container -----------------------------------------
print("7. notifier")
shutil.rmtree(MAIL_DIR, ignore_errors=True)
os.makedirs(os.path.join(MAIL_DIR, "out"))
config = os.path.join(MAIL_DIR, "notify.config.json")
try:
    with open(config, "w") as fh:
        json.dump({"api_url": BASE + "/api/activity", "api_key": KEY, "site_url": BASE,
                   "smtp_user": "e2e-sender@example.invalid", "smtp_app_password": "not-a-password",
                   "mail_to": "admin@example.invalid", "redirect_all_to": REDIRECT,
                   "deliver": "dir:" + os.path.join(MAIL_DIR, "out"), "quiet_minutes": 0,
                   "state_path": os.path.join(MAIL_DIR, ".notify-state-v2.json")}, fh)
    run = subprocess.run(["/usr/bin/python3", os.path.join(REPO, "deploy", "notifier", "gokapi_notify.py"),
                          "--once", "--from-start", "--config", config],
                         stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, timeout=120)
    print("    notifier output:\n      " + "\n      ".join(run.stdout.strip().splitlines()[-12:]))
    check("gokapi_notify.py --once --from-start exits 0", run.returncode == 0, str(run.returncode))
    mails = []
    for path in sorted(glob.glob(os.path.join(MAIL_DIR, "out", "*"))):
        with open(path, "rb") as fh:
            mails.append((os.path.basename(path), email.message_from_bytes(fh.read(), policy=email.policy.default)))
    # A subject longer than a line is folded right after "Subject:"; unfolding
    # leaves the leading space, which mail clients drop, so strip it too
    subjects = [str(m["Subject"]).strip() for _, m in mails]
    print("    %d emails: %s" % (len(mails), subjects))
    check("at least one .eml was written", mails and all(n.endswith(".eml") for n, _ in mails), str([n for n, _ in mails]))
    # immediate kinds of one poll share one email, joined with "; " (README)
    check("an email for the group A created: '… New group: %s by Anna Muster'" % GNAME,
          any(s.startswith("[FGS] ") and ("New group: %s by Anna Muster" % GNAME) in s.split("[FGS] ", 1)[1].split("; ")
              for s in subjects))
    parts = [x for s in subjects if s.startswith("[FGS] ") for x in s[6:].split("; ")]
    check("View as… start and stop: 'View as…: tester viewing as Anna Muster' (… stopped: exit)",
          "View as…: tester viewing as Anna Muster" in parts and
          "View as…: tester viewing as Anna Muster stopped: exit" in parts, str(parts)[:160])
    summaries = [s for s in subjects if s.startswith("[FGS] Activity: ")]
    check("exactly one summary '[FGS] Activity: …'", len(summaries) == 1, str(summaries))
    bodies = [m.get_body(("plain",)).get_content() for _, m in mails if str(m["Subject"]).strip().startswith("[FGS] Activity: ")]
    check("  the summary has a section for the group, and a Blocked sign-ins section",
          len(bodies) == 1 and GNAME in bodies[0] and "Blocked sign-ins" in bodies[0])
    check("every To is the redirect, no Cc or Bcc",
          mails and all(str(m["To"]) == REDIRECT and m["Cc"] is None and m["Bcc"] is None for _, m in mails),
          str(sorted({str(m["To"]) for _, m in mails})))
finally:
    shutil.rmtree(MAIL_DIR, ignore_errors=True)
check("the mail directory is removed", not os.path.exists(MAIL_DIR))

print()
print("%d passed, %d failed" % (counts["pass"], counts["fail"]))
print("ALL PASS" if ok else "SOME FAILED")
sys.exit(0 if ok else 1)
