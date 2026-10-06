// 3.7 原文查看：按资料分页查看识别出的文字，定位到出处段落并高亮；上一页、下一页；显示本页还识别出哪些知识点。
import { semantic, spacing } from '@training/ui-tokens';
import { useQuery } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { ScrollView, StyleSheet, View } from 'react-native';
import { Button, Card, ErrorState, Loading, Screen, Text } from '@/components';
import { bankKeys } from '@/features/bank/api';
import { Marked } from '@/features/bank/Marked';
import { PageHeader } from '@/features/import/ui';
import { api, unwrap } from '@/lib/api';

export default function SourcePage() {
  const p = useLocalSearchParams<{ materialId: string; page: string; highlight?: string }>();
  const materialId = Number(p.materialId);
  const [page, setPage] = useState(Number(p.page) || 1);
  const highlight = page === Number(p.page) ? p.highlight : undefined;
  const q = useQuery({
    queryKey: bankKeys.page(materialId, page, highlight),
    queryFn: () => unwrap(api.GET('/materials/{materialId}/pages/{pageNo}', { params: { path: { materialId, pageNo: page }, query: { highlight: highlight || undefined } } })),
  });
  if (q.isLoading) return <Screen><Loading rows={8} /></Screen>;
  if (q.isError || !q.data) return <Screen><ErrorState error={q.error} onRetry={() => void q.refetch()} /></Screen>;
  const d = q.data;
  return (
    <Screen>
      <PageHeader title={d.file_name} onBack={() => router.back()} right={<Text variant="small">{`${d.page_no}/${d.page_count}`}</Text>} />
      <ScrollView contentContainerStyle={styles.scroll}>
        <Card tone="fill" style={styles.paper}>
          <Marked text={d.text} highlights={d.highlights} low={d.low_confidence} />
        </Card>
        {d.low_confidence.length > 0 ? <Text variant="small">虚线标出的地方识别得不太确定</Text> : null}
      </ScrollView>
      {d.knowledge_points.length > 0 ? (
        <View style={styles.kps}>
          <Text variant="small">本页还识别出 {d.knowledge_points.length} 个知识点：</Text>
          {d.knowledge_points.map((k, i) => (
            <Text key={k.id} variant="small" color={semantic.textPrimary} onPress={() => router.push({ pathname: '/bank/kp/[id]', params: { id: String(k.id) } })}>
              {i > 0 ? '、' : ''}
              {k.name}
            </Text>
          ))}
        </View>
      ) : null}
      <View style={styles.nav}>
        <Button title="上一页" kind="secondary" disabled={page <= 1} onPress={() => setPage(page - 1)} style={styles.flex} />
        <Button title="下一页" kind="secondary" disabled={page >= d.page_count} onPress={() => setPage(page + 1)} style={styles.flex} />
      </View>
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing.md, gap: spacing.sm, flexGrow: 1 },
  paper: { flexGrow: 1, padding: 20 },
  kps: { flexDirection: 'row', flexWrap: 'wrap', justifyContent: 'center', paddingTop: 4 },
  nav: { flexDirection: 'row', gap: 10, paddingVertical: spacing.sm },
  flex: { flex: 1 },
});
