"""按 VI 第 06 板生成 App 图标与启动图（ADR 0010）。

- icon.png：App Store 1024px 直角版（圆角由系统添加）
- android-icon-foreground / background / monochrome：安卓自适应图标，前景在圆形安全区内
- splash-icon.png：原生启动图上的标志；完整的竖版组合（琥珀「考研」+ 白色 Training）由 App 内 0.1 启动页用真实字体绘制
- favicon.png：网页调试用

拿到 Logo 的 SVG 源文件（open-questions Q07）后，替换 MARK 并重新执行：pip install cairosvg && python scripts/gen-icons.py
"""
from pathlib import Path

import cairosvg

OUT = Path(__file__).resolve().parent.parent / "apps/mobile/assets"

# 标志构成（VI 第 02 板，100u 网格）：轨道环、70% 琥珀进度弧、85% 处白色目标点、字母 T
MARK = """
  <circle cx="50" cy="50" r="30" fill="none" stroke="{track}" stroke-width="9"/>
  <path d="M50 20 A30 30 0 1 1 21.47 59.27" fill="none" stroke="{arc}" stroke-width="9" stroke-linecap="round"/>
  <circle cx="25.73" cy="32.37" r="5.5" fill="{dot}"/>
  <rect x="38" y="38" width="24" height="6.5" rx="2" fill="{t}"/>
  <rect x="46.75" y="38" width="6.5" height="24" rx="2" fill="{t}"/>
"""
INDIGO, TRACK, AMBER, WHITE = "#231F55", "#3B377A", "#FFB547", "#FFFFFF"


def svg(view_box: str, body: str, bg: str | None = None) -> str:
    rect = f'<rect x="-100" y="-100" width="400" height="400" fill="{bg}"/>' if bg else ""
    return f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="{view_box}">{rect}{body}</svg>'


def render(name: str, content: str, size: int) -> None:
    cairosvg.svg2png(bytestring=content.encode(), write_to=str(OUT / name), output_width=size, output_height=size)
    print(name)


def main() -> None:
    color = MARK.format(track=TRACK, arc=AMBER, dot=WHITE, t=WHITE)
    mono = MARK.format(track="#FFFFFF66", arc=WHITE, dot=WHITE, t=WHITE)
    # 标志直径 69u；安卓前景画布取 119u，让标志落在 66/108 的圆形安全区内
    safe = "-9.5 -9.5 119 119"
    render("icon.png", svg("0 0 100 100", color, INDIGO), 1024)
    render("android-icon-foreground.png", svg(safe, color), 1024)
    render("android-icon-background.png", svg("0 0 100 100", "", INDIGO), 1024)
    render("android-icon-monochrome.png", svg(safe, mono), 1024)
    rounded = f'<rect width="100" height="100" rx="22" fill="{INDIGO}"/>' + color
    render("splash-icon.png", svg("0 0 100 100", rounded), 512)
    render("favicon.png", svg("0 0 100 100", rounded), 64)


if __name__ == "__main__":
    main()
