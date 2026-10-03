// 3.9 知识图谱：按板块显示知识点网络，节点颜色 = 掌握状态，大小 = 真题次数；连线 = 关联（易混对比、组成要素、同章并列、相关）；
// 可缩放、点节点看摘要并进卡片；筛选全部 / 薄弱 / 真题考过。关联由 AI 按资料章节和同题出现整理，可在卡片里调整。
import type { Schemas } from '@training/api-client';
import { ApiError } from '@training/api-client';
import { colors, semantic, spacing } from '@training/ui-tokens';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { useMemo, useState } from 'react';
import { ScrollView, StyleSheet, View } from 'react-native';
import Svg, { Circle, G, Line, Text as SvgText } from 'react-native-svg';
import { BottomSheet, Button, EmptyState, ErrorState, Loading, Screen, Text, toast } from '@/components';
import { relationNames, stateNames } from '@/features/bank/api';
import { layoutGraph } from '@/features/bank/graphLayout';
import { PageHeader, Segments } from '@/features/import/ui';
import { api, unwrap } from '@/lib/api';

type Filter = 'all' | 'weak' | 'exam';
const stateColor: Record<Schemas['MasteryState'], string> = {
  mastered: semantic.mastered,
  consolidating: semantic.progress,
  learning: semantic.info,
  unlearned: colors.line,
};
const SIZE = 800;

