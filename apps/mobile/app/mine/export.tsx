// 6.4 导出题库：按专业课选择；导出内容可勾选（题目和参考答案、知识点卡片、错题和我的作答、AI 出的变式题默认不勾）；格式 PDF 或 Word；
// 显示预计页数；生成后保存到手机或分享到微信；只包含用户自己的内容。
import type { Schemas } from '@training/api-client';
import { radius, semantic, spacing } from '@training/ui-tokens';
import { useMutation, useQuery } from '@tanstack/react-query';
import { File, Paths } from 'expo-file-system';
import { router } from 'expo-router';
import * as Sharing from 'expo-sharing';
import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { AIGenerating, Button, Card, EmptyState, ErrorState, Loading, Screen, Text, toast } from '@/components';
import { Checkbox, PageHeader } from '@/features/import/ui';
import { mineKeys, useSubjects } from '@/features/mine/api';
import { api, unwrap } from '@/lib/api';
import { track } from '@/lib/analytics';

type Options = Schemas['ExportOptions'];
type Format = 'pdf' | 'docx';

const items: { key: keyof Options; label: string; desc: (n: number) => string }[] = [
  { key: 'questions', label: '题目和参考答案', desc: (n) => `${n} 题，按题型排，附采分点` },
  { key: 'kps', label: '知识点卡片', desc: (n) => `${n} 个，含原文表述` },
  { key: 'wrong', label: '错题和我的作答', desc: (n) => `${n} 题，附失分原因` },
  { key: 'ai_variants', label: 'AI 出的变式题', desc: (n) => `${n} 题，会标「AI 出题」` },
];

/** 下载到缓存目录再交给系统分享面板（保存到手机或发到微信）。 */
async function saveOrShare(job: Schemas['ExportJob']) {
  if (!job.download_url) return;
  const target = new File(Paths.cache, job.file_name);
  if (target.exists) target.delete();
  const file = await File.downloadFileAsync(job.download_url, target);
  if (await Sharing.isAvailableAsync()) {
    await Sharing.shareAsync(file.uri, {
      mimeType: job.format === 'pdf' ? 'application/pdf' : 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
      dialogTitle: job.file_name,
    });
  } else {
    toast('已保存到 App 缓存目录');
  }
}

