"""把标题字体 Noto Serif SC 裁剪为常用字子集（ADR 0010）。

完整的中文字体每个字重十几 MB，打包会让安装包明显变大；标题只用常用字，
取 GB2312 一级汉字（3755 字，覆盖《现代汉语常用字表》的绝大部分）+ ASCII + 常用中文标点。

用法：pip install fonttools && python scripts/subset-fonts.py <NotoSerifSC_900Black.ttf> <NotoSerifSC_700Bold.ttf>
源字体来自 npm 包 @expo-google-fonts/noto-serif-sc（SIL OFL，可商用）。
"""
import sys
from pathlib import Path

from fontTools import subset

OUT = Path(__file__).resolve().parent.parent / "apps/mobile/assets/fonts"


def common_chars() -> str:
    chars = []
    # GB2312 一级汉字：区 16–55，位 1–94（55 区只到 89 位）
    for zone in range(16, 56):
        for pos in range(1, 95):
            if zone == 55 and pos > 89:
                break
            try:
                chars.append(bytes([zone + 0xA0, pos + 0xA0]).decode("gb2312"))
            except UnicodeDecodeError:
                pass
    ascii_chars = "".join(chr(c) for c in range(0x20, 0x7F))
    punct = "，。、；：？！「」『』“”‘’（）《》〈〉【】…—～·￥％＋－×÷＝０１２３４５６７８９"
    return "".join(chars) + ascii_chars + punct


def main(heavy: str, bold: str) -> None:
    text = common_chars()
    OUT.mkdir(parents=True, exist_ok=True)
    for src, name in ((heavy, "NotoSerifSC-Heavy.ttf"), (bold, "NotoSerifSC-Bold.ttf")):
        opts = subset.Options()
        opts.name_IDs = ["*"]
        opts.layout_features = ["*"]
        font = subset.load_font(src, opts)
        sub = subset.Subsetter(opts)
        sub.populate(text=text)
        sub.subset(font)
        out = OUT / name
        font.save(str(out))
        print(f"{out.name}: {out.stat().st_size // 1024} KB，{len(set(text))} 个字符")


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
