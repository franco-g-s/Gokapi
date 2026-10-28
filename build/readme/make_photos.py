#!/usr/bin/env python3
"""Synthetic landscape "photos" for the README screenshots.

Every image is drawn here from a seed: a sky gradient with a sun, layered
ridges that fade into haze, and sometimes a lake that mirrors the sky. They
read as photos in a grid but contain nothing real, so the screenshots can
be public. Standard library only.

    python3 build/readme/make_photos.py OUT_DIR

Writes PNGs named after their scene, plus avatar-*.png (square gradients
for the seeded profile photos).
"""
import math, os, random, struct, sys, zlib

# (name, sky top, sky horizon, sun, ridge near, lake?)
SCENES = [
    ("Piz Bernina at dawn",      (40, 52, 99),   (247, 170, 120), (255, 222, 170), (28, 30, 46),  True),
    ("Lej da Silvaplana",        (70, 120, 190), (200, 222, 240), (255, 250, 230), (34, 58, 52),  True),
    ("Morteratsch glacier",      (96, 140, 200), (220, 232, 245), (255, 255, 240), (60, 70, 86),  False),
    ("Last light, Muottas",      (60, 40, 90),   (250, 140, 90),  (255, 200, 120), (40, 24, 40),  False),
    ("Fog over the valley",      (150, 160, 175), (225, 225, 228), (250, 250, 245), (90, 96, 104), False),
    ("Swim at Lej Nair",         (80, 150, 210), (190, 225, 245), (255, 255, 235), (30, 70, 50),  True),
    ("Blue hour, Maloja",        (20, 30, 70),   (90, 110, 170),  (200, 210, 255), (14, 18, 34),  True),
    ("Larch forest in October",  (110, 150, 200), (235, 215, 170), (255, 240, 200), (120, 80, 30), False),
    ("Summit, 3,056 m",          (30, 80, 170),  (170, 205, 240), (255, 255, 250), (70, 74, 82),  False),
    ("Sunset from the hut",      (90, 50, 110),  (255, 160, 80),  (255, 220, 140), (50, 30, 46),  True),
    ("Morning swim",             (120, 170, 220), (240, 220, 200), (255, 245, 220), (40, 64, 70), True),
    ("Storm coming in",          (60, 66, 80),   (150, 150, 155), (210, 210, 200), (36, 40, 46),  False),
    ("Alpine meadow",            (90, 150, 215), (210, 235, 250), (255, 255, 240), (60, 110, 50), False),
    ("Moonrise",                 (12, 16, 40),   (60, 70, 120),   (235, 235, 255), (10, 12, 24),  True),
    ("Ridge walk",               (70, 120, 200), (230, 210, 190), (255, 240, 210), (66, 60, 70),  False),
    ("Golden hour on the lake",  (100, 80, 120), (255, 190, 110), (255, 230, 160), (60, 40, 50),  True),
    ("Snowfield",                (110, 160, 220), (235, 242, 250), (255, 255, 255), (170, 180, 200), False),
    ("Cloud inversion",          (60, 110, 190), (240, 240, 245), (255, 255, 245), (50, 54, 70),  False),
    ("Evening, Sils",            (70, 60, 120),  (240, 150, 130), (255, 210, 170), (34, 30, 50),  True),
    ("First snow",               (140, 160, 190), (230, 232, 240), (250, 250, 255), (110, 116, 130), False),
    ("Pass road",                (80, 130, 200), (220, 225, 230), (255, 250, 235), (72, 68, 64),  False),
    ("Reflections",              (50, 90, 160),  (200, 210, 230), (255, 245, 225), (24, 40, 44),  True),
]

AVATARS = [((236, 120, 90), (120, 60, 140)), ((80, 160, 200), (40, 70, 140)),
           ((120, 190, 120), (40, 110, 90)), ((230, 180, 80), (190, 90, 60))]


def write_png(path, w, h, rows):
    raw = b"".join(b"\x00" + bytes(r) for r in rows)

    def chunk(kind, data):
        c = kind + data
        return struct.pack(">I", len(data)) + c + struct.pack(">I", zlib.crc32(c) & 0xFFFFFFFF)

    with open(path, "wb") as f:
        f.write(b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", w, h, 8, 2, 0, 0, 0))
                + chunk(b"IDAT", zlib.compress(raw, 6)) + chunk(b"IEND", b""))


def mix(a, b, t):
    return tuple(a[i] + (b[i] - a[i]) * t for i in range(3))


def ridge(w, rnd, base, amp):
    waves = [(rnd.uniform(0.6, 1.6) * amp / (k + 1), rnd.uniform(1.5, 3.5) * (k + 1), rnd.uniform(0, 6.3))
             for k in range(5)]
    return [base - sum(a * math.sin(f * x / w * math.pi + p) for a, f, p in waves) - rnd.uniform(0, amp * 0.06)
            for x in range(w)]


def scene(w, h, seed, top, horizon, sun, near, lake):
    rnd = random.Random(seed)
    water = int(h * rnd.uniform(0.70, 0.78)) if lake else h
    sun_x, sun_y, sun_r = rnd.uniform(0.2, 0.8) * w, rnd.uniform(0.25, 0.5) * h, h * rnd.uniform(0.03, 0.05)
    layers = []
    for k in range(4):                       # far to near
        t = (k + 1) / 4
        base = h * (0.42 + 0.09 * k) if not lake else h * (0.40 + 0.08 * k)
        layers.append((ridge(w, rnd, base, h * (0.16 - 0.025 * k)), mix(horizon, near, 0.35 + 0.65 * t)))
    rows = []
    for y in range(h):
        row = bytearray(w * 3)
        mirror = y >= water
        sy = 2 * water - y if mirror else y
        sky = mix(top, horizon, min(1.0, sy / (h * 0.62)))
        for x in range(w):
            c = sky
            d = math.hypot(x - sun_x, sy - sun_y)
            if d < sun_r:
                c = sun
            else:
                glow = math.exp(-(d - sun_r) / (h * 0.16)) * 0.55
                c = mix(c, sun, glow)
            for line, colour in layers:
                if sy >= line[x]:
                    c = colour
            if mirror:
                ripple = 0.5 + 0.5 * math.sin(y * 0.9 + math.sin(x * 0.02) * 2)
                c = mix(c, (c[0] * 0.55, c[1] * 0.6, c[2] * 0.7), 0.35 + 0.15 * ripple)
            o = x * 3
            row[o], row[o + 1], row[o + 2] = (max(0, min(255, int(v))) for v in c)
        rows.append(row)
    return rows


def avatar(size, a, b):
    rows = []
    for y in range(size):
        row = bytearray()
        for x in range(size):
            c = mix(a, b, (x + y) / (2 * size))
            row += bytes(int(v) for v in c)
        rows.append(row)
    return rows


def main():
    out = sys.argv[1]
    os.makedirs(out, exist_ok=True)
    for i, (name, top, horizon, sun, near, lake) in enumerate(SCENES):
        w, h = (900, 1200) if i % 5 == 3 else (1200, 800)
        write_png(os.path.join(out, name + ".png"), w, h, scene(w, h, i, top, horizon, sun, near, lake))
        print(name, file=sys.stderr)
    for i, (a, b) in enumerate(AVATARS):
        write_png(os.path.join(out, "avatar-%d.png" % i), 256, 256, avatar(256, a, b))


if __name__ == "__main__":
    main()
