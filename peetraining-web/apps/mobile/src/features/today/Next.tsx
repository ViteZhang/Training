// 今日训练每一段结束页（4.11 本组总结、4.17 背诵完成）底部的接续按钮：
// 还有没做的 →「继续今日训练（还剩 N 项）」直接进下一段；都做完了 →「查看今日总结」进 2.2。
import { spacing } from '@training/ui-tokens';
import { router } from 'expo-router';
import { StyleSheet, View } from 'react-native';
import { Button } from '@/components';
import { useAIFillPref } from '@/features/practice/offline';
import { useContinueToday, useHome } from './api';

export function TodayNextFooter({ subjectId, onLater }: { subjectId?: number; onLater: () => void }) {
  const home = useHome();
  const next = useContinueToday(true);
  const [aiFill] = useAIFillPref();
  const plan = home.data?.plan;
  // 结束页刚结算完，今日进度还在重新拉：等它回来再显示数字，免得闪一下旧的。
  const pending = home.isFetching || !plan;
  const left = plan ? plan.item_count - plan.done_count : 0;
  if (!pending && left === 0) {
    return (
      <View style={styles.footer}>
        <Button title="查看今日总结" style={styles.flex} onPress={() => router.replace('/plan/summary')} />
      </View>
    );
  }
  return (
    <View style={styles.footer}>
      <Button title="先到这里" kind="secondary" style={styles.flex} onPress={onLater} />
      <Button
        title={pending ? '继续今日训练' : `继续今日训练（还剩 ${left} 项）`}
        style={styles.flex2}
        loading={next.isPending}
        onPress={() => next.mutate({ subject_id: subjectId, ai_fill: aiFill })}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  footer: { flexDirection: 'row', gap: 10, paddingVertical: spacing.md },
  flex: { flex: 1 },
  flex2: { flex: 1.8 },
});
