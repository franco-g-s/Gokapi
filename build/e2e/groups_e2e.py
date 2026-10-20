#!/usr/bin/env python3
"""End-to-end test of Groups (Part 2) against a local Gokapi fork.

Runs against 127.0.0.1:53900, set up by setup_local.py (internal auth, admin
"tester"). The admin creates four ordinary users through the API (A, B, C, D;
names carry a per-run suffix, so the script can run again on the same
container). Then:

  A creates a group (60 days) and uploads two files into it
  B joins by link, C joins by code
  duplicates are refused (blocking: 409) or silently dropped (non-blocking: 200)
  B's downloads tick the tiles; the zip is byte-equal to the originals
  D, who is in no group, sees the empty Groups page and cannot reach the group

Every request is made with urllib and one cookie jar per person. Redirects are
never followed, so 303s can be asserted.
"""
import base64, hashlib, http.cookiejar, io, json, os, re, sys, time, uuid
import urllib.error, urllib.parse, urllib.request, zipfile

BASE = "http://127.0.0.1:53900"
ADMIN, ADMIN_PW = "tester", "testpassword123"
RUN = uuid.uuid4().hex[:6]
NEW_PW = "E2e-Passw0rd-" + RUN

ok = True


def check(label, cond, detail=""):
    global ok
    if not cond:
        ok = False
    print("  %-62s %s %s" % (label, "PASS" if cond else "FAIL", detail))


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *a, **k):
        return None


class Client:
    """One person: a cookie jar, and requests that never follow redirects."""

    def __init__(self, name):
        self.name = name
        self.opener = urllib.request.build_opener(
            urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()), NoRedirect)
        self.id = None

    def req(self, method, path, data=None, headers=None):
        r = urllib.request.Request(BASE + path, data=data, method=method, headers=headers or {})
        try:
            x = self.opener.open(r)
            return x.status, x.read(), dict(x.headers)
        except urllib.error.HTTPError as e:
            return e.code, e.read(), dict(e.headers)

    def get(self, path):
        return self.req("GET", path)

    def post_json(self, path, obj):
        return self.req("POST", path, json.dumps(obj).encode(), {"Content-Type": "application/json"})

    def post_form(self, path, fields):
        return self.req("POST", path, urllib.parse.urlencode(fields).encode(),
                        {"Content-Type": "application/x-www-form-urlencoded"})

    def text(self, path):
        s, b, _ = self.get(path)
        return s, b.decode("utf-8", "replace")

    def json(self, path):
        s, b, _ = self.get(path)
        return s, json.loads(b) if b else None

    def csrf(self, page):
        m = re.search(r'name="csrf-token"[^>]*value="([^"]+)"', page) or \
            re.search(r'value="([^"]+)"[^>]*name="csrf-token"', page)
        return m.group(1) if m else None

    def login(self, user, pw):
        _, page = self.text("/login")
        fields = {"username": user, "password": pw}
        if self.csrf(page):
            fields["csrf-token"] = self.csrf(page)
        return self.post_form("/login", fields)[0]       # 307 to /admin on success

    def token(self, perm):
        s, b, _ = self.req("POST", "/auth/token", b"", {"permission": perm, "Content-Type": "application/json"})
        if s != 200:
            raise RuntimeError("auth/token %s -> %s %s" % (perm, s, b[:80]))
        return json.loads(b)["key"]

    # ---- uploading, the way the browser does it -----------------------------
    def send_chunk(self, content, upload_id):
        boundary = uuid.uuid4().hex
        parts = b""
        for k, v in (("dztotalfilesize", str(len(content))), ("dzchunkbyteoffset", "0"), ("dzuuid", upload_id)):
            parts += ('--%s\r\nContent-Disposition: form-data; name="%s"\r\n\r\n%s\r\n' % (boundary, k, v)).encode()
        parts += ('--%s\r\nContent-Disposition: form-data; name="file"; filename="blob"\r\n'
                  'Content-Type: application/octet-stream\r\n\r\n' % boundary).encode() + content + \
                 ("\r\n--%s--\r\n" % boundary).encode()
        return self.req("POST", "/uploadChunk", parts,
                        {"Content-Type": "multipart/form-data; boundary=" + boundary})

    def upload(self, name, content, group_id="", nonblocking=False):
        """uploadChunk then chunk/complete (with groupId). Returns (status, parsed body)."""
        upload_id = uuid.uuid4().hex
        s, b, _ = self.send_chunk(content, upload_id)
        if s != 200:
            return s, {"chunk": b.decode("utf-8", "replace")}
        headers = {"apikey": self.token("PERM_UPLOAD"), "uuid": upload_id,
                   "filename": "base64:" + base64.b64encode(name.encode()).decode(),
                   "filesize": str(len(content)), "realsize": str(len(content)),
                   "contenttype": "application/octet-stream", "isE2E": "false",
                   "nonblocking": "true" if nonblocking else "false"}
        if group_id:
            headers["groupId"] = group_id
        else:       # a private upload needs its own terms
            headers.update({"allowedDownloads": "0", "expiryDays": "5"})
        s, b, _ = self.req("POST", "/api/chunk/complete", b"", headers)
        try:
            return s, json.loads(b)
        except ValueError:
            return s, {"raw": b.decode("utf-8", "replace")}

    def own_files(self):
        s, b, _ = self.req("GET", "/api/files/list", headers={"apikey": self.token("PERM_VIEW")})
        return json.loads(b) or []


