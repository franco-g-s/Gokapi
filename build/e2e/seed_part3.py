#!/usr/bin/env python3
"""Shared fixture for Part 3 end-to-end tests.

Against the local Gokapi fork on 127.0.0.1:53900 (see setup_local.py): the admin
makes two users A and B (per-run suffix, so it can run again on the same
container); A creates a group, B joins by code; A uploads 230 and B 20 generated
8x8 flat-colour PNGs (p3-<nnn>-<word>.png) into it; A makes a 150-file
collection from their uploads.

Progress goes to stderr; stdout gets exactly one JSON line:
  {"A":{id,name,password},"B":{...},"group":id,"collection":id,
   "ids":{"A":[230 file ids],"B":[20 file ids]}}
Exit status 1 on any failure.
"""
import base64, http.cookiejar, json, re, struct, sys, time, uuid, zlib
import urllib.error, urllib.parse, urllib.request
from concurrent.futures import ThreadPoolExecutor

BASE = "http://127.0.0.1:53900"
ADMIN, ADMIN_PW = "tester", "testpassword123"
RUN = uuid.uuid4().hex[:6]
NEW_PW = "E2e-Passw0rd-" + RUN
N_A, N_B, N_COLLECTION, WORKERS = 230, 20, 150, 6
WORDS = ["alpine", "harbour", "meadow", "summit"]


def log(*a):
    print(*a, file=sys.stderr, flush=True)


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

    def post_form(self, path, fields, extra=None):
        headers = {"Content-Type": "application/x-www-form-urlencoded"}
        headers.update(extra or {})
        return self.req("POST", path, urllib.parse.urlencode(fields).encode(), headers)

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
        headers = {"apikey": self.upload_key(), "uuid": upload_id,
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

    def upload_key(self):
        if not getattr(self, "_upkey", None):
            self._upkey = self.token("PERM_UPLOAD")
        return self._upkey

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


def retry(fn, tries=15, delay=0.5):
    """Poll fn() until it returns something truthy; return the last value."""
    value = None
    for _ in range(tries):
        try:
            value = fn()
        except (urllib.error.URLError, OSError, ValueError):
            value = None
        if value:
            return value
        time.sleep(delay)
    return value


def png(index):
    """An 8x8 flat-colour PNG; the colour is unique for every index."""
    rgb = bytes([(index * 37 + 11) % 256, (index * 91 + 53) % 256, (index * 5 + 7) % 256])
    raw = b"".join(b"\x00" + rgb * 8 for _ in range(8))

    def chunk(kind, data):
        body = kind + data
        return struct.pack(">I", len(data)) + body + struct.pack(">I", zlib.crc32(body) & 0xffffffff)
    return (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", 8, 8, 8, 2, 0, 0, 0))
            + chunk(b"IDAT", zlib.compress(raw)) + chunk(b"IEND", b""))


def make_user(admin, label):
    name = ("p3%s_%s" % (label, RUN)).lower()
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
    if c.text("/groups")[0] != 200:
        raise RuntimeError("user %s cannot reach /groups" % name)
    return c


def upload_all(client, group, start, count):
    """Upload `count` PNGs (indices start..start+count-1); return file ids in index order."""
    def one(i):
        name = "p3-%03d-%s.png" % (i, WORDS[i % len(WORDS)])
        content = png(i)

        def attempt():
            s, r = client.upload(name, content, group)
            fid = (r.get("FileInfo") or {}).get("Id", "") if s == 200 else ""
            return fid
        fid = retry(attempt, tries=6, delay=1)
        if not fid:
            raise RuntimeError("upload of %s failed" % name)
        return fid
    client.upload_key()
    with ThreadPoolExecutor(WORKERS) as pool:
        return list(pool.map(one, range(start, start + count)))


def main():
    assert len({tuple(zlib.decompress(png(i)[41:-12])[1:4]) for i in range(N_A + N_B)}) == N_A + N_B, "colours not distinct"
    admin = Client("admin")
    if admin.login(ADMIN, ADMIN_PW) not in (302, 303, 307):
        raise RuntimeError("admin login failed")
    log("users")
    A, B = make_user(admin, "a"), make_user(admin, "b")
    log("group")
    s, b, _ = A.post_json("/groups/create", {"name": "P3 seed " + RUN, "days": 60})
    created = json.loads(b)
    if s != 200 or created.get("Result") != "OK":
        raise RuntimeError("groups/create -> %s %s" % (s, b[:200]))
    gid = created["Id"]
    s, b, _ = B.post_json("/groups/join", {"code": created["Code"]})
    if s != 200 or json.loads(b).get("Result") != "OK":
        raise RuntimeError("groups/join -> %s %s" % (s, b[:200]))
    log("uploading %d for A" % N_A)
    ids_a = upload_all(A, gid, 0, N_A)
    log("uploading %d for B" % N_B)
    ids_b = upload_all(B, gid, N_A, N_B)
    log("collection")
    s, b, _ = A.post_json("/collections/create", {"name": "P3 collection " + RUN, "fileIds": ids_a[:N_COLLECTION]})
    col = json.loads(b)
    if s != 200 or col.get("Result") != "OK" or col.get("Files") != N_COLLECTION:
        raise RuntimeError("collections/create -> %s %s" % (s, b[:200]))
    print(json.dumps({"A": {"id": A.id, "name": A.name, "password": NEW_PW},
                      "B": {"id": B.id, "name": B.name, "password": NEW_PW},
                      "group": gid, "collection": col["Id"],
                      "ids": {"A": ids_a, "B": ids_b}}), flush=True)


if __name__ == "__main__":
    try:
        main()
    except Exception as e:
        log("FAILED: %s: %s" % (type(e).__name__, e))
        sys.exit(1)
