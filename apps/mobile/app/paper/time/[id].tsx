// 4.25 时间分析报告：是否用满时间、未作答题数、估计时间失分；各题型建议与实际用时对比，超时与少用标出，检查时间；
// 下次分配建议；近几次整卷未答题数趋势；「按建议再做一套」。建议用时与选择模式、考情分析同一套数字（PRD 11.9）。
import type { Schemas } from '@training/api-client';
import { radius, semantic, spacing } from '@training/ui-tokens';
import { router, useLocalSearchParams } from 'expo-router';
import { ScrollView, StyleSheet, View } from 'react-native';
import { Button, Card, ErrorState, Loading, Screen, Tag, Text } from '@/components';
import { qtypeNames } from '@/features/import/api';
import { PageHeader } from '@/features/import/ui';
import { usePaperReport } from '@/features/paper/api';

type Status = Schemas['TimeStatus'];

function statusTag(status: Status, diff: number, unfinished?: number) {
  const parts: string[] = [];
  if (status === 'overtime') parts.push(`超时 ${diff}′`);
  if (status === 'under') parts.push(`少用 ${-diff}′`);
  if (unfinished) parts.push(`${unfinished} 题未完`);
  if (parts.length === 0) return <Tag label="合适" tone="mastered" />;
  return <Tag label={parts.join(' · ')} tone={status === 'overtime' || unfinished ? 'danger' : 'info'} />;
}

/** 建议与实际用时的对比条：两条横条按两者中较大的那个缩放。 */
function Bars({ suggested, actual, status }: { suggested: number; actual: number; status: Status }) {
  const top = Math.max(suggested, actual, 1);
  return (
    <View style={styles.bars}>
      <View style={styles.barRow}>
        <Text variant="small" style={styles.barLabel}>
          建议
        </Text>
        <View style={styles.track}>
          <View style={[styles.bar, { width: `${(suggested / top) * 100}%` }, styles.barSuggested]} />
        </View>
        <Text variant="small" style={styles.barValue}>
          {suggested}′
        </Text>
      </View>
      <View style={styles.barRow}>
        <Text variant="small" style={styles.barLabel}>
          实际
        </Text>
        <View style={styles.track}>
          <View style={[styles.bar, { width: `${(actual / top) * 100}%` }, status === 'overtime' ? styles.barOver : styles.barActual]} />
        </View>
        <Text variant="small" style={styles.barValue}>
          {actual}′
        </Text>
      </View>
    </View>
  );
}

export default function TimeReportPage() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const report = usePaperReport(Number(id));
  if (report.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (report.isError || !report.data) return <Screen><ErrorState error={report.error} onRetry={() => void report.refetch()} /></Screen>;
  const r = report.data;
  const t = r.time;
  if (!t) {
    return (
      <Screen>
        <PageHeader title="时间分析" onBack={() => router.back()} />
        <Text variant="body">练习模式没有时间分析，用模拟考试模式做一套就能看到。</Text>
      </Screen>
    );
  }
  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        <PageHeader title="时间分析" onBack={() => router.back()} />
        <View style={styles.stats}>
          <View style={styles.stat}>
            <Text variant="number">{t.used_full ? `${t.total_minutes}′` : `${t.used_minutes}′`}</Text>
            <Text variant="caption">{t.used_full ? '用满时间' : `共 ${t.total_minutes}′，未用满`}</Text>
          </View>
          <View style={styles.stat}>
            <Text variant="number">{t.unanswered} 题</Text>
            <Text variant="caption">未作答</Text>
          </View>
          <View style={styles.stat}>
            <Text variant="number">约 {t.time_loss}</Text>
            <Text variant="caption">时间失分</Text>
          </View>
        </View>

        <Card style={styles.gap}>
          {t.sections.map((s) => (
            <View key={s.qtype} style={styles.section}>
              <View style={styles.row}>
                <Text variant="bodyStrong" style={styles.flex}>
                  {qtypeNames[s.qtype]}
                </Text>
                {statusTag(s.status, s.diff_minutes, s.unfinished ? s.count - s.answered : 0)}
              </View>
              <Bars suggested={s.suggested_minutes} actual={s.actual_minutes} status={s.status} />
            </View>
          ))}
          <View style={styles.section}>
            <View style={styles.row}>
              <Text variant="bodyStrong" style={styles.flex}>
                检查
              </Text>
              {t.check_status === 'under' && t.check_actual_minutes === 0 ? (
                <Tag label="没有留出" tone="danger" />
              ) : (
                statusTag(t.check_status, t.check_actual_minutes - t.check_suggested_minutes)
              )}
            </View>
            <Bars suggested={t.check_suggested_minutes} actual={t.check_actual_minutes} status={t.check_status} />
          </View>
        </Card>

        <Card style={styles.gap}>
          <Text variant="h3">下次这样分配</Text>
          {t.advice.map((a) => (
            <Text key={a} variant="body">
              · {a}
            </Text>
          ))}
        </Card>

        {t.trend.length > 1 ? (
          <Card style={styles.gap}>
            <Text variant="caption">近 {t.trend.length} 次整卷未答题</Text>
            <Text variant="number">{t.trend.map((p) => p.unanswered).join(' → ')}</Text>
          </Card>
        ) : null}
      </ScrollView>
      <Button
        title="按建议再做一套"
        onPress={() =>
          r.paper_id
            ? router.push({ pathname: '/paper/[id]', params: { id: String(r.paper_id) } })
            : router.push({ pathname: '/paper/list', params: { subjectId: String(r.subject_id) } })
        }
      />
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { gap: spacing.md, paddingBottom: spacing.xl },
  gap: { gap: spacing.sm },
  flex: { flex: 1 },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  stats: { flexDirection: 'row', gap: spacing.sm },
  stat: { flex: 1, alignItems: 'center', padding: spacing.md, borderRadius: radius.md, backgroundColor: semantic.surface, gap: 2 },
  section: { gap: spacing.xs, paddingVertical: spacing.xs },
  bars: { gap: 4 },
  barRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  barLabel: { width: 28 },
  barValue: { width: 36, textAlign: 'right' },
  track: { flex: 1, height: 8, borderRadius: 4, backgroundColor: semantic.background, overflow: 'hidden' },
  bar: { height: 8, borderRadius: 4 },
  barSuggested: { backgroundColor: semantic.border },
  barActual: { backgroundColor: semantic.primary },
  barOver: { backgroundColor: semantic.danger },
});
