/**
 * 视觉 token。页面颜色一比一取自设计稿（docs/design/）；VI（docs/design/vi/）只管 Logo、启动页、App 图标与字体。
 * App 与管理后台共用；颜色、字号、字体只从这里取，不在页面里写死。
 */

/**
 * 页面颜色，一比一取自设计稿（docs/design/spec.dc.html「01 颜色」与 pages/*.dc.html）：黑白灰为主，分类色点缀。
 * VI 的夜靛、琥珀、轨道紫、纸白只用于 Logo、启动页和 App 图标，见下方 brand，页面里不用。
 */
export const colors = {
  /** 墨黑：标题、正文、主按钮、选中态 */
  ink: '#1A1A1A',
  /** 深灰：「学习中」标签文字、次级强调 */
  inkSoft: '#4A4A4A',
  /** 次级灰：说明文字 */
  gray: '#6E6E6E',
  /** 弱化灰：未选中导航、占位 */
  muted: '#767676',
  /** AI 生成中的第二个点 */
  dim: '#8A8A8A',
  /** 禁用按钮文字 */
  disabled: '#9E9E9E',
  /** 加载中按钮文字、浅图标 */
  faint: '#BDBDBD',
  /** 占位块、图表空格 */
  placeholder: '#CFCFCF',
  /** 「未学习」描边、细描边胶囊 */
  outline: '#D6D6D6',
  /** 描边灰：卡片与输入框描边 */
  border: '#E6E6E6',
  /** 分割线、进度轨道、禁用按钮底 */
  line: '#EFEFEF',
  /** 「学习中」标签底 */
  chip: '#F2F2F0',
  /** 浅底：次级背景、提醒卡、次级胶囊按钮 */
  fill: '#F7F7F5',
  /** 纯白：页面背景、卡片 */
  white: '#FFFFFF',

  // 功能分类色（每种颜色固定对应一类功能；只用于图标底块、色点、标签和数据块）
  /** 复习 · 天蓝（到期复习、记忆类） */
  blue: '#6FA8DC',
  blueSoft: '#E5F0FA',
  blueInk: '#1D4C77',
  /** 复习天蓝的描边与次级文字（1.7 缺答案说明卡） */
  blueLine: '#A9C7E4',
  blueMuted: '#3A6A96',
  /** 查漏 · 蜜桃（薄弱、错题、提醒；也用于预估分、进度、提分） */
  peach: '#F2956B',
  peachSoft: '#FDEDE4',
  peachInk: '#8A3A16',
  /** 背诵 · 薰衣草 */
  lavender: '#8C80E0',
  lavenderSoft: '#EEEBFB',
  lavenderInk: '#3E3190',
  /** 真题 · 薄荷 */
  mint: '#6CC08E',
  mintSoft: '#E4F4EB',
  mintInk: '#1F5C3E',
  /** 作文 · 奶油 */
  cream: '#E3C25A',
  creamSoft: '#FBF3D9',
  creamInk: '#6B5207',

  // 状态色（只用于批改结果与系统反馈，不与分类色混用）
  /** 正确、命中、已掌握数据 */
  green: '#1F7A4D',
  greenSoft: '#E4F4EB',
  /** 部分命中、注意 */
  orange: '#B26A00',
  orangeSoft: '#FDF1DC',
  orangeInk: '#6B4A12',
  orangeLine: '#F3DDB3',
  /** 错误、遗漏、删除 */
  red: '#C23B22',
  redSoft: '#FCE9E5',
  /** 危险按钮描边 */
  redLine: '#F3C9C0',
  /** 信息、链接 */
  link: '#2F5FB3',
} as const;

/** VI 品牌色（VI 第 04 板）：只用于 Logo、启动页、App 图标，页面不用。 */
export const brand = {
  indigo: '#231F55',
  amber: '#FFB547',
  track: '#3B377A',
  paper: '#FAF8F3',
} as const;

