#!/usr/bin/env python3
"""生成壁纸贴图（不透明彩色 PNG，tile 无缝，128x128）。

为什么不再用 CSS background-blend-mode：
  运行时把「灰度图案 x 壁纸色」相乘交给浏览器，一旦 blend 未生效，
  近白的图案就会盖住颜色，墙看起来是白的。这里在出图时就把颜色乘进去，
  前端只需要 background-image，任何内核/任何加载时序都不会掉色。

加一款新壁纸：在 WALLPAPERS 里加一行（颜色需与 src/scene-art.js 的 wallColor 一致），
再跑一次本脚本即可。

用法：python3 frontend/tools/gen_wallpaper.py
"""
import os
import random
from PIL import Image

N = 128
OUT_DIR = os.path.join(os.path.dirname(__file__), '..', 'public', 'art')

# key -> (颜色, 花纹绘制函数)。颜色与 scene-art.js 的 wallColor 保持一致。
WALLPAPERS = {
    'wp-pink': ((237, 204, 208), 'floral'),
    'wp-blue': ((198, 216, 222), 'stripe_star'),
    'wp-mint': ((197, 215, 181), 'gingham'),
}


def new_mask(base=252, grain=3, seed=11):
    """灰度明暗遮罩：252=原色，数值越小压得越暗。"""
    r = random.Random(seed)
    im = Image.new('L', (N, N))
    p = im.load()
    for y in range(N):
        for x in range(N):
            p[x, y] = max(228, min(255, base + r.randint(-grain, grain)))
    return im, p


def put(p, x, y, v):
    p[x % N, y % N] = v


def draw_floral(p):
    """五瓣小花 + 花苞点缀。"""
    petal = [(0,-3),(0,-2),(0,2),(0,3),(-3,0),(-2,0),(2,0),(3,0),
             (-2,-2),(2,-2),(-2,2),(2,2),(-1,-1),(1,-1),(-1,1),(1,1)]
    core = [(-1,0),(1,0),(0,-1),(0,1),(0,0)]
    for cx, cy in [(32, 32), (96, 96)]:
        for dx, dy in petal:
            put(p, cx + dx, cy + dy, 198)
        for dx, dy in core:
            put(p, cx + dx, cy + dy, 172)
    for cx, cy in [(0, 96), (96, 0)]:
        for dx, dy in [(0,-1),(0,1),(-1,0),(1,0),(0,0)]:
            put(p, cx + dx, cy + dy, 216)


def draw_stripe_star(p):
    """细竖条纹 + 小星点。"""
    for x in range(N):
        if x % 16 < 7:
            for y in range(N):
                put(p, x, y, 218)
    for gy in range(16, N, 64):
        for gx in range(16, N, 64):
            for dx, dy in [(0,-2),(0,-1),(0,1),(0,2),(-2,0),(-1,0),(1,0),(2,0),(0,0)]:
                put(p, gx + dx, gy + dy, 186)


def draw_gingham(p):
    """格子布。"""
    for x in range(N):
        if x % 16 < 6:
            for y in range(N):
                put(p, x, y, 232)
    for y in range(N):
        if y % 16 < 6:
            for x in range(N):
                put(p, x, y, min(p[x, y], 222))
    for x in range(N):
        if x % 16 < 6:
            for y in range(N):
                if y % 16 < 6:
                    put(p, x, y, 206)


DRAW = {'floral': draw_floral, 'stripe_star': draw_stripe_star, 'gingham': draw_gingham}


def build(color, style):
    mask, p = new_mask()
    DRAW[style](p)
    out = Image.new('RGB', (N, N))
    op = out.load()
    cr, cg, cb = color
    for y in range(N):
        for x in range(N):
            m = mask.getpixel((x, y)) / 255
            op[x, y] = (int(cr * m), int(cg * m), int(cb * m))
    return out


if __name__ == '__main__':
    os.makedirs(OUT_DIR, exist_ok=True)
    for key, (color, style) in WALLPAPERS.items():
        path = os.path.join(OUT_DIR, key + '.png')
        build(color, style).save(path)
        print('生成', key, color, style, '->', os.path.relpath(path))
