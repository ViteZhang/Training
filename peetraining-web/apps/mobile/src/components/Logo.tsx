import { colors, logo } from '@training/ui-tokens';
import Svg, { Circle, Path, Rect } from 'react-native-svg';

/**
 * 标志（VI 第 02 板）：夜靛底板、轨道环、70% 琥珀进度弧、85% 处白色目标点、字母 T。
 * 70% 与 85% 是固定形态，不随真实分数变化。拿到 Logo 源文件后（open-questions Q07）核对路径。
 */
export function Logo({ size = 64, plate = true }: { size?: number; plate?: boolean }) {
  return (
    <Svg width={size} height={size} viewBox="0 0 100 100" accessibilityLabel="考研Training">
      {plate ? <Rect width={100} height={100} rx={logo.cornerRadius} fill={colors.indigo} /> : null}
      <Circle cx={50} cy={50} r={logo.ringRadius} fill="none" stroke={colors.track} strokeWidth={logo.ringWidth} />
      <Path d="M50 20 A30 30 0 1 1 21.47 59.27" fill="none" stroke={colors.amber} strokeWidth={logo.ringWidth} strokeLinecap="round" />
      <Circle cx={25.73} cy={32.37} r={logo.targetDiameter / 2} fill={colors.white} />
      <Rect x={38} y={38} width={logo.tBar.width} height={logo.tBar.height} rx={logo.tRadius} fill={colors.white} />
      <Rect x={46.75} y={38} width={logo.tStem.width} height={logo.tStem.height} rx={logo.tRadius} fill={colors.white} />
    </Svg>
  );
}
