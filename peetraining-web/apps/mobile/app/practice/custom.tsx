// 4.2 自定义练习：练习范围（板块）、题型（多选，显示题数）、题量、只练未掌握的知识点、题库不够时 AI 出变式题；
// 实时显示符合条件的题数和预计用时（服务端计算）。
import type { Schemas } from '@training/api-client';
import { radius, semantic, spacing } from '@training/ui-tokens';
import { useQuery } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { useMemo, useState } from 'react';
import { Pressable, ScrollView, StyleSheet, Switch, View } from 'react-native';
import { Button, Card, ErrorState, Loading, Screen, Text } from '@/components';
import { qtypeNames } from '@/features/import/api';
import { PageHeader } from '@/features/import/ui';
import { usePracticeHome, usePreview, useStartPractice } from '@/features/practice/api';
import { api, unwrap } from '@/lib/api';

const counts = [5, 10, 20, 30];

function Chip({ label, on, onPress }: { label: string; on: boolean; onPress: () => void }) {
  return (
    <Pressable accessibilityRole="checkbox" accessibilityState={{ checked: on }} accessibilityLabel={label} onPress={onPress} style={[styles.chip, on && styles.chipOn]}>
      <Text variant="body" color={on ? semantic.textOnBrand : semantic.textPrimary}>
        {label}
      </Text>
    </Pressable>
  );
}

function toggle<T>(list: T[], v: T) {
  return list.includes(v) ? list.filter((x) => x !== v) : [...list, v];
}

export default function CustomPractice() {
  const { subjectId } = useLocalSearchParams<{ subjectId: string }>();
  const sid = Number(subjectId);
  const home = usePracticeHome(sid);
  const tree = useQuery({
    queryKey: ['bank', sid, 'tree', 'all'],
    queryFn: () => unwrap(api.GET('/subjects/{subjectId}/knowledge-tree', { params: { path: { subjectId: sid }, query: { filter: 'all' } } })),
  });
  const [sections, setSections] = useState<number[]>([]);
  const [qtypes, setQtypes] = useState<Schemas['QuestionType'][]>([]);
  const [count, setCount] = useState(10);
  const [onlyUnmastered, setOnlyUnmastered] = useState(false);
  const [aiFill, setAiFill] = useState(false);
  const config = useMemo(() => ({ section_ids: sections, qtypes, count, only_unmastered: onlyUnmastered, ai_fill: aiFill }), [sections, qtypes, count, onlyUnmastered, aiFill]);
  const preview = usePreview(sid, config);
  const start = useStartPractice();

  if (home.isLoading || tree.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (home.isError || !home.data) return <Screen><ErrorState error={home.error} onRetry={() => void home.refetch()} /></Screen>;
  const p = preview.data;
  const summary = p
    ? `符合条件 ${p.available} 题 · 本组 ${p.count + (p.ai_fill ?? 0)} 题${p.ai_fill ? `（AI 补 ${p.ai_fill} 题）` : ''} · 约 ${Math.round(p.minutes)} 分钟`
    : '正在计算…';

  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        <PageHeader title="自定义练习" onBack={() => router.back()} />
        <Card style={styles.card}>
          <Text variant="bodyStrong">练习范围</Text>
          <View style={styles.chips}>
            <Chip label="全部" on={sections.length === 0} onPress={() => setSections([])} />
            {(tree.data?.sections ?? []).map((s) => (
              <Chip key={s.id} label={s.name} on={sections.includes(s.id)} onPress={() => setSections((v) => toggle(v, s.id))} />
            ))}
          </View>
        </Card>
        <Card style={styles.card}>
          <Text variant="bodyStrong">题型</Text>
          <View style={styles.chips}>
            <Chip label="全部" on={qtypes.length === 0} onPress={() => setQtypes([])} />
            {home.data.qtype_counts.map((q) => (
              <Chip key={q.qtype} label={`${qtypeNames[q.qtype]} ${q.count}`} on={qtypes.includes(q.qtype)} onPress={() => setQtypes((v) => toggle(v, q.qtype))} />
            ))}
          </View>
        </Card>
        <Card style={styles.card}>
          <Text variant="bodyStrong">题量</Text>
          <View style={styles.chips}>
            {counts.map((n) => (
              <Chip key={n} label={`${n} 题`} on={count === n} onPress={() => setCount(n)} />
            ))}
          </View>
        </Card>
        <Card style={styles.card}>
          <View style={styles.switchRow}>
            <View style={styles.flex}>
              <Text variant="bodyStrong">只练未掌握的知识点</Text>
              <Text variant="caption">跳过已掌握的内容</Text>
            </View>
            <Switch accessibilityLabel="只练未掌握的知识点" value={onlyUnmastered} onValueChange={setOnlyUnmastered} trackColor={{ true: semantic.primary }} />
          </View>
          <View style={styles.switchRow}>
            <View style={styles.flex}>
              <Text variant="bodyStrong">题库不够时 AI 出变式题</Text>
              <Text variant="caption">按你资料里的知识点出题，会标「AI 出题」</Text>
            </View>
            <Switch accessibilityLabel="题库不够时 AI 出变式题" value={aiFill} onValueChange={setAiFill} trackColor={{ true: semantic.primary }} />
          </View>
        </Card>
      </ScrollView>
      <View style={styles.footer}>
        <Text variant="caption">{summary}</Text>
        <Button
          title="开始练习"
          loading={start.isPending}
          disabled={!p || p.count + (p.ai_fill ?? 0) === 0}
          onPress={() => start.mutate({ subject_id: sid, kind: 'custom', config })}
        />
      </View>
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { gap: spacing.md, paddingBottom: spacing.xl },
  card: { gap: spacing.sm },
  chips: { flexDirection: 'row', flexWrap: 'wrap', gap: spacing.sm },
  chip: { minHeight: 44, justifyContent: 'center', paddingHorizontal: spacing.md, borderRadius: radius.pill, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface },
  chipOn: { backgroundColor: semantic.primary, borderColor: semantic.primary },
  switchRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.md, minHeight: 56 },
  flex: { flex: 1 },
  footer: { gap: spacing.sm, paddingVertical: spacing.sm },
});
