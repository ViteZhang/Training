import { colors } from '@training/ui-tokens';
import type { ThemeConfig } from 'antd';

/** 后台用 Ant Design 默认规范，颜色按设计稿（a7_*：墨黑主色、白底），字体用系统字体（dev-spec 第十一节）。 */
export const theme: ThemeConfig = {
  token: {
    colorPrimary: colors.ink,
    colorLink: colors.link,
    colorSuccess: colors.green,
    colorError: colors.red,
    colorWarning: colors.orange,
    colorText: colors.ink,
    colorTextSecondary: colors.gray,
    colorBorder: colors.border,
    colorSplit: colors.line,
    colorBgLayout: colors.fill,
    fontFamily: '-apple-system, BlinkMacSystemFont, "PingFang SC", "Microsoft YaHei", "Noto Sans SC", sans-serif',
  },
};
