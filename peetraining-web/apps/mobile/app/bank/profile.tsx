// 3.8 考情分析：标「AI 统计」并写明依据（哪份资料、哪些年份、几套）；题型结构与建议用时；板块分值占比与「你的掌握度」对照；
// 缺资料提醒；高频考点（可「刷一组高频考点」）；出题风格标签；回忆版等不完整的题不计入并注明数量（PRD 11.11）。
import { semantic, spacing } from '@training/ui-tokens';
import { useQuery } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { Button, Card, EmptyState, ErrorState, Loading, ProgressBar, Screen, Tag, Text, toast } from '@/components';
import { stateNames, stateTone } from '@/features/bank/api';
import { qtypeNames } from '@/features/import/api';
import { PageHeader } from '@/features/import/ui';
import { api, unwrap } from '@/lib/api';

const pct = (v: number) => `${Math.round(v * 100)}%`;

export default function ExamProfileScreen() {
  const subjectId = Number(useLocalSearchParams<{ subjectId: string }>().subjectId);
  const subjects = useQuery({ queryKey: ['subjects'], queryFn: () => unwrap(api.GET('/subjects')) });
  const subject = subjects.data?.items.find((s) => s.id === subjectId);
  const q = useQuery({
    queryKey: ['bank', subjectId, 'exam-profile'],
    queryFn: () => unwrap(api.GET('/subjects/{subjectId}/exam-profile', { params: { path: { subjectId } } })),
  });
  if (q.isLoading) return <Screen><Loading rows={8} /></Screen>;
  if (q.isError || !q.data) return <Screen><ErrorState error={q.error} onRetry={() => void q.refetch()} /></Screen>;
  const p = q.data;
  const title = subject ? `${subject.code ? `${subject.code} ` : ''}${subject.name}怎么考` : '考情分析';

  if (!p.ready) {
    return (
      <Screen>
        <PageHeader title="考情分析" onBack={() => router.back()} />
        <EmptyState
          title={p.paper_count === 0 ? '还没有导入真题' : `目前只有 ${p.paper_count} 套真题`}
          desc={`至少 ${p.min_papers} 套不同年份的真题卷才能统计这门课怎么考。导入更多年份的真题后会自动更新`}
          actionText="导入真题"
          onAction={() => router.push({ pathname: '/import', params: { subjectId: String(subjectId) } })}
        />
      </Screen>
    );
  }
  const years = [...p.years].sort((a, b) => a - b);
  const span = years.length ? `${years[0]}–${years[years.length - 1]}` : '';
  const used = p.structure.reduce((s, x) => s + x.suggested_minutes, 0);
  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        <PageHeader title={title} onBack={() => router.back()} right={<Tag label="AI 统计" tone="ai" />} />
        <Text variant="caption">
          依据：{p.basis.map((b) => b.file_name).join('、')} 中 {span} 年 {p.paper_count} 套
        </Text>

        <Card style={styles.gap}>
          <View style={styles.row}>
            <Text variant="bodyStrong" style={styles.flex}>
              题型结构与建议用时
            </Text>
            <Text variant="caption">{p.changed_years?.length ? `${p.changed_years.join('、')} 年有变化` : `近 ${p.stable_years ?? 0} 年未变`}</Text>
          </View>
          {p.structure.map((s) => (
            <View key={s.qtype} style={styles.row}>
              <Text variant="body" style={styles.flex}>
                {qtypeNames[s.qtype]}
              </Text>
              <Text variant="caption">
                {s.count} 题 × {s.score_each} 分
              </Text>
              <Text variant="number" style={styles.num}>
                {s.total} 分
              </Text>
              <Text variant="number" style={styles.num}>
                {s.suggested_minutes}′
              </Text>
            </View>
          ))}
          <Text variant="caption">
            建议用时按分值折算，另留 {p.check_minutes} 分钟检查 · 合计 {used + (p.check_minutes ?? 0)} 分钟
          </Text>
        </Card>

        <Card style={styles.gap}>
          <View style={styles.row}>
            <Text variant="bodyStrong" style={styles.flex}>
              板块分值占比
            </Text>
            <Text variant="caption">你的掌握度</Text>
          </View>
          {p.sections.map((s) => (
            <View key={s.id} style={styles.section}>
              <View style={styles.row}>
                <Text variant="body" style={styles.flex}>
                  {s.name}
                </Text>
                <Text variant="number">{pct(s.share)}</Text>
                <Text variant="number" color={s.mastery < 40 ? semantic.danger : semantic.textPrimary} style={styles.num}>
                  {Math.round(s.mastery)}%
                </Text>
              </View>
              <ProgressBar value={s.share} target={s.mastery / 100} />
            </View>
          ))}
          {p.missing_sections.map((s) => (
            <View key={s.id} style={styles.missing}>
              <Text variant="caption">
                {s.name}占 {pct(s.share)} 分值，你的资料里只有 {s.kp_count} 个知识点、掌握度 {Math.round(s.mastery)}%。可以补一份{s.name}的讲义或笔记
              </Text>
            </View>
          ))}
        </Card>

        <Card style={styles.gap}>
          <View style={styles.row}>
            <Text variant="bodyStrong" style={styles.flex}>
              高频考点
            </Text>
            <Text variant="caption">真题考过 {p.high_freq_total ?? 0} 个</Text>
          </View>
          {p.high_freq.length === 0 ? <Text variant="caption">还没有考过 2 次以上的知识点</Text> : null}
          {p.high_freq.map((k, i) => (
            <Pressable key={k.kp_id} accessibilityRole="button" onPress={() => router.push({ pathname: '/bank/kp/[id]', params: { id: String(k.kp_id), subjectId: String(subjectId) } })} style={styles.row}>
              <Text variant="number" color={semantic.textSecondary}>
                {i + 1}
              </Text>
              <View style={styles.flex}>
                <Text variant="body">{k.name}</Text>
                <Text variant="caption">
                  {k.path.join(' · ')} · 考过 {k.exam_count} 次
                </Text>
              </View>
              <Tag label={stateNames[k.state]} tone={stateTone[k.state]} />
            </Pressable>
          ))}
        </Card>

        {p.style_tags.length > 0 ? (
          <Card style={styles.gap}>
            <Text variant="bodyStrong">出题风格</Text>
            <View style={styles.tags}>
              {p.style_tags.map((t) => (
                <Tag key={t} label={t} tone="brand" />
              ))}
            </View>
          </Card>
        ) : null}

        <Button title="刷一组高频考点" disabled={p.high_freq.length === 0} onPress={() => toast('专项练习在训练模块上线后开放')} />
        <Text variant="caption">
          只统计你导入的真题{p.excluded_count > 0 ? `，另有 ${p.excluded_count} 道回忆版或缺分值的题目不完整，未计入` : ''}。导入更多年份后会自动更新
        </Text>
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing.xl, gap: spacing.md },
  gap: { gap: spacing.sm },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  flex: { flex: 1 },
  num: { minWidth: 48, textAlign: 'right' },
  section: { gap: spacing.xs },
  missing: { padding: spacing.sm, borderRadius: 8, backgroundColor: semantic.amberSoft },
  tags: { flexDirection: 'row', flexWrap: 'wrap', gap: spacing.xs },
});
