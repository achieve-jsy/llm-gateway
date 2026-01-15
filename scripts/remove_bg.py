#!/usr/bin/env python3
"""
去除 web/public/images/mascot/ 下 PNG 图片的白色背景，生成带透明通道的 RGBA PNG。
仅需 Pillow（pip install Pillow），无需 numpy。
"""
import sys
import os
from pathlib import Path
from collections import deque


def remove_white_bg(img, threshold=235, flood_threshold=30):
    """
    使用 BFS flood-fill 从图片边缘开始，将白色/近白色背景设为透明。
    """
    img = img.convert("RGBA")
    w, h = img.size
    pixels = img.load()

    def is_near_white(r, g, b):
        return r >= threshold and g >= threshold and b >= threshold

    def color_diff(c1, c2):
        return sum(abs(int(a) - int(b)) for a, b in zip(c1[:3], c2[:3]))

    visited = [[False] * w for _ in range(h)]
    queue = deque()

    # 从四条边找白色像素作为起点
    for x in range(w):
        for y in [0, h - 1]:
            r, g, b, a = pixels[x, y]
            if is_near_white(r, g, b) and not visited[y][x]:
                queue.append((x, y))
                visited[y][x] = True
    for y in range(h):
        for x in [0, w - 1]:
            r, g, b, a = pixels[x, y]
            if is_near_white(r, g, b) and not visited[y][x]:
                queue.append((x, y))
                visited[y][x] = True

    # BFS 扩展
    while queue:
        cx, cy = queue.popleft()
        cr, cg, cb, _ = pixels[cx, cy]
        for dx, dy in [(-1, 0), (1, 0), (0, -1), (0, 1)]:
            nx, ny = cx + dx, cy + dy
            if 0 <= nx < w and 0 <= ny < h and not visited[ny][nx]:
                nr, ng, nb, _ = pixels[nx, ny]
                if is_near_white(nr, ng, nb) or color_diff((cr, cg, cb), (nr, ng, nb)) <= flood_threshold:
                    visited[ny][nx] = True
                    queue.append((nx, ny))

    # 将访问过的背景像素设为透明
    for y in range(h):
        for x in range(w):
            if visited[y][x]:
                r, g, b, _ = pixels[x, y]
                pixels[x, y] = (r, g, b, 0)

    return img


def main():
    try:
        from PIL import Image
    except ImportError:
        print("Pillow 未安装，请执行: pip install Pillow")
        sys.exit(1)

    mascot_dir = Path(__file__).parent.parent / "web/public/images/mascot"
    png_files = sorted(mascot_dir.glob("*.png"))

    if not png_files:
        print(f"未找到图片文件：{mascot_dir}")
        sys.exit(1)

    print(f"找到 {len(png_files)} 张图片，开始处理...")

    for png_path in png_files:
        print(f"  处理: {png_path.name} ...", end="", flush=True)
        try:
            img = Image.open(png_path)
            result = remove_white_bg(img, threshold=235, flood_threshold=30)
            result.save(png_path, "PNG", optimize=True)
            print(" ✓")
        except Exception as e:
            print(f" ✗ 失败: {e}")

    print("全部处理完毕！")


if __name__ == "__main__":
    main()
