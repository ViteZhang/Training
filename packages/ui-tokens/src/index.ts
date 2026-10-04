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

/** 字号层级（App，VI 第 05 板）。lineHeight 单位与 fontSize 相同。 */
export const typography = {
  h1: { fontSize: 32, lineHeight: 40, fontFamily: fontFamily.serifHeavy },
  h2: { fontSize: 22, lineHeight: 30, fontWeight: '700' as const },
  h3: { fontSize: 18, lineHeight: 26, fontWeight: '700' as const },
  body: { fontSize: 16, lineHeight: 24 },
  bodyStrong: { fontSize: 16, lineHeight: 24, fontWeight: '600' as const },
  caption: { fontSize: 13, lineHeight: 18, color: colors.gray },
  small: { fontSize: 11, lineHeight: 16, color: colors.gray },
  score: { fontSize: 48, lineHeight: 52, fontFamily: fontFamily.numberBold, color: colors.indigo },
  number: { fontSize: 20, lineHeight: 26, fontFamily: fontFamily.numberSemiBold },
} as const;

/** 间距（4 的倍数）。 */
export const spacing = { xs: 4, sm: 8, md: 12, lg: 16, xl: 20, xxl: 24, xxxl: 32 } as const;

/** 圆角（取自设计稿常用值）。 */
export const radius = { sm: 4, md: 12, lg: 16, xl: 18, pill: 999 } as const;

/** 基准尺寸与可点区域（dev-spec 第十一节）。 */
export const layout = {
  baseWidth: 390,
  baseHeight: 844,
  minTouch: 44,
  pagePadding: 20,
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
