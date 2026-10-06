// 4.12 错题本：统计（总数、待重做、本周新增、已消灭）；按知识点 / 题型 / 失分原因分组；每题显示错误次数或最近得分、来源；
// 「重做」一组、「重做全部」按下次复习日升序。收录与移出规则见 PRD 11.8。
import type { Schemas } from '@training/api-client';
import { colors, fontFamily, semantic, spacing } from '@training/ui-tokens';
import { router, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { Button, Card, EmptyState, ErrorState, Loading, Screen, Text } from '@/components';
import { qtypeNames } from '@/features/import/api';
import { PageHeader, Segments } from '@/features/import/ui';
import { addedReasonNames, lossNames, useStartPractice, useWrongBook } from '@/features/practice/api';

type By = Schemas['WrongGroupBy'];
type Item = Schemas['WrongItem'];

function groupOf(it: Item, by: By): { key: string; name: string } {
  switch (by) {
    case 'kp':
      return it.kp ? { key: String(it.kp.id), name: it.kp.name } : { key: '0', name: '未关联知识点' };
    case 'qtype':
      return { key: it.qtype, name: qtypeNames[it.qtype] };
    default:
      return it.loss_type ? { key: it.loss_type, name: lossNames[it.loss_type] } : { key: '', name: addedReasonNames[it.added_reason] };
  }
}

function sourceLine(it: Item) {
  const src = it.source === 'exam' ? `${it.exam_year ?? ''} 真题` : it.source === 'ai_generated' ? 'AI 变式题' : '习题';
  const last = it.last_score_rate !== undefined ? `最近得分率 ${Math.round(it.last_score_rate * 100)}%` : `错 ${it.wrong_count} 次`;
  return `${src} · ${last}`;
}

export default function WrongBookPage() {
  // group=loss 从提分看板的失分归因进来（6.2「点击看相关错题」）。
  const { subjectId, group } = useLocalSearchParams<{ subjectId: string; group?: By }>();
  const sid = Number(subjectId);
  const wb = useWrongBook(sid);
  const start = useStartPractice();
  const [by, setBy] = useState<By>(group === 'loss' || group === 'qtype' ? group : 'kp');

  if (wb.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (wb.isError || !wb.data) return <Screen><ErrorState error={wb.error} onRetry={() => void wb.refetch()} /></Screen>;
  const d = wb.data;
  const groups: { key: string; name: string; items: Item[] }[] = [];
  for (const it of d.items) {
    const g = groupOf(it, by);
    let row = groups.find((x) => x.key === g.key && x.name === g.name);
    if (!row) groups.push((row = { ...g, items: [] }));
    row.items.push(it);
  }

  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        <PageHeader title="错题本" onBack={() => router.back()} />
        <View style={styles.stats}>
          <View style={styles.stat}>
            <Text style={styles.statNum}>{d.to_redo}</Text>
            <View style={styles.dotRow}>
              <View style={styles.dot} />
              <Text variant="small">待重做</Text>
            </View>
          </View>
          <View style={styles.stat}>
            <Text style={styles.statNum}>{d.week_new}</Text>
            <Text variant="small">本周新增</Text>
          </View>
          <View style={styles.stat}>
            <Text style={styles.statNum}>{d.eliminated}</Text>
            <Text variant="small">已消灭</Text>
          </View>
        </View>
        {d.items.length === 0 ? (
          <EmptyState title="没有待重做的错题" desc="答错、没拿满分或看了答案的题会收进来；在两个不同日期连续答对后自动移出" />
        ) : (
          <>
            <Segments<By>
              value={by}
              onChange={setBy}
              options={[
                { key: 'kp', label: '按知识点' },
                { key: 'qtype', label: '按题型' },
                { key: 'loss', label: '按失分原因' },
              ]}
            />
            {groups.map((g) => (
              <Card key={`${g.key}:${g.name}`} style={styles.card}>
                <View style={styles.row}>
                  <View style={[styles.flex, styles.gap2]}>
                    <Text variant="body" style={styles.bold}>
                      {g.name}
                    </Text>
                    <Text variant="small">{g.items.length} 题</Text>
                  </View>
                  {g.key ? (
                    <Button title="重做这组" kind="soft" size="sm" onPress={() => start.mutate({ subject_id: sid, kind: 'wrong_redo', wrong_group: { by, key: g.key } })} />
                  ) : null}
                </View>
                {g.items.map((it) => (
                  <Pressable
                    key={it.question_id}
                    accessibilityRole="button"
                    onPress={() => router.push({ pathname: '/bank/question/[id]', params: { id: String(it.question_id) } })}
                    style={styles.item}
                  >
                    <Text variant="body" numberOfLines={2}>
                      {qtypeNames[it.qtype]}：{it.stem}
                    </Text>
                    <Text variant="small">{sourceLine(it)}</Text>
                  </Pressable>
                ))}
              </Card>
            ))}
          </>
        )}
      </ScrollView>
      {d.items.length > 0 ? (
        <View style={styles.footer}>
          <Button title={`重做全部 ${d.total} 题`} loading={start.isPending} onPress={() => start.mutate({ subject_id: sid, kind: 'wrong_redo' })} />
        </View>
      ) : null}
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { gap: 12, paddingBottom: spacing.xl },
  stats: { flexDirection: 'row', marginTop: spacing.sm, marginBottom: 4 },
  stat: { flex: 1, gap: 2 },
  statNum: { fontFamily: fontFamily.numberSemiBold, fontSize: 26, lineHeight: 32, color: colors.ink },
  dotRow: { flexDirection: 'row', alignItems: 'center', gap: 5 },
  dot: { width: 7, height: 7, borderRadius: 4, backgroundColor: colors.amber },
  card: { gap: 4, paddingVertical: 14 },
  row: { flexDirection: 'row', alignItems: 'center', gap: 8, paddingBottom: 4 },
  flex: { flex: 1 },
  gap2: { gap: 2 },
  bold: { fontWeight: '700' },
  item: { minHeight: 52, paddingVertical: 10, borderTopWidth: 1, borderTopColor: semantic.border, gap: 2 },
  footer: { paddingVertical: spacing.md },
});
