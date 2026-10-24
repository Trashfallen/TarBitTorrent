"""Рисует значок программы: tarbit/ui/icon-*.png и assets/icon.ico.

Запуск: .venv\\Scripts\\python tools\\make_icon.py
"""

from pathlib import Path

from PIL import Image, ImageDraw, ImageFilter

ROOT = Path(__file__).resolve().parent.parent
N = 1024
C1, C2 = (0, 214, 255), (138, 63, 252)   # бирюзовый -> фиолетовый


def gradient(n):
    g = Image.new("RGB", (n, n))
    px = g.load()
    for y in range(n):
        for x in range(n):
            t = (x + y) / (2 * n - 2)
            px[x, y] = tuple(int(C1[i] + (C2[i] - C1[i]) * t) for i in range(3))
    return g


def draw():
    small = 256
    bg = gradient(small).resize((N, N), Image.BICUBIC)
    mask = Image.new("L", (N, N), 0)
    ImageDraw.Draw(mask).rounded_rectangle((40, 40, N - 40, N - 40), radius=230, fill=255)
    img = Image.new("RGBA", (N, N), (0, 0, 0, 0))
    img.paste(bg, (0, 0), mask)

    # мягкая тень под знаком
    glyph = Image.new("L", (N, N), 0)
    d = ImageDraw.Draw(glyph)
    # перекладина T
    d.rounded_rectangle((250, 250, 774, 372), radius=60, fill=255)
    # ствол T - стрелка вниз
    d.rounded_rectangle((452, 330, 572, 640), radius=40, fill=255)
    d.polygon([(330, 600), (694, 600), (512, 800)], fill=255)
    shadow = glyph.filter(ImageFilter.GaussianBlur(24))
    dark = Image.new("RGBA", (N, N), (20, 10, 60, 110))
    img.paste(dark, (0, 18), shadow)
    white = Image.new("RGBA", (N, N), (255, 255, 255, 255))
    img.paste(white, (0, 0), glyph)
    # три «бита» справа от стрелки
    for i, (x, y) in enumerate([(700, 520), (760, 610), (700, 700)]):
        r = 34 - i * 4
        ImageDraw.Draw(img).ellipse((x - r, y - r, x + r, y + r), fill=(255, 255, 255, 200))
    return img


def main():
    img = draw()
    ui = ROOT / "tarbit" / "ui"
    for s in (16, 32, 64, 128, 256, 512):
        img.resize((s, s), Image.LANCZOS).save(ui / f"icon-{s}.png")
    (ROOT / "assets").mkdir(exist_ok=True)
    img.resize((256, 256), Image.LANCZOS).save(
        ROOT / "assets" / "icon.ico", sizes=[(16, 16), (24, 24), (32, 32), (48, 48), (64, 64), (128, 128), (256, 256)])
    print("ok")


if __name__ == "__main__":
    main()
