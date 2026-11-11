import { brand, colors, logo, semantic, spacing } from '@training/ui-tokens';
import { useEffect, useState } from 'react';
import { AccessibilityInfo, Animated, Easing, StyleSheet, View } from 'react-native';
import Svg, { Circle, Path, Rect } from 'react-native-svg';
import { Text } from './Text';

/** 等待超过这些时刻（毫秒）换一句安抚文案，让用户知道还在加载、不是白屏。 */
const HINTS: [number, string][] = [
  [0, '加载中…'],
  [4_000, '内容有点多，马上就好'],
  [12_000, '网络有点慢，再稍等一下'],
];

/**
 * 加载动画：用 Logo 做的「进度环」。轨道环、字母 T 不动，琥珀进度弧连同白色目标点绕轨道转，底板轻轻呼吸。
 * 品牌色只用在 Logo 本身（CLAUDE.md 视觉规则），文案颜色按页面 token。系统开了「减弱动态效果」时不转，只显示静态 Logo。
 */
export function LogoLoader({ size = 64, label = true }: { size?: number; label?: boolean }) {
  const [spin] = useState(() => new Animated.Value(0));
  const [breathe] = useState(() => new Animated.Value(0));
  const [hint, setHint] = useState('加载中…');

  useEffect(() => {
    let loops: Animated.CompositeAnimation[] = [];
    let alive = true;
    void AccessibilityInfo.isReduceMotionEnabled()
      .catch(() => false)
      .then((reduce) => {
        if (!alive || reduce) return;
        loops = [
          Animated.loop(Animated.timing(spin, { toValue: 1, duration: 1400, easing: Easing.inOut(Easing.cubic), useNativeDriver: true })),
          Animated.loop(
            Animated.sequence([
              Animated.timing(breathe, { toValue: 1, duration: 900, easing: Easing.inOut(Easing.quad), useNativeDriver: true }),
              Animated.timing(breathe, { toValue: 0, duration: 900, easing: Easing.inOut(Easing.quad), useNativeDriver: true }),
            ]),
          ),
        ];
        loops.forEach((l) => l.start());
      });
    return () => {
      alive = false;
      loops.forEach((l) => l.stop());
    };
  }, [spin, breathe]);

  useEffect(() => {
    if (!label) return;
    const timers = HINTS.slice(1).map(([ms, text]) => setTimeout(() => setHint(text), ms));
    return () => timers.forEach(clearTimeout);
  }, [label]);

  const rotate = spin.interpolate({ inputRange: [0, 1], outputRange: ['0deg', '360deg'] });
  const scale = breathe.interpolate({ inputRange: [0, 1], outputRange: [1, 1.05] });

  return (
    <View accessibilityLabel="加载中" accessibilityRole="progressbar" style={styles.wrap}>
      <Animated.View style={[{ width: size, height: size }, { transform: [{ scale }] }]}>
        <Svg width={size} height={size} viewBox="0 0 100 100">
          <Rect width={100} height={100} rx={logo.cornerRadius} fill={brand.indigo} />
          <Circle cx={50} cy={50} r={logo.ringRadius} fill="none" stroke={brand.track} strokeWidth={logo.ringWidth} />
          <Rect x={38} y={38} width={logo.tBar.width} height={logo.tBar.height} rx={logo.tRadius} fill={colors.white} />
          <Rect x={46.75} y={38} width={logo.tStem.width} height={logo.tStem.height} rx={logo.tRadius} fill={colors.white} />
        </Svg>
        <Animated.View style={[StyleSheet.absoluteFill, { transform: [{ rotate }] }]}>
          <Svg width={size} height={size} viewBox="0 0 100 100">
            <Path d="M50 20 A30 30 0 1 1 21.47 59.27" fill="none" stroke={brand.amber} strokeWidth={logo.ringWidth} strokeLinecap="round" />
            <Circle cx={25.73} cy={32.37} r={logo.targetDiameter / 2} fill={colors.white} />
          </Svg>
        </Animated.View>
      </Animated.View>
      {label ? (
        <Text variant="caption" color={semantic.textSecondary} style={styles.hint}>
          {hint}
        </Text>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: { flexGrow: 1, minHeight: 220, alignItems: 'center', justifyContent: 'center', gap: spacing.md, paddingVertical: spacing.xl },
  hint: { textAlign: 'center' },
});
