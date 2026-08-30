#!/usr/bin/env python3
"""Render `quotamon --demo --color=always` to docs/console.png.

Deterministic, sample-data only (no live account). Parses the ANSI SGR codes
quotamon emits (reset/33/31) into coloured spans and draws them with a
monospace font on a dark terminal-like background. Regenerate via
scripts/screenshots.sh.
"""
import re, subprocess, sys
from pathlib import Path
from PIL import Image, ImageDraw, ImageFont

ROOT = Path(__file__).resolve().parent.parent
BIN = ROOT / "core" / "bin" / "quotamon"
OUT = ROOT / "docs" / "console.png"

BG = (30, 30, 36)
FG = (222, 222, 226)
COLORS = {"0": FG, "33": (222, 180, 60), "31": (232, 90, 82)}  # reset / warning / critical

def cells(line):
    """Yield (text, color) spans from a line containing SGR codes 0/33/31."""
    out, color, i = [], FG, 0
    for m in re.finditer(r"\x1b\[([0-9;]*)m", line):
        if m.start() > i:
            out.append((line[i:m.start()], color))
        code = m.group(1).split(";")[-1] or "0"
        color = COLORS.get(code, FG)
        i = m.end()
    if i < len(line):
        out.append((line[i:], color))
    return out

def font():
    for p in ("/System/Library/Fonts/SFNSMono.ttf",
              "/System/Library/Fonts/Menlo.ttc",
              "/Library/Fonts/SF-Mono-Regular.otf"):
        if Path(p).exists():
            try: return ImageFont.truetype(p, 26)
            except Exception: pass
    return ImageFont.load_default()

def main():
    if not BIN.exists():
        sys.exit(f"build the core first: (cd core && make build) — missing {BIN}")
    raw = subprocess.run([str(BIN), "--demo", "--color=always"],
                         capture_output=True, text=True, check=True).stdout
    lines = raw.rstrip("\n").split("\n")
    f = font()
    asc, desc = f.getmetrics()
    lh = asc + desc + 8
    cw = f.getbbox("M")[2]
    pad = 28
    width = pad * 2 + cw * (max((len(re.sub(r"\x1b\[[0-9;]*m", "", l)) for l in lines), default=40) + 1)
    height = pad * 2 + lh * len(lines)
    img = Image.new("RGB", (width, height), BG)
    d = ImageDraw.Draw(img)
    y = pad
    for line in lines:
        x = pad
        for text, color in cells(line):
            d.text((x, y), text, font=f, fill=color)
            x += cw * len(text)
        y += lh
    img.save(OUT)
    print(f"wrote {OUT} ({width}x{height})")

if __name__ == "__main__":
    main()
