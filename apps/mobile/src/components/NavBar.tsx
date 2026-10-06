import { layout, semantic } from '@training/ui-tokens';
import { router } from 'expo-router';
import type { ReactNode } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import Svg, { Path } from 'react-native-svg';
import { Text } from './Text';

/** 返回：44×44 的左箭头（设计稿各二级页左上角）。 */
export function BackButton({ onPress, label = '返回', icon = 'back' }: { onPress?: () => void; label?: string; icon?: 'back' | 'close' }) {
  return (
    <Pressable accessibilityRole="button" accessibilityLabel={label} onPress={onPress ?? (() => router.back())} style={styles.back}>
      <Svg width={22} height={22} viewBox="0 0 24 24" fill="none" stroke={semantic.textPrimary} strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round">
        <Path d={icon === 'close' ? 'M6 6l12 12M18 6L6 18' : 'M15 5l-7 7 7 7'} />
      </Svg>
    </Pressable>
  );
}

/** 顶部栏：返回 + 居中标题（15 号）+ 右侧操作（设计稿二级页通用）。 */
export function NavBar({ title, onBack, right, back = true }: { title?: string; onBack?: () => void; right?: ReactNode; back?: boolean }) {
  return (
    <View style={styles.bar}>
      <View style={styles.side}>{back ? <BackButton onPress={onBack} /> : null}</View>
      {title ? (
        <Text variant="bodyStrong" numberOfLines={1} style={styles.title} accessibilityRole="header">
          {title}
        </Text>
      ) : (
        <View style={styles.flex} />
      )}
      <View style={[styles.side, styles.right]}>{right}</View>
    </View>
  );
}

const styles = StyleSheet.create({
  bar: { flexDirection: 'row', alignItems: 'center', minHeight: layout.minTouch, marginTop: 4 },
  side: { minWidth: 72, flexDirection: 'row', alignItems: 'center' },
  right: { justifyContent: 'flex-end' },
  title: { flex: 1, textAlign: 'center' },
  flex: { flex: 1 },
  back: { width: layout.minTouch, height: layout.minTouch, alignItems: 'center', justifyContent: 'center', marginLeft: -12 },
});
