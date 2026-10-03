// 3.7 原文查看：按资料分页查看识别出的文字，定位到出处段落并高亮；上一页、下一页；显示本页还识别出哪些知识点。
import { spacing } from '@training/ui-tokens';
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
      <PageHeader title={d.file_name} desc={`${d.page_no} / ${d.page_count}`} onBack={() => router.back()} />
      <ScrollView contentContainerStyle={styles.scroll}>
        <Marked text={d.text} highlights={d.highlights} low={d.low_confidence} />
        {d.low_confidence.length > 0 ? <Text variant="caption">虚线标出的地方识别得不太确定</Text> : null}
        {d.knowledge_points.length > 0 ? (
          <Card style={styles.kps}>
            <Text variant="caption">本页还识别出 {d.knowledge_points.length} 个知识点：</Text>
            <View style={styles.row}>
              {d.knowledge_points.map((k) => (
                <Button key={k.id} title={k.name} kind="text" onPress={() => router.push({ pathname: '/bank/kp/[id]', params: { id: String(k.id) } })} />
              ))}
            </View>
          </Card>
        ) : null}
      </ScrollView>
      <View style={styles.nav}>
        <Button title="上一页" kind="secondary" disabled={page <= 1} onPress={() => setPage(page - 1)} style={styles.flex} />
        <Button title="下一页" kind="secondary" disabled={page >= d.page_count} onPress={() => setPage(page + 1)} style={styles.flex} />
      </View>
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing.xl, gap: spacing.md },
  kps: { gap: spacing.xs },
  row: { flexDirection: 'row', flexWrap: 'wrap' },
  nav: { flexDirection: 'row', gap: spacing.sm, paddingVertical: spacing.sm },
  flex: { flex: 1 },
});