def retry(fn, tries=15, delay=0.5):
    """Poll fn() until it returns something truthy; return the last value."""
    value = None
    for _ in range(tries):
        value = fn()
        if value:
            return value
        time.sleep(delay)
    return value


def tiles(page):
    """{file id: downloaded?} for the group page's board tiles."""
    result = {}
    for tag in re.findall(r'<figure class="fgs-tile"[^>]*>', page):
        fid = re.search(r'data-id="([^"]*)"', tag)
        dl = re.search(r'data-downloaded="([^"]*)"', tag)
        if fid and fid.group(1):        # the template tile has an empty id
            result[fid.group(1)] = (dl.group(1) == "true") if dl else None
    return result


# ---- 0. the admin makes four ordinary users ---------------------------------
print("0. users")
admin = Client("admin")
check("admin logs in", admin.login(ADMIN, ADMIN_PW) in (302, 303, 307))


def make_user(label):
    name = ("g11%s_%s" % (label, RUN)).lower()   # Gokapi lowercases names
    mgmt = {"apikey": admin.token("PERM_MANAGE_USERS")}
    s, b, _ = admin.req("POST", "/api/user/create", b"", dict(mgmt, username=name))
    if s != 200:
        raise RuntimeError("user/create %s -> %s %s" % (name, s, b))
    uid = json.loads(b)["id"]
    s, b, _ = admin.req("POST", "/api/user/resetPassword", b"",
                        {"apikey": admin.token("PERM_MANAGE_USERS"), "userid": str(uid),
                         "generateNewPassword": "true"})
    one_time = json.loads(b)["password"]
    c = Client(name)
    c.id = uid
    c.login(name, one_time)
    _, page = c.text("/changePassword")      # first login forces a new password
    c.post_form("/changePassword", {"newpw": NEW_PW, "csrf-token": c.csrf(page)})
    return c


A, B, C, D = (make_user(x) for x in "ABCD")
for c in (A, B, C, D):
    s, _ = c.text("/groups")
    check("user %s signed in and reaches /groups" % c.name, s == 200)

# ---- 1. A creates a group ---------------------------------------------------
print("1. A creates a group with 60 days")
for path in ("/groups/create", "/groups/join", "/groups/delete"):
    check("GET %s is refused (405)" % path, A.get(path)[0] == 405)
s, b, _ = A.post_json("/groups/create", {"name": "Iceland " + RUN, "days": 60})
created = json.loads(b)
check("create returns OK", s == 200 and created.get("Result") == "OK", str(created)[:110])
gid, code, invite = created["Id"], created["Code"], created["InviteUrl"]
token = urllib.parse.parse_qs(urllib.parse.urlparse(invite).query).get("t", [""])[0]
check("create gives a code and an invite link with a token", bool(code) and bool(token), "%s %s" % (code, invite))
s, mine = A.json("/groups/list")
row = next((g for g in mine if g["id"] == gid), None)
check("A's group list has it, A is the creator", bool(row) and row["role"] == "creator")
closes_in_days = (row["expires"] - time.time()) / 86400 if row else 0
check("it closes in about 60 days", 59.5 < closes_in_days < 60.5, "%.2f days" % closes_in_days)
group_expires = row["expires"] if row else 0

# ---- 2. B joins by link -----------------------------------------------------
print("2. B joins by link")
s, page = B.text("/join?t=" + urllib.parse.quote(token))
check("GET /join?t= shows the confirmation page", s == 200 and "Iceland " + RUN in page and 'name="t"' in page)
check("a GET joined nothing", not any(g["id"] == gid for g in B.json("/groups/list")[1]))
s, b, h = B.post_form("/groups/join", {"t": token})
check("POST /groups/join (form) is a 303 to g?id=", s == 303 and ("g?id=" + gid) in h.get("Location", ""),
      "%s %s" % (s, h.get("Location")))