export default function ExportPage() {
  const subjects = useSubjects();
  const list = subjects.data?.items ?? [];
  const [picked, setPicked] = useState<number>();
  const sid = picked ?? list[0]?.id ?? 0;
  const [opts, setOpts] = useState<Options>({ questions: true, kps: true, wrong: true, ai_variants: false });
  const [format, setFormat] = useState<Format>('pdf');
  const [jobId, setJobId] = useState<number>();
  const preview = useQuery({
    queryKey: mineKeys.exportPreview(sid),
    queryFn: () => unwrap(api.GET('/subjects/{subjectId}/export-preview', { params: { path: { subjectId: sid } } })),
    enabled: sid > 0,
  });
  const job = useQuery({
    queryKey: mineKeys.export(jobId ?? 0),
    queryFn: () => unwrap(api.GET('/exports/{exportId}', { params: { path: { exportId: jobId! } } })),
    enabled: !!jobId,
    refetchInterval: (q) => (q.state.data && (q.state.data.status === 'queued' || q.state.data.status === 'running') ? 2000 : false),
  });
  const create = useMutation({
    mutationFn: () => unwrap(api.POST('/exports', { body: { subject_id: sid, options: opts, format } })),
    onSuccess: (j) => {
      track('export', { format });
      setJobId(j.id);
    },
    onError: (e) => toast(e instanceof Error ? e.message : '生成失败，请重试'),
  });
  const share = useMutation({ mutationFn: saveOrShare, onError: () => toast('保存失败，请重试') });

  if (subjects.isLoading) return <Screen><Loading rows={5} /></Screen>;
  if (subjects.isError) return <Screen><ErrorState error={subjects.error} onRetry={() => void subjects.refetch()} /></Screen>;
  if (list.length === 0) {
    return (
      <Screen>
        <PageHeader title="导出题库" onBack={() => router.back()} />
        <EmptyState title="还没有专业课" desc="添加专业课并导入资料后可以导出" actionText="去导入" onAction={() => router.push('/import')} />
      </Screen>
    );
  }
  const p = preview.data;
  const pages = p ? items.reduce((sum, it) => sum + (opts[it.key] ? p[it.key].pages : 0), 0) : 0;
  const j = job.data;
  const generating = create.isPending || (j && (j.status === 'queued' || j.status === 'running'));
  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        <PageHeader title="导出题库" onBack={() => router.back()} />
        <Text variant="caption">把题库整理成文档，打印出来背或者存一份备份。只包含你自己的资料和作答记录。</Text>
        <View style={styles.tabs}>
          {list.map((s) => (
            <Pressable
              key={s.id}
              accessibilityRole="tab"
              accessibilityState={{ selected: s.id === sid }}
              onPress={() => {
                setPicked(s.id);
                setJobId(undefined);
              }}
              style={[styles.tab, s.id === sid && styles.tabOn]}
            >
              <Text variant="caption" color={s.id === sid ? semantic.textOnBrand : undefined}>
                {s.code ? `${s.code} ` : ''}
                {s.name}
              </Text>
            </Pressable>
          ))}
        </View>
        <Card style={styles.gap}>
          <Text variant="bodyStrong">导出内容</Text>
          {preview.isLoading ? <Loading rows={3} /> : null}
          {p
            ? items.map((it) => (
                <View key={it.key} style={styles.item}>
                  <Checkbox checked={opts[it.key]} onChange={(v) => setOpts((o) => ({ ...o, [it.key]: v }))} label={it.label} />
                  <Text variant="small" style={styles.itemDesc}>
                    {it.desc(p[it.key].count)}
                  </Text>
                </View>
              ))
            : null}
        </Card>
        <Card style={styles.gap}>
          <Text variant="bodyStrong">格式</Text>
          <View style={styles.tabs}>
            {(['pdf', 'docx'] as const).map((f) => (
              <Pressable key={f} accessibilityRole="radio" accessibilityState={{ selected: format === f }} onPress={() => setFormat(f)} style={[styles.format, format === f && styles.tabOn]}>
                <Text variant="bodyStrong" color={format === f ? semantic.textOnBrand : undefined}>
                  {f === 'pdf' ? 'PDF' : 'Word'}
                </Text>
              </Pressable>
            ))}
          </View>
          <Text variant="caption">约 {pages} 页 · 生成后保存到手机或发到微信</Text>
        </Card>
        {generating ? <AIGenerating steps={['整理题目与知识点', '排版', '生成文件']} current={1} eta="大约 10 秒" /> : null}
        {j?.status === 'done' ? (
          <Card style={styles.gap}>
            <Text variant="bodyStrong">{j.file_name}</Text>
            <Text variant="caption">约 {j.pages} 页 · 文件 24 小时后删除</Text>
            <Button title="保存到手机或分享" loading={share.isPending} onPress={() => share.mutate(j)} />
          </Card>
        ) : null}
        {j?.status === 'failed' ? (
          <Text variant="caption" color={semantic.danger}>
            生成失败，请重试
          </Text>
        ) : null}
        {j?.status === 'expired' ? <Text variant="caption">文件已过期删除，请重新生成</Text> : null}
      </ScrollView>
      <Button title="生成文档" disabled={!items.some((it) => opts[it.key]) || !!generating} loading={!!generating} onPress={() => create.mutate()} />
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { gap: spacing.md, paddingBottom: spacing.xl },
  gap: { gap: spacing.sm },
  tabs: { flexDirection: 'row', flexWrap: 'wrap', gap: spacing.sm },
  tab: { minHeight: 36, justifyContent: 'center', paddingHorizontal: spacing.md, borderRadius: radius.lg, borderWidth: 1, borderColor: semantic.border },
  tabOn: { backgroundColor: semantic.primary, borderColor: semantic.primary },
  format: { flex: 1, minHeight: 44, alignItems: 'center', justifyContent: 'center', borderRadius: radius.md, borderWidth: 1, borderColor: semantic.border },
  item: { gap: 2 },
  itemDesc: { marginLeft: 32 },
});
