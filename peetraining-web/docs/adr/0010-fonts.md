# 0010 字体：正文用系统字体，标题打包思源宋体子集，数字打包 Sora

- 日期：2026-10-01
- 状态：已采纳

## 背景

VI v1.0 规定：标题思源宋体 Heavy / Bold，界面思源黑体，分数与英文 Sora。完整中文字体每个字重十几 MB，全部打包会让安装包明显变大；产品不依赖 Google 服务，不能在运行时从 Google Fonts 加载。

## 决定

- 正文和界面用系统中文字体：iOS 是苹方，多数安卓机自带思源黑体（与 Noto Sans SC 同一套字形），不打包
- 标题用 Noto Serif SC，只打包 GB2312 一级汉字（3755 字）+ ASCII + 常用中文标点的子集，Heavy（900）与 Bold（700）两个字重，每个约 1.6 MB；由 scripts/subset-fonts.py 生成
- 分数、倒计时、英文品牌名用 Sora SemiBold / Bold，整套打包（约 58 KB）
- 字体文件来自 npm 包 @expo-google-fonts/noto-serif-sc 与 @expo-google-fonts/sora（SIL OFL，可商用），放在 apps/mobile/assets/fonts，构建时打包，启动时用 expo-font 的 useFonts 加载，加载完成前保持原生启动图
- 字体名统一用 ui-tokens 的 fontFamily（NotoSerifSC-Heavy、Sora-Bold 等），用 useFonts 的别名注册，iOS 与安卓一致
- 管理后台只用系统字体

## 放弃的方案与原因

- 运行时从 Google Fonts 加载：国内不可用，也违反「不依赖 Google 服务」
- 按 3500 字《现代汉语常用字表》裁剪：仓库里没有可靠的字表来源；GB2312 一级字基本覆盖且可由编码表直接生成
- 用 expo-font 配置插件嵌入原生：iOS 按字体内部 PostScript 名称识别，与文件名不一致，容易出错

## 影响

- 标题里出现子集外的生僻字时，会回退到系统字体显示，不会出现方块
- 更换字体或扩充字集时重新执行脚本并提交生成的 ttf