check("B is now a member", any(g["id"] == gid and g["role"] == "member" for g in B.json("/groups/list")[1]))
s, _, h = B.post_form("/groups/join", {"t": "not-a-real-token-" + RUN})
check("a bad invite token does not join (303 back to /join)", s == 303 and "join?t=" in h.get("Location", "") and "g?id=" not in h.get("Location", ""),
      h.get("Location", ""))

# ---- 3. C joins by code -----------------------------------------------------
print("3. C joins by code")
s, b, _ = C.post_json("/groups/join", {"code": code})
joined = json.loads(b)
check("POST /groups/join {code} returns OK", s == 200 and joined.get("Result") == "OK" and joined.get("Id") == gid, str(joined)[:90])
s, listing = C.json("/groups/list")
check("C is a member; the group has 3 people", any(g["id"] == gid and g["members"] == 3 for g in listing),
      str([(g["role"], g["members"]) for g in listing]))

# ---- 4. A uploads two files into the group ----------------------------------
print("4. A uploads two files through uploadChunk + chunk/complete with groupId")
body1 = ("alpha-" + RUN + "-").encode() * 400
body2 = os.urandom(6000)
s1, r1 = A.upload("alpha-%s.txt" % RUN, body1, gid)
s2, r2 = A.upload("beta-%s.bin" % RUN, body2, gid)
check("both uploads answer 200", s1 == 200 and s2 == 200, "%s %s" % (s1, s2))
f1 = (r1.get("FileInfo") or {}).get("Id", "")
f2 = (r2.get("FileInfo") or {}).get("Id", "")
check("both answers carry a file id", bool(f1 and f2), "%s %s" % (f1, f2))


def item_of(client, fid):
    s, j = client.json("/groups/item?id=%s&file=%s" % (gid, fid))
    return j if s == 200 else None


item1 = retry(lambda: item_of(B, f1))
item2 = retry(lambda: item_of(B, f2))
check("/groups/item answers 200 for both (B, a member)", bool(item1 and item2))
if item1:
    check("the file follows the group's closing time", abs(item1["ExpireAt"] - group_expires) <= 1,
          "%s vs %s" % (item1["ExpireAt"], group_expires))
    check("unlimited downloads, no password", item1["UnlimitedDownloads"] and not item1["IsPasswordProtected"])
    check("the item names A as uploader", item1["UploaderName"] == A.name, item1.get("UploaderName", ""))
check("B has not downloaded them (not ticked)", item1 is not None and item1["Downloaded"] is False)
# the tick is a Downloads row only; "own uploads count as had" (D12) is the filters' rule
a_item = item_of(A, f1)
check("A's own upload is not ticked (a tick is a download)", a_item is not None and a_item["Downloaded"] is False)
a_tiles = re.findall(r'<figure class="fgs-tile"[^>]*data-mine="(\w+)"[^>]*data-id="(\w+)"', A.text("/g?id=" + gid)[1])
check("A's page marks both tiles data-mine=\"true\" (D12 input)", sorted(a_tiles) == sorted([("true", f1), ("true", f2)]), str(a_tiles))
s, page_b = B.text("/g?id=" + gid)
check("B's group page shows exactly the two tiles", s == 200 and set(tiles(page_b)) == {f1, f2}, str(tiles(page_b)))

# ---- 5. B asks whether the bytes are already there --------------------------
print("5. /groups/has")
sha1 = hashlib.sha1(body1).hexdigest()
s, has = B.json("/groups/has?id=%s&sha1=%s" % (gid, sha1))
check("A's bytes: checkable, exists, uploader A, file id", s == 200 and has.get("checkable") and has.get("exists")
      and has.get("uploader") == A.name and has.get("fileId") == f1, str(has))
s, has = B.json("/groups/has?id=%s&sha1=%s" % (gid, hashlib.sha1(b"never uploaded " + RUN.encode()).hexdigest()))
check("unknown bytes: exists is false", s == 200 and has.get("exists") is False, str(has))
check("a malformed sha1 is a 400", B.get("/groups/has?id=%s&sha1=zz" % gid)[0] == 400)

# ---- 6. B uploads the same bytes anyway -------------------------------------
print("6. duplicates")
s, r = B.upload("my-copy-of-alpha.txt", body1, gid)
check("blocking duplicate answers 409 'Already in the group'",
      s == 409 and r.get("ErrorMessage") == "Already in the group", "%s %s" % (s, str(r)[:100]))
s, listing = A.json("/groups/list")
check("the group still has two items", next(g["files"] for g in listing if g["id"] == gid) == 2)
check("B has no leftover copy", not any(f["Name"] == "my-copy-of-alpha.txt" for f in B.own_files()))