/** 语义色：组件按用途取色，换肤只改这里。 */
export const semantic = {
  /** 页面底（设计稿为纯白） */
  background: colors.white,
  surface: colors.white,
  textPrimary: colors.ink,
  textSecondary: colors.gray,
  textTertiary: colors.muted,
  textOnBrand: colors.white,
  /** 卡片描边（设计稿页面里卡片都是 #EFEFEF） */
  border: colors.line,
  /** 输入框、次按钮描边 */
  inputBorder: colors.border,
  /** 列表分割线 */
  divider: colors.line,
  /** 主按钮、选中态 */
  primary: colors.ink,
  /** 预估分、进度、提分 */
  progress: colors.peach,
  progressTrack: colors.line,
  /** 深色（墨黑）底上的进度轨道 */
  progressTrackOnBrand: '#3C3A36',
  mastered: colors.green,
  info: colors.link,
  danger: colors.red,
  warning: colors.orange,
  /** 选中的标签、品牌提示条底（设计稿用浅底） */
  primarySoft: colors.fill,
  peachSoft: colors.peachSoft,
  dangerSoft: colors.redSoft,
  infoSoft: colors.blueSoft,
  masteredSoft: colors.greenSoft,
  warningSoft: colors.orangeSoft,
  /** 卡片内的浅底块、次级胶囊按钮 */
  fill: colors.fill,
  /** 不可用按钮 */
  disabledBg: colors.line,
  disabledText: colors.disabled,
  /** 弹层遮罩 */
  scrim: 'rgba(26,26,26,0.45)',
  /** Toast 底 */
  toast: 'rgba(26,26,26,0.88)',
} as const;

/** 设计稿颜色 → token。页面颜色与设计稿一致，这张表只用于读 .dc.html 时查 token 名。 */
export const designColorMap: Record<string, string> = Object.fromEntries(
  Object.entries(colors).map(([name, hex]) => [hex, name]),
);

/** 字体族，与设计稿一致（spec.dc.html「02 字体」：中文 Noto Sans SC、数字 DM Sans）。中文用系统字体不打包；DM Sans 构建时打包（ADR 0010）。 */
export const fontFamily = {
  /** 数字、分数、倒计时、英文（DM Sans）。安卓上自定义字体不认 fontWeight，按字重分别取名 */
  numberRegular: 'DMSans-Regular',
  numberMedium: 'DMSans-Medium',
  numberSemiBold: 'DMSans-SemiBold',
  numberBold: 'DMSans-Bold',
  /** 正文：系统字体（iOS 苹方、安卓思源黑体，与 Noto Sans SC 同字形），不指定字体名 */
  body: undefined as string | undefined,
} as const;

/** 数字字体按字重取对应的 DM Sans 文件（400 / 500 / 600 / 700）。 */
export function numberFont(weight: number | string | undefined): string {
  const w = weight === 'bold' ? 700 : Number(weight) || 400;
  if (w >= 700) return fontFamily.numberBold;
  if (w >= 600) return fontFamily.numberSemiBold;
  if (w >= 500) return fontFamily.numberMedium;
  return fontFamily.numberRegular;
}

/**
 * 字号层级（App），按 docs/design 设计稿（页面标题 26、卡片标题 17、正文 15、辅助 13、注释 12、分数 40）。
 * 界面文字用系统黑体，数字用 DM Sans。lineHeight 单位与 fontSize 相同。
 */
export const typography = {
  /** 大标题（备用，设计稿未用到更大的标题） */
  display: { fontSize: 32, lineHeight: 40, fontWeight: '700' as const },
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
  score: { fontSize: 40, lineHeight: 46, fontFamily: fontFamily.numberSemiBold, color: colors.ink, letterSpacing: -1.5 },
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

export type BrandColor = keyof typeof brand;
export type ColorName = keyof typeof colors;
export type SemanticColor = keyof typeof semantic;
