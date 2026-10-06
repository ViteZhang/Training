// 1.6 AI 解析中：每个文件一行进度（识别文字 · 拆分题目 · 配答案和采分点）、预计剩余时间；可先核对已识别的题或先进入 App；
// 等待时设置学习提醒，开启时请求通知权限。1.6b 部分文件识别失败：只影响失败的文件，说明原因，可重试、保留已识别的或移除。
import { ApiError, type Schemas } from '@training/api-client';
import { colors, radius, semantic, spacing } from '@training/ui-tokens';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import * as Notifications from 'expo-notifications';
import { router, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { Button, Card, ErrorState, Icon, Loading, ProgressBar, Screen, Text, toast } from '@/components';
import { importKeys, isRunning, stepGroups, stepIndex, useImportJob, type ImportJob } from '@/features/import/api';
import { useImportFlow } from '@/features/import/store';
import { PageHeader } from '@/features/import/ui';
import { Footer } from '@/features/onboarding/ui';
import { usePermissionPrompt } from '@/features/permissions/PermissionPrompt';
import { api, unwrap } from '@/lib/api';

type JobMaterial = Schemas['ImportJobMaterial'];

function eta(seconds?: number) {
  if (!seconds) return undefined;
  return seconds < 90 ? '不到 2 分钟' : `${Math.ceil(seconds / 60)} 分钟`;
}

function MaterialProgress({ m, first }: { m: JobMaterial; first?: boolean }) {
  const current = stepIndex(m.step);
  const pct = Math.round(((current + 0.5) / stepGroups.length) * 100);
  return (
    <View style={[styles.material, !first && styles.divider]}>
      <View style={styles.row}>
        <View style={[styles.flex, styles.gap2]}>
          <Text variant="body" numberOfLines={1}>
            {m.file_name}
          </Text>
          {m.status === 'done' ? (
            <Text variant="small" color={semantic.mastered}>
              已识别 {m.recognized_count ?? 0} 条
            </Text>
          ) : m.status === 'pending' ? (
            <Text variant="small">排队中</Text>
          ) : null}
        </View>
        {m.status === 'done' ? (
          <View style={styles.ok}>
            <Icon name="check" size={14} color={semantic.mastered} />
          </View>
        ) : m.status === 'running' ? (
          <Text variant="small">{pct}%</Text>
        ) : null}
      </View>
      {m.status === 'running' ? (
        <>
          <ProgressBar value={pct / 100} height={4} color={colors.ink} />
          <View style={styles.steps}>
            {stepGroups.map((g, i) => (
              <Text key={g.label} variant="small" color={i <= current ? semantic.textPrimary : semantic.textSecondary}>
                {i < current ? '✓ ' : i === current ? '· ' : ''}
                {g.label}
              </Text>
            ))}
          </View>
        </>
      ) : null}
    </View>
  );
}

/** 文件类型小方块上的字：PDF / DOC / IMG。 */
function fileBadge(name: string) {
  const ext = name.split('.').pop()?.toLowerCase() ?? '';
  if (ext === 'pdf') return 'PDF';
  if (ext === 'doc' || ext === 'docx') return 'DOC';
  if (ext === 'xls' || ext === 'xlsx') return 'XLS';
  if (ext === 'txt') return 'TXT';
  return 'IMG';
}

/** 1.6b 失败的文件：原因 + 重试 / 移除；只识别出部分内容的可以「保留」。 */
function FailedMaterial({ job, m }: { job: ImportJob; m: JobMaterial }) {
  const qc = useQueryClient();
  const [busy, setBusy] = useState<'retry' | 'remove' | null>(null);
  const act = async (kind: 'retry' | 'remove') => {
    setBusy(kind);
    try {
      const params = { params: { path: { jobId: job.id, materialId: m.material_id } } };
      const next = kind === 'retry' ? await unwrap(api.POST('/import-jobs/{jobId}/materials/{materialId}/retry', params)) : await unwrap(api.DELETE('/import-jobs/{jobId}/materials/{materialId}', params));
      qc.setQueryData(importKeys.job(job.id), next);
    } catch (e) {
      toast(e instanceof ApiError ? e.message : '操作没成功，请重试');
    } finally {
      setBusy(null);
    }
  };
  const partial = m.status === 'partial';
  return (
    <View style={styles.failed}>
      <View style={styles.failedHead}>
        <View style={styles.badge}>
          <Text variant="small" color={semantic.danger} style={styles.badgeText}>
            {fileBadge(m.file_name)}
          </Text>
        </View>
        <View style={[styles.flex, styles.gap2]}>
          <Text variant="bodyStrong" style={styles.bold} numberOfLines={1}>
            {m.file_name}
          </Text>
          <Text variant="caption" color={semantic.danger}>
            {m.fail_reason ?? '没能识别'}
          </Text>
        </View>
      </View>
      <View style={styles.actions}>
        <Button title="重新识别" size="sm" loading={busy === 'retry'} onPress={() => void act('retry')} />
        {partial ? <Button title={`保留这 ${m.recognized_count ?? 0} 条`} kind="secondary" size="sm" onPress={() => toast('已保留，可以继续核对')} /> : null}
        <Button title="移除" kind="secondary" size="sm" color={semantic.danger} loading={busy === 'remove'} onPress={() => void act('remove')} />
      </View>
    </View>
  );
}

const reminderOptions = ['07:30', '12:30', '20:00'];

/** 等待时设置学习提醒（PRD 1.6）：保存到备考档案，开启时请求通知权限。 */
function ReminderPicker() {
  const qc = useQueryClient();
  const profile = useQuery({ queryKey: ['profile'], queryFn: () => unwrap(api.GET('/profile')) });
  const notify = usePermissionPrompt('notifications', async () => {
    const r = await Notifications.requestPermissionsAsync();
    return { granted: r.granted, canAskAgain: r.canAskAgain };
  });
  const times = profile.data?.reminder_times ?? [];

  const toggle = async (t: string) => {
    const p = profile.data;
    if (!p) return;
    const on = !times.includes(t);
    if (on && !(await notify.ensure())) return;
    const next = on ? [...times, t].sort() : times.filter((x) => x !== t);
    try {
      const saved = await unwrap(
        api.PUT('/profile', {
          body: { exam_year: p.exam_year, stage: p.stage, stage_manual: p.stage_manual, daily_minutes: p.daily_minutes as 30 | 45 | 60 | 90, reminder_times: next, notify_daily: true },
        }),
      );
      qc.setQueryData(['profile'], saved);
    } catch (e) {
      toast(e instanceof ApiError ? e.message : '提醒没设置成功，请重试');
    }
  };

  if (!profile.data) return null;
  return (
    <Card tone="fill" style={styles.reminder}>
      <View style={styles.gap2}>
        <Text variant="bodyStrong" style={styles.bold}>
          趁这会儿，设置学习提醒
        </Text>
        <Text variant="small">每天到点提醒你完成今日计划</Text>
      </View>
      <View style={styles.actions}>
        {reminderOptions.map((t) => {
          const on = times.includes(t);
          return (
            <Pressable key={t} accessibilityRole="switch" accessibilityState={{ checked: on }} accessibilityLabel={`${t} 提醒`} onPress={() => void toggle(t)} style={[styles.chip, on && styles.chipOn]}>
              <Text variant="caption" color={on ? semantic.textOnBrand : semantic.textPrimary}>
                {t}
              </Text>
            </Pressable>
          );
        })}
      </View>
      <Text variant="small" color={semantic.textSecondary}>
        开启后会请求通知权限
      </Text>
      {notify.element}
    </Card>
  );
}

export default function JobScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const jobId = Number(id);
  const job = useImportJob(jobId);
  const onboarding = useImportFlow((s) => s.onboarding);

  if (job.isLoading) return <Screen><Loading rows={5} /></Screen>;
  if (job.isError || !job.data) return <Screen><ErrorState error={job.error} onRetry={() => void job.refetch()} /></Screen>;

  const j = job.data;
  const running = isRunning(j);
  const failed = j.materials.filter((m) => m.status === 'failed' || m.status === 'partial');
  const ok = j.materials.filter((m) => m.status === 'done');
  const recognized = j.counts.questions + j.counts.knowledge_points + (j.counts.essay_items ?? 0);
  const leave = () => router.replace('/(tabs)/today');
  const review = () => router.push({ pathname: '/import/confirm/[id]', params: { id: String(jobId) } });

  let title = '正在整理你的题库';
  let desc = `预计还需 ${eta(j.eta_seconds) ?? '几分钟'}。可以先离开，整理好会通知你。`;
  if (!running && failed.length > 0 && j.status !== 'failed') {
    title = `有 ${failed.length} 项没能识别好`;
    desc = ok.length > 0 ? `另外 ${ok.length} 个文件已识别出 ${recognized} 条，不受影响。` : '可以重新识别或移除。';
  } else if (j.status === 'failed') {
    title = '这次没能识别出内容';
    desc = '看看下面的原因，换个文件或重新拍照再试。';
  } else if (!running) {
    title = '整理好了';
    desc = `识别出 ${recognized} 条，核对后就能入库。`;
  }

  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        {running ? (
          <View style={styles.runHead}>
            <View style={styles.dots} accessibilityElementsHidden>
              <View style={[styles.dot, { backgroundColor: colors.ink }]} />
              <View style={[styles.dot, { backgroundColor: '#8C877D' }]} />
              <View style={[styles.dot, { backgroundColor: '#CFCAC0' }]} />
            </View>
            <Text variant="h1">{title}</Text>
            <Text variant="caption" style={styles.desc}>
              {desc}
            </Text>
          </View>
        ) : (
          <PageHeader title={title} desc={desc} onBack={() => router.back()} />
        )}
        {failed.length > 0 ? (
          <View style={styles.gap}>
            {failed.map((m) => (
              <FailedMaterial key={m.material_id} job={j} m={m} />
            ))}
            <View style={styles.tips}>
              <Text variant="caption" color={semantic.textPrimary} style={styles.medium}>
                拍得更清楚的办法
              </Text>
              <Text variant="caption">· 光线充足，避免阴影和反光</Text>
              <Text variant="caption">· 一张照片只拍一页，页面放平、拍全</Text>
              <Text variant="caption">· 手写答案请写工整，字不要太小</Text>
            </View>
          </View>
        ) : null}
        <Card style={styles.list}>
          {j.materials
            .filter((m) => m.status !== 'failed' && m.status !== 'partial')
            .map((m, i) => (
              <MaterialProgress key={m.material_id} m={m} first={i === 0} />
            ))}
          {j.materials.length === 0 ? <Text variant="caption">没有文件了</Text> : null}
        </Card>
        {running ? <ReminderPicker /> : null}
        {j.detected_essay ? (
          <Card style={styles.essay}>
            <Icon name="sparkle" color={semantic.primary} size={18} />
            <Text variant="caption" style={styles.flex}>
              AI 判断这批是作文资料，会整理成作文知识库。判断不对可以在「备考设置」里改
            </Text>
          </Card>
        ) : null}
      </ScrollView>
      <Footer>
        {recognized > 0 ? <Button title={running ? `先核对已识别的 ${recognized} 条` : `继续核对已识别的 ${recognized} 条`} onPress={review} /> : null}
        {running || recognized === 0 ? (
          <Button
            title={running ? '先进入 App，好了通知我' : '回到首页'}
            kind={recognized > 0 ? 'text' : 'primary'}
            onPress={onboarding && !running ? () => router.replace('/import') : leave}
          />
        ) : null}
      </Footer>
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing.xl, gap: spacing.md },
  gap: { gap: spacing.md },
  gap2: { gap: 2 },
  flex: { flex: 1 },
  bold: { fontWeight: '700' },
  medium: { fontWeight: '500' },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  list: { paddingVertical: 0 },
  material: { gap: 8, paddingVertical: 16 },
  divider: { borderTopWidth: 1, borderTopColor: semantic.border },
  ok: { width: 24, height: 24, borderRadius: 12, backgroundColor: semantic.masteredSoft, alignItems: 'center', justifyContent: 'center' },
  steps: { flexDirection: 'row', gap: spacing.md, flexWrap: 'wrap' },
  failed: { gap: 14, padding: 16, borderRadius: radius.xl, borderWidth: 1, borderColor: '#F0C6C2', backgroundColor: semantic.dangerSoft },
  failedHead: { flexDirection: 'row', gap: 12, alignItems: 'flex-start' },
  badge: { width: 34, height: 34, borderRadius: 8, backgroundColor: semantic.surface, alignItems: 'center', justifyContent: 'center' },
  badgeText: { fontSize: 10, fontWeight: '700' },
  actions: { flexDirection: 'row', gap: spacing.sm, flexWrap: 'wrap', alignItems: 'center' },
  tips: { gap: 4, paddingHorizontal: 2 },
  reminder: { gap: 12 },
  chip: { minHeight: 36, paddingHorizontal: 14, justifyContent: 'center', borderRadius: radius.pill, backgroundColor: semantic.surface },
  chipOn: { backgroundColor: semantic.primary },
  essay: { flexDirection: 'row', gap: spacing.sm, alignItems: 'center', backgroundColor: semantic.primarySoft },
  runHead: { gap: 8, marginTop: 40, marginBottom: 8 },
  desc: { fontSize: 14, lineHeight: 21 },
  dots: { flexDirection: 'row', gap: 6 },
  dot: { width: 8, height: 8, borderRadius: 4 },
});