s, r = B.upload("nonblocking-copy-of-beta.bin", body2, gid, nonblocking=True)
check("non-blocking duplicate answers 200", s == 200, "%s %s" % (s, str(r)[:80]))
time.sleep(2)
check("the server deleted the non-blocking copy", not any(f["Name"] == "nonblocking-copy-of-beta.bin" for f in B.own_files()))
s, listing = A.json("/groups/list")
check("the group still has two items", next(g["files"] for g in listing if g["id"] == gid) == 2)

# ---- 7. ticks ---------------------------------------------------------------
print("7. ticks")
s, item2 = B.json("/groups/item?id=%s&file=%s" % (gid, f2))
check("before any download B's item is not ticked; looking did not tick it",
      s == 200 and item2["Downloaded"] is False)
check("non-member D cannot download from the group (/gd 404)", D.get("/gd?id=%s&file=%s" % (gid, f1))[0] == 404)
s, content, h = B.get("/gd?id=%s&file=%s" % (gid, f1))
check("B downloads one through /gd, bytes equal", s == 200 and content == body1, "%s %d bytes" % (s, len(content)))


def ticks():
    return tiles(B.text("/g?id=" + gid)[1])


t = retry(lambda: ticks().get(f1) is True and ticks())
t = ticks()
check("B's /g shows data-downloaded=\"true\" on the downloaded tile", t.get(f1) is True, str(t))
check("and \"false\" on the other", t.get(f2) is False, str(t))
check("C's page is untouched by B's download", tiles(C.text("/g?id=" + gid)[1]).get(f1) is False)
check("A's tiles are not ticked by B's downloads", set(tiles(A.text("/g?id=" + gid)[1]).values()) == {False})

# ---- 8. zip -----------------------------------------------------------------
print("8. zip")
s, data, h = B.get("/gz?id=%s&files=%s,%s" % (gid, f1, f2))
check("/gz answers 200 as a zip", s == 200 and "zip" in h.get("Content-Type", "").lower(), "%s %s" % (s, h.get("Content-Type")))
try:
    z = zipfile.ZipFile(io.BytesIO(data))
    entries = {n: z.read(n) for n in z.namelist()}
except zipfile.BadZipFile as e:
    entries = {}
    print("   bad zip:", e)
check("the zip has two entries", len(entries) == 2, str(sorted(entries)))
check("byte-equal to the originals", sorted(entries.values()) == sorted([body1, body2]))
check("entries carry the original names", {"alpha-%s.txt" % RUN, "beta-%s.bin" % RUN} == set(entries), str(sorted(entries)))
t = retry(lambda: ticks().get(f2) is True and ticks())
t = ticks()
check("the zip ticked both tiles for B", t.get(f1) is True and t.get(f2) is True, str(t))
check("non-member D cannot download the zip (/gz 404)", D.get("/gz?id=%s&files=%s,%s" % (gid, f1, f2))[0] == 404)

# ---- 8b. a non-blocking upload of new bytes shows up live --------------------
print("8b. live add of a non-blocking upload")
body3 = os.urandom(3000)
name3 = "gamma-%s.bin" % RUN
s, r = B.upload(name3, body3, gid, nonblocking=True)
check("non-blocking upload of new bytes answers 200", s == 200)
fid3 = retry(lambda: next((f["Id"] for f in B.own_files() if f["Name"] == name3), None), tries=20)
check("the file lands in B's list", bool(fid3))
live = retry(lambda: item_of(A, fid3)) if fid3 else None       # 404 until the attach lands
check("/groups/item answers 200 once the attach lands", bool(live))
check("group tiles now: three", len(tiles(A.text("/g?id=" + gid)[1])) == 3)

# ---- 9. D has no groups -----------------------------------------------------
print("9. D, in no group")
s, page = D.text("/groups")
check("/groups renders for D", s == 200 and "Groups" in page)
check("empty state: no groups table, the Create and Join actions are there",
      "groupstable" not in page and "Create group" in page and "Join with a code" in page)
check("D's /groups/list is empty", D.json("/groups/list")[1] == [])
check("D gets 404 on the group page", D.get("/g?id=" + gid)[0] == 404)
check("D gets 404 on /groups/item", D.get("/groups/item?id=%s&file=%s" % (gid, f1))[0] == 404)
check("D gets 404 on /groups/has", D.get("/groups/has?id=%s&sha1=%s" % (gid, sha1))[0] == 404)
s, _, _ = D.post_json("/groups/join", {"code": "ZZZZZZ"})
check("D joining with a wrong code is refused", s in (400, 404), str(s))
check("a member (B) cannot delete the group (403)", B.post_json("/groups/delete", {"id": gid})[0] == 403)
check("the group is untouched", any(g["id"] == gid for g in A.json("/groups/list")[1]))

print("\n" + ("ALL CHECKS PASSED" if ok else "SOME CHECKS FAILED"))
sys.exit(0 if ok else 1)
