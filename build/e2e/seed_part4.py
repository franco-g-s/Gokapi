#!/usr/bin/env python3
"""Shared fixture for Part 4 end-to-end tests.

Runs seed_part3's fixture (users A and B, group G, 230 + 20 PNGs, a
150-file collection) with one change: A and B get email addresses as their
account names instead of p3a_/p3b_ names, because Part 4 keys on "@" (the
Upload page switch, the no-email note, blocking a domain). Then:

  - A and B get display names "Anna Muster" and "Ben Keller" (/settings/save);
  - A gets a 64x64 JPEG photo (PHOTO_JPEG, below) through POST /seed/photo,
    which exists only when the container runs with GOKAPI_E2E_SEED=1;
  - a third user C (an email, in no group) and a second admin (an email, so
    "You cannot block an admin." can be tested: the setup admin "tester" is
    not email-shaped);
  - an admin API key with ManageLogs, made with /api/auth/create and granted
    with /api/auth/modify.

Progress goes to stderr; stdout gets exactly one JSON line:
  {"A":{id,name,password},"B":{...},"C":{...},"admin2":{id,name},
   "group":id,"groupName":..,"collection":id,"ids":{"A":[..],"B":[..]},
   "apikey":<admin key with ManageLogs>,"bDomain":"@..."}
Exit status 1 on any failure.
"""
import base64, contextlib, importlib.util, io, json, os, sys

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("seed_part3", os.path.join(HERE, "seed_part3.py"))
seed = importlib.util.module_from_spec(spec)
spec.loader.exec_module(seed)
Client, log, RUN = seed.Client, seed.log, seed.RUN

EMAILS = {
    "a": "anna.%s@example.com" % RUN,
    "b": "ben.%s@keller-%s.example" % (RUN, RUN),
    "c": "carla.%s@example.org" % RUN,
}
ADMIN2 = "boss.%s@admin-%s.example" % (RUN, RUN)
B_DOMAIN = "@keller-%s.example" % RUN
# A 64x64 baseline JPEG (a blue gradient, made with macOS sips); inline
# because build/e2e/fixtures/ is git-ignored
PHOTO_JPEG = base64.b64decode(
    "/9j/4AAQSkZJRgABAQAASABIAAD/4QBMRXhpZgAATU0AKgAAAAgAAYdpAAQAAAABAAAAGgAAAAAA"
    "A6ABAAMAAAABAAEAAKACAAQAAAABAAAAQKADAAQAAAABAAAAQAAAAAD/7QA4UGhvdG9zaG9wIDMu"
    "MAA4QklNBAQAAAAAAAA4QklNBCUAAAAAABDUHYzZjwCyBOmACZjs+EJ+/8AAEQgAQABAAwEiAAIR"
    "AQMRAf/EAB8AAAEFAQEBAQEBAAAAAAAAAAABAgMEBQYHCAkKC//EALUQAAIBAwMCBAMFBQQEAAAB"
    "fQECAwAEEQUSITFBBhNRYQcicRQygZGhCCNCscEVUtHwJDNicoIJChYXGBkaJSYnKCkqNDU2Nzg5"
    "OkNERUZHSElKU1RVVldYWVpjZGVmZ2hpanN0dXZ3eHl6g4SFhoeIiYqSk5SVlpeYmZqio6Slpqeo"
    "qaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2drh4uPk5ebn6Onq8fLz9PX29/j5+v/EAB8BAAMB"
    "AQEBAQEBAQEAAAAAAAABAgMEBQYHCAkKC//EALURAAIBAgQEAwQHBQQEAAECdwABAgMRBAUhMQYS"
    "QVEHYXETIjKBCBRCkaGxwQkjM1LwFWJy0QoWJDThJfEXGBkaJicoKSo1Njc4OTpDREVGR0hJSlNU"
    "VVZXWFlaY2RlZmdoaWpzdHV2d3h5eoKDhIWGh4iJipKTlJWWl5iZmqKjpKWmp6ipqrKztLW2t7i5"
    "usLDxMXGx8jJytLT1NXW19jZ2uLj5OXm5+jp6vLz9PX29/j5+v/bAEMAAgICAgICAwICAwQDAwME"
    "BQQEBAQFBwUFBQUFBwgHBwcHBwcICAgICAgICAoKCgoKCgsLCwsLDQ0NDQ0NDQ0NDf/bAEMBAgIC"
    "AwMDBgMDBg0JBwkNDQ0NDQ0NDQ0NDQ0NDQ0NDQ0NDQ0NDQ0NDQ0NDQ0NDQ0NDQ0NDQ0NDQ0NDQ0N"
    "DQ0NDf/dAAQABP/aAAwDAQACEQMRAD8A+XIoq04oqSKKtSKKv6ZrVj8Xy2vtqEUVacUXSiKKtOKK"
    "vKrVj9Ay2vtqEUVacUVEUVacUVeVWrH6BltfbUWKKtOKKkiirUiirya1Y/QMtr7an//Q8BiirUii"
    "pIoq04oq/oStWP58y3ELQWKKtOKKkiirTiiryq1Y/QMtxC0FiirTiioiirTiirya1Y/QMtxC0CKK"
    "tOKKiKKtOKKvKrVj9Ay3ELQ//9HyeKKtOKKiKKtOKKv2+vWP5hy3EbBFFWnFFRFFWnFFXk1qx+gZ"
    "biNgiirUiipIoq04oq8qtWP0DLcRsLFFWnFFRFFWnFFXlVqx+gZbiNj/0uNiirTiipIoq04oq/W6"
    "1Y/kfLa70FiirTiipIoq1Ioq8itWP0DLa70CKKtOKKiKKtOKKvKrVj9Ay2u9AiirTiioiirTiiry"
    "a1Y/QMtrvQ//2Q=="
)
CLIENTS = {}


