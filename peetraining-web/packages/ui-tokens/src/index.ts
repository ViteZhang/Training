/**
 * 视觉 token，取自《考研Training 视觉识别规范 v1.0》（docs/design/vi/）。
 * App 与管理后台共用；颜色、字号、字体只从这里取，不在页面里写死。
 */

/** 颜色（VI 第 04 板）。用色比例：夜靛约 50%、纸白 30%、琥珀 12%、其他 8%。 */
export const colors = {
  /** 夜靛：主按钮、选中态、品牌强调 */
  indigo: '#231F55',
  /** 琥珀：只用于预估分、进度、提分 */
  amber: '#FFB547',
  /** 轨道紫：进度轨道、次要分隔（深色底上） */
  track: '#3B377A',
  white: '#FFFFFF',
  /** 纸白：页面底色（卡片仍为白） */
  paper: '#FAF8F3',
  /** 墨：正文 */
  ink: '#1B1A17',
  /** 灰：辅助文字 */
  gray: '#6B675F',
  /** 线：描边、分隔线 */
  line: '#E4DFD4',
  /** 已掌握 */
  green: '#2F9E6E',
  /** 提示、链接、到期复习 */
  blue: '#3E6FD8',
  /** 薄弱、错误、删除 */
  red: '#D6453D',
} as const;

/** 语义色：组件按用途取色，换肤只改这里。 */
export const semantic = {
  background: colors.paper,
  surface: colors.white,
  textPrimary: colors.ink,
  textSecondary: colors.gray,
  textOnBrand: colors.white,
  border: colors.line,
  primary: colors.indigo,
  progress: colors.amber,
  progressTrack: colors.line,
  progressTrackOnBrand: colors.track,
  mastered: colors.green,
  info: colors.blue,
  danger: colors.red,
  /** 弱化的品牌底色，用于选中的标签、提示条背景 */
  primarySoft: '#EDECF6',
  amberSoft: '#FFF3DE',
  dangerSoft: '#FBECEB',
  infoSoft: '#EAF0FB',
  masteredSoft: '#E8F5EF',
  /** 卡片内的浅底块、次级胶囊按钮（设计稿 #F7F7F5，在纸白底上加深一档） */
  fill: '#F3F0E8',
  /** 不可用按钮 */
  disabledBg: '#ECE9E2',
  disabledText: '#9E9A92',
} as const;

/** 设计稿旧色 → VI 色（dev-spec 第十一节），读 .dc.html 时对照替换。 */
export const legacyColorMap: Record<string, string> = {
  '#1A1A1A': colors.indigo, // 按钮、选中；正文改用 ink
  '#6E6E6E': colors.gray,
  '#EFEFEF': colors.line,
  '#F2956B': colors.amber,
  '#6FA8DC': colors.blue,
  '#C23B22': colors.red,
};

/** 字体族。正文用系统中文字体（不打包）；标题打包思源宋体子集；数字与英文打包 Sora（ADR 0010）。 */
export const fontFamily = {
  /** 标题：Noto Serif SC（思源宋体）常用字子集 */
  serifHeavy: 'NotoSerifSC-Heavy',
  serifBold: 'NotoSerifSC-Bold',
  /** 数字、分数、倒计时、英文品牌名 */
  numberSemiBold: 'Sora-SemiBold',
  numberBold: 'Sora-Bold',
  /** 正文：系统字体（iOS 苹方、安卓思源黑体），不指定字体名 */
  body: undefined as string | undefined,
} as const;

/**
 * 字号层级（App）。字号与字重按 docs/design/pages 设计稿的实际用法（页面标题 26、卡片标题 17、正文 15、辅助 13、注释 12），
 * 字体族与颜色按 VI：界面文字用系统黑体，数字用 Sora，思源宋体只用在品牌标题（display）。lineHeight 单位与 fontSize 相同。
 */
export const typography = {
  /** 品牌标题：启动页、报告页大标题（VI 第 05 板 H1） */
  display: { fontSize: 32, lineHeight: 40, fontFamily: fontFamily.serifHeavy },
  /** 页面大标题：「训练」「选择题目文件」 */
  h1: { fontSize: 26, lineHeight: 35, fontWeight: '700' as const },
  /** 弹层标题、结果页标题 */
  h2: { fontSize: 20, lineHeight: 28, fontWeight: '700' as const },
  /** 卡片标题、分组标题 */
  h3: { fontSize: 17, lineHeight: 24, fontWeight: '700' as const },
  body: { fontSize: 15, lineHeight: 22 },
  bodyStrong: { fontSize: 15, lineHeight: 22, fontWeight: '500' as const },
  caption: { fontSize: 13, lineHeight: 19, color: colors.gray },
  small: { fontSize: 12, lineHeight: 17, color: colors.gray },
  /** 倒计时、分数大数字 */
  score: { fontSize: 40, lineHeight: 46, fontFamily: fontFamily.numberSemiBold, color: colors.indigo, letterSpacing: -1 },
  number: { fontSize: 20, lineHeight: 26, fontFamily: fontFamily.numberSemiBold },
} as const;

/** 间距（4 的倍数）。 */
export const spacing = { xs: 4, sm: 8, md: 12, lg: 16, xl: 20, xxl: 24, xxxl: 32 } as const;

/** 圆角（取自设计稿常用值）。 */
export const radius = { sm: 4, md: 12, lg: 16, xl: 18, card: 22, pill: 999 } as const;

/** 基准尺寸与可点区域（dev-spec 第十一节）。 */
export const layout = {
  baseWidth: 390,
  baseHeight: 844,
  minTouch: 44,
  pagePadding: 22,
  /** 主按钮高度（设计稿 50，圆角为一半） */
  buttonHeight: 50,
  /** 系统字体放大到 1.3 倍不破版 */
  maxFontScale: 1.3,
} as const;

/** 时长（PRD 第 14 节）。 */
export const timing = {
  /** 超过 300 毫秒才显示骨架屏 */
  skeletonDelayMs: 300,
  /** Toast 2 秒消失 */
  toastMs: 2000,
  /** AI 生成超过 15 秒换安抚文案 */
  aiSlowMs: 15000,
  /** 作答草稿每 5 秒保存 */
  draftSaveMs: 5000,
} as const;

/** Logo 构成（VI 第 02 板），单位 u = 图标边长的 1/100。70% 与 85% 是固定形态，不随真实分数变化。 */
export const logo = {
  cornerRadius: 22,
  ringRadius: 30,
  ringWidth: 9,
  arcPercent: 0.7,
  targetPercent: 0.85,
  targetDiameter: 11,
  tBar: { width: 24, height: 6.5 },
  tStem: { width: 6.5, height: 24 },
  tRadius: 2,
} as const;

export type ColorName = keyof typeof colors;
export type SemanticColor = keyof typeof semantic;
