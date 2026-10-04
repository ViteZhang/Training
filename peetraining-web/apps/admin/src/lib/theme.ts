import { colors } from '@training/ui-tokens';
import type { ThemeConfig } from 'antd';

/** 后台用 Ant Design 默认规范，只把主色换成夜靛、字体用系统字体（dev-spec 第十一节）。 */
export const theme: ThemeConfig = {
  token: {
    colorPrimary: colors.indigo,
    colorLink: colors.blue,
    colorSuccess: colors.green,
    colorError: colors.red,
    colorWarning: colors.amber,
    colorText: colors.ink,
    colorTextSecondary: colors.gray,
    colorBorder: colors.line,
    colorBgLayout: colors.paper,
    fontFamily: '-apple-system, BlinkMacSystemFont, "PingFang SC", "Microsoft YaHei", "Noto Sans SC", sans-serif',
  },
};
