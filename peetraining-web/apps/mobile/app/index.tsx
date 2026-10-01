// 0.1 启动页：按 VI 竖版组合（夜靛底、琥珀「考研」、白色 Training），约 1.5 秒后按登录与引导状态分流。
// 分流逻辑（登录态、引导进度、强制更新、协议更新）在 T06 接入 GET /bootstrap，这里先进入今日首页。
import { colors, fontFamily, spacing } from '@training/ui-tokens';
import { router } from 'expo-router';
import { useEffect } from 'react';
import { StyleSheet, View } from 'react-native';
import { Logo, Text } from '@/components';

export const SPLASH_MS = 1500;

export default function Splash() {
  useEffect(() => {
    const t = setTimeout(() => router.replace('/(tabs)/today'), SPLASH_MS);
    return () => clearTimeout(t);
  }, []);
  return (
    <View style={styles.root}>
      <Logo size={96} />
      <View style={styles.wordmark}>
        <Text style={styles.cn}>考研</Text>
        <Text style={styles.en}>Training</Text>
      </View>
      <Text variant="caption" color="#C4C0E0" style={styles.slogan}>
        考研文科专业课 AI 提分教练
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1, backgroundColor: colors.indigo, alignItems: 'center', justifyContent: 'center' },
  wordmark: { flexDirection: 'row', alignItems: 'baseline', marginTop: spacing.xl, gap: spacing.xs },
  cn: { fontFamily: fontFamily.serifHeavy, fontSize: 32, lineHeight: 40, color: colors.amber },
  en: { fontFamily: fontFamily.numberBold, fontSize: 30, lineHeight: 40, color: colors.white },
  slogan: { marginTop: spacing.sm },
});
