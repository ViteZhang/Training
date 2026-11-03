// 1.11a 申请收录成功（PRD v3 增补，设计稿 n_apply_done）：申请的课、目前申请人数；引导「导入我的资料」或「先进入 App」。
// 处理结果（已立项、已上线、暂不收录及原因）通过消息中心通知。
import { colors, radius, semantic, spacing } from '@training/ui-tokens';
import { router, useLocalSearchParams } from 'expo-router';
import { StyleSheet, View } from 'react-native';
import { Button, Icon, Screen, Text } from '@/components';
import { setStep } from '@/features/onboarding/api';

export default function ApplyDone() {
  const { count, label, entry, again } = useLocalSearchParams<{ count?: string; label?: string; entry?: string; again?: string }>();
  const onboarding = (entry ?? 'onboarding') === 'onboarding';
  const importOwn = async () => {
    if (onboarding) {
      await setStep('1.4');
      router.replace('/(onboarding)/import');
    } else router.replace('/import');
  };
  const enter = async () => {
    if (onboarding) await setStep('done');
    router.replace('/(tabs)/today');
  };
  return (
    <Screen>
      <View style={styles.body}>
        <View style={styles.badge}>
          <Icon name="check" size={34} color={semantic.mastered} />
        </View>
        <Text variant="h1">{again ? '你已申请过' : '申请已收到'}</Text>
        <Text variant="caption" style={styles.center}>
          {label}
          {count ? `\n目前有 ${count} 位同学申请了这门课。` : '\n'}收录上线后会第一时间通知你。
        </Text>
        <View style={styles.tip}>
          <Text variant="bodyStrong">等待的时候</Text>
          <Text variant="caption">先把你手里的真题导进来，现在就能刷、能批改。官方题库上线后两边可以一起用。</Text>
        </View>
      </View>
      <View style={styles.footer}>
        <Button title="导入我的资料" onPress={() => void importOwn()} />
        <Button title="先进入 App" kind="text" onPress={() => void enter()} />
      </View>
    </Screen>
  );
}

const styles = StyleSheet.create({
  body: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: spacing.lg, paddingHorizontal: spacing.sm },
  badge: { width: 72, height: 72, borderRadius: 36, alignItems: 'center', justifyContent: 'center', backgroundColor: colors.greenSoft },
  center: { textAlign: 'center', lineHeight: 24 },
  tip: { alignSelf: 'stretch', gap: 6, padding: spacing.lg, borderRadius: radius.xl, backgroundColor: semantic.fill },
  footer: { gap: 4, paddingBottom: spacing.xxl },
});