export default function GraphScreen() {
  const subjectId = Number(useLocalSearchParams<{ subjectId: string }>().subjectId);
  const qc = useQueryClient();
  const [filter, setFilter] = useState<Filter>('all');
  const [section, setSection] = useState<number | undefined>();
  const [zoom, setZoom] = useState(1);
  const [picked, setPicked] = useState<number | null>(null);
  const key = ['bank', subjectId, 'graph', filter, section ?? 0];
  const graph = useQuery({
    queryKey: key,
    queryFn: () => unwrap(api.GET('/subjects/{subjectId}/knowledge-graph', { params: { path: { subjectId }, query: { filter, section_id: section } } })),
  });
  const data = graph.data;
  const layout = useMemo(
    () =>
      data
        ? layoutGraph(
            data.nodes.map((n) => ({ id: n.id, sectionId: n.section_id, examCount: n.exam_count })),
            data.edges.map((e) => ({ source: e.source_id, target: e.target_id })),
            SIZE,
          )
        : new Map(),
    [data],
  );

  if (graph.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (graph.isError || !data) return <Screen><ErrorState error={graph.error} onRetry={() => void graph.refetch()} /></Screen>;
  const node = data.nodes.find((n) => n.id === picked);
  const name = (id: number) => data.nodes.find((n) => n.id === id)?.name ?? '';
  const rels = node ? data.edges.filter((e) => e.source_id === node.id || e.target_id === node.id) : [];

  const removeRelation = async (id: number) => {
    try {
      await unwrap(api.DELETE('/knowledge-relations/{relationId}', { params: { path: { relationId: id } } }));
      await qc.invalidateQueries({ queryKey: ['bank', subjectId, 'graph'] });
    } catch (e) {
      toast(e instanceof ApiError ? e.message : '删除没成功，请重试');
    }
  };

  return (
    <Screen>
      <PageHeader title="知识图谱" onBack={() => router.back()} />
      <View style={styles.gap}>
        {data.sections.length > 1 ? (
          <Segments
            value={String(section ?? 0)}
            onChange={(v) => setSection(Number(v) || undefined)}
            options={[{ key: '0', label: '全部板块' }, ...data.sections.map((s) => ({ key: String(s.id), label: s.name }))]}
          />
        ) : null}
        <Segments<Filter>
          value={filter}
          onChange={setFilter}
          options={[
            { key: 'all', label: '全部' },
            { key: 'weak', label: '薄弱' },
            { key: 'exam', label: '真题考过' },
          ]}
        />
      </View>
      {data.nodes.length === 0 ? (
        <EmptyState title="这里还没有知识点" />
      ) : (
        <ScrollView style={styles.canvas} contentContainerStyle={{ width: SIZE * zoom }} horizontal>
          <ScrollView contentContainerStyle={{ height: SIZE * zoom }}>
            <Svg width={SIZE * zoom} height={SIZE * zoom} viewBox={`0 0 ${SIZE} ${SIZE}`} accessibilityLabel="知识图谱">
              {data.edges.map((e) => {
                const a = layout.get(e.source_id);
                const b = layout.get(e.target_id);
                if (!a || !b) return null;
                return (
                  <Line
                    key={e.id}
                    x1={a.x}
                    y1={a.y}
                    x2={b.x}
                    y2={b.y}
                    stroke={e.relation_type === 'contrast' ? semantic.danger : semantic.border}
                    strokeDasharray={e.relation_type === 'sibling' ? '4 4' : undefined}
                    strokeWidth={picked && (e.source_id === picked || e.target_id === picked) ? 2.5 : 1}
                  />
                );
              })}
              {data.nodes.map((n) => {
                const p = layout.get(n.id);
                if (!p) return null;
                return (
                  <G key={n.id} onPress={() => setPicked(n.id)}>
                    <Circle cx={p.x} cy={p.y} r={p.r} fill={stateColor[n.state]} stroke={n.id === picked ? semantic.primary : semantic.surface} strokeWidth={n.id === picked ? 3 : 1.5} />
                    <SvgText x={p.x} y={p.y + p.r + 12} fontSize={11} fill={semantic.textPrimary} textAnchor="middle">
                      {n.name.length > 6 ? `${n.name.slice(0, 6)}…` : n.name}
                    </SvgText>
                  </G>
                );
              })}
            </Svg>
          </ScrollView>
        </ScrollView>
      )}
      <View style={styles.footer}>
        <Text variant="small" color={semantic.textSecondary} style={styles.flex}>
          {(['mastered', 'consolidating', 'learning', 'unlearned'] as const).map((s) => `● ${stateNames[s]}`).join('  ')} · 圆越大真题考得越多
        </Text>
        <Button title="－" kind="secondary" accessibilityLabel="缩小" disabled={zoom <= 0.5} onPress={() => setZoom(Math.max(zoom - 0.25, 0.5))} />
        <Button title="＋" kind="secondary" accessibilityLabel="放大" disabled={zoom >= 2.5} onPress={() => setZoom(Math.min(zoom + 0.25, 2.5))} />
      </View>
      <BottomSheet visible={!!node} onClose={() => setPicked(null)} title={node?.name ?? ''}>
        {node ? (
          <View style={styles.gap}>
            <Text variant="caption">
              {stateNames[node.state]} · 掌握 {Math.round(node.m)}
              {node.exam_count ? ` · 真题考过 ${node.exam_count} 次` : ''}
            </Text>
            {rels.length === 0 ? <Text variant="caption">还没有关联</Text> : null}
            {rels.map((r) => (
              <View key={r.id} style={styles.rel}>
                <Text variant="body" style={styles.flex}>
                  {name(r.source_id === node.id ? r.target_id : r.source_id)}（{relationNames[r.relation_type]}）
                </Text>
                <Button title="删除" kind="text" onPress={() => void removeRelation(r.id)} />
              </View>
            ))}
            <Button
              title="查看知识点卡片"
              onPress={() => {
                setPicked(null);
                router.push({ pathname: '/bank/kp/[id]', params: { id: String(node.id), subjectId: String(subjectId) } });
              }}
            />
            <Text variant="small" color={semantic.textSecondary}>
              关联由 AI 根据你资料里的章节和同题出现整理，在知识点卡片里可以添加
            </Text>
          </View>
        ) : null}
      </BottomSheet>
    </Screen>
  );
}

const styles = StyleSheet.create({
  gap: { gap: spacing.sm },
  canvas: { flex: 1, marginTop: spacing.sm, backgroundColor: semantic.surface, borderRadius: 12 },
  footer: { flexDirection: 'row', alignItems: 'center', gap: spacing.xs, paddingVertical: spacing.sm },
  flex: { flex: 1 },
  rel: { flexDirection: 'row', alignItems: 'center' },
});