def create_user(admin, name):
    s, b, _ = admin.req("POST", "/api/user/create", b"",
                        {"apikey": admin.token("PERM_MANAGE_USERS"), "username": name})
    if s != 200:
        raise RuntimeError("user/create %s -> %s %s" % (name, s, b))
    return json.loads(b)["id"]


def make_user(admin, label):
    """seed_part3.make_user, with an email as the account name"""
    name = EMAILS[label]
    uid = create_user(admin, name)
    s, b, _ = admin.req("POST", "/api/user/resetPassword", b"",
                        {"apikey": admin.token("PERM_MANAGE_USERS"), "userid": str(uid),
                         "generateNewPassword": "true"})
    one_time = json.loads(b)["password"]
    c = Client(name)
    c.id = uid
    c.login(name, one_time)
    _, page = c.text("/changePassword")      # first login forces a new password
    c.post_form("/changePassword", {"newpw": seed.NEW_PW, "csrf-token": c.csrf(page)})
    if c.text("/groups")[0] != 200:
        raise RuntimeError("user %s cannot reach /groups" % name)
    CLIENTS[label] = c
    return c


def admin_key(admin, manage_logs=True, friendly="part4 e2e"):
    """A new API key of the admin's; with ManageLogs only when asked"""
    mod = admin.token("PERM_API_MOD")
    s, b, _ = admin.req("POST", "/api/auth/create", b"",
                        {"apikey": mod, "friendlyName": friendly, "basicPermissions": "false"})
    if s != 200:
        raise RuntimeError("auth/create -> %s %s" % (s, b[:200]))
    key = json.loads(b)["Id"]
    if manage_logs:
        s, b, _ = admin.req("POST", "/api/auth/modify", b"",
                            {"apikey": mod, "targetKey": key, "permission": "PERM_MANAGE_LOGS",
                             "permissionModifier": "GRANT"})
        if s != 200:
            raise RuntimeError("auth/modify -> %s %s" % (s, b[:200]))
    return key


def main():
    seed.make_user = make_user
    out = io.StringIO()
    with contextlib.redirect_stdout(out):
        seed.main()
    fixture = json.loads(out.getvalue().strip().splitlines()[-1])
    A, B = CLIENTS["a"], CLIENTS["b"]

    admin = Client("admin")
    if admin.login(seed.ADMIN, seed.ADMIN_PW) not in (302, 303, 307):
        raise RuntimeError("admin login failed")
    log("display names")
    for client, name in ((A, "Anna Muster"), (B, "Ben Keller")):
        s, b, _ = client.post_json("/settings/save", {"displayName": name})
        if s != 200 or json.loads(b).get("Name") != name:
            raise RuntimeError("settings/save %s -> %s %s" % (name, s, b[:200]))
    log("photo for A")
    jpeg = base64.b64encode(PHOTO_JPEG).decode()
    s, b, _ = admin.post_json("/seed/photo", {"userId": A.id, "jpegBase64": jpeg})
    if s != 200 or not json.loads(b).get("PhotoUrl"):
        raise RuntimeError("seed/photo -> %s %s (is GOKAPI_E2E_SEED=1 set?)" % (s, b[:200]))
    log("user C and a second admin")
    C = make_user(admin, "c")
    admin2 = create_user(admin, ADMIN2)
    s, b, _ = admin.req("POST", "/api/user/changeRank", b"",
                        {"apikey": admin.token("PERM_MANAGE_USERS"), "userid": str(admin2), "newRank": "admin"})
    if s != 200:
        raise RuntimeError("user/changeRank -> %s %s" % (s, b[:200]))
    log("admin API key with ManageLogs")
    key = admin_key(admin)
    fixture.update({
        "A": {"id": A.id, "name": A.name, "password": seed.NEW_PW},
        "B": {"id": B.id, "name": B.name, "password": seed.NEW_PW},
        "C": {"id": C.id, "name": C.name, "password": seed.NEW_PW},
        "admin2": {"id": admin2, "name": ADMIN2},
        "groupName": "P3 seed " + RUN,
        "apikey": key,
        "bDomain": B_DOMAIN,
        "run": RUN,
    })
    print(json.dumps(fixture), flush=True)


if __name__ == "__main__":
    try:
        main()
    except Exception as e:
        log("FAILED: %s: %s" % (type(e).__name__, e))
        sys.exit(1)
