// 4.7 批改结果（从待批改提交、消息或作答记录进入时的独立页面）；4.8 异议。
import { useQueryClient } from '@tanstack/react-query';
import { spacing } from '@training/ui-tokens';
import { router, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { ScrollView, StyleSheet } from 'react-native';
import { Button, ErrorState, Loading, Screen } from '@/components';
import { PageHeader } from '@/features/import/ui';
import { DisputeSheet, GradingResultView, useGrading, useRegrade } from '@/features/practice/grading';

export default function GradingPage() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const qc = useQueryClient();
  const g = useGrading(Number(id));
  const [disputing, setDisputing] = useState(false);
  const show = (next: { grading_id: number }) => router.replace({ pathname: '/practice/grading/[id]', params: { id: String(next.grading_id) } });
  const regrade = useRegrade(show);
  if (g.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (g.isError || !g.data) return <Screen><ErrorState error={g.error} onRetry={() => void g.refetch()} /></Screen>;
  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        <PageHeader title="批改结果" onBack={() => router.back()} />
        <GradingResultView g={g.data} onDispute={() => setDisputing(true)} onRegrade={() => regrade.mutate(g.data!.grading_id)} regrading={regrade.isPending} />
        <Button title="查看题目" kind="secondary" onPress={() => router.push({ pathname: '/bank/question/[id]', params: { id: String(g.data!.question_id) } })} />
      </ScrollView>
      <DisputeSheet
        g={g.data}
        visible={disputing}
        onClose={() => setDisputing(false)}
        onDone={(r) => {
          void qc.invalidateQueries({ queryKey: ['grading'] });
          show(r);
        }}
      />
    </Screen>
  );
}

const styles = StyleSheet.create({ scroll: { gap: spacing.md, paddingBottom: spacing.xl } });
