import { radius, semantic } from '@training/ui-tokens';
import { StyleSheet, View } from 'react-native';

export interface ProgressBarProps {
  /** 0–1 */
  value: number;
  /** 深色底（夜靛卡片）上用轨道紫做轨道 */
  onBrand?: boolean;
  height?: number;
  /** 目标点位置 0–1（预估分卡上的目标分），不传不显示 */
  target?: number;
  /** 填充色，默认琥珀（进度、提分）；解析进度等中性进度用墨色 */
  color?: string;
}

/** 进度条：琥珀只用于进度与提分（VI）。 */
export function ProgressBar({ value, onBrand, height = 6, target, color }: ProgressBarProps) {
  const pct = Math.max(0, Math.min(1, value));
  return (
    <View
      accessible
      accessibilityRole="progressbar"
      accessibilityValue={{ min: 0, max: 100, now: Math.round(pct * 100) }}
      style={[styles.track, { height, backgroundColor: onBrand ? semantic.progressTrackOnBrand : semantic.progressTrack }]}
    >
      <View style={[styles.fill, { width: `${pct * 100}%` }, color ? { backgroundColor: color } : null]} />
      {target !== undefined ? (
        <View
          style={[
            styles.target,
            { left: `${Math.max(0, Math.min(1, target)) * 100}%`, width: height + 4, height: height + 4, marginLeft: -(height + 4) / 2, top: -2 },
            { backgroundColor: onBrand ? semantic.surface : semantic.primary },
          ]}
        />
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  track: { width: '100%', borderRadius: radius.pill, overflow: 'visible' },
  fill: { height: '100%', borderRadius: radius.pill, backgroundColor: semantic.progress },
  target: { position: 'absolute', borderRadius: radius.pill },
});
