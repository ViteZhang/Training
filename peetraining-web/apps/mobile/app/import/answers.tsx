// 1.7c 补充答案文件（PRD v3 1.7c、11.16，10 月 26 日更新）：选择或拍摄一份答案（文件、相册、拍照，规则同 1.5，计入解析额度）；
// AI 先按年份 + 题号把答案配到缺答案的题上，对不上的再按题干配；配对结果逐条显示：按题号配上 / 按题干配上 · 请核对 / 仍缺答案；
// 可改错配的、取消某条；「确认配对 N 题」后这些题的答案来源为「来自你的资料」，采分点由 AI 从答案中提取，在 1.7b 核对。
// 入口：1.7 说明卡（带 job，配这次导入里缺答案的题）、3.3b、3.1b 缺答案筛选（只带 subject，配题库里缺答案的题）。
import type { Schemas } from '@training/api-client';
import { ApiError } from '@training/api-client';
import { colors, radius, semantic, spacing } from '@training/ui-tokens';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import * as DocumentPicker from 'expo-document-picker';
import * as ImagePicker from 'expo-image-picker';
import { router, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { AIGenerating, BottomSheet, Button, Card, EmptyState, ErrorState, Icon, Loading, QuotaSheet, Screen, Text, toast } from '@/components';
import { qtypeNames, sourceText } from '@/features/import/api';
import { checkLimits, formatOf, limits, uploadAll, UnsupportedFile, type Picked } from '@/features/import/files';
import { Checkbox, FileRow, PageHeader } from '@/features/import/ui';
import { Footer } from '@/features/onboarding/ui';
import { useUpgradeActions } from '@/features/membership/purchase';
import { usePermissionPrompt } from '@/features/permissions/PermissionPrompt';
import { api, unwrap } from '@/lib/api';
import { track } from '@/lib/analytics';

let seq = 0;
const nextKey = () => `a${Date.now()}-${seq++}`;

type Pairing = Schemas['AnswerPairing'];
type Target = Schemas['PairingTarget'];

const methodLabel: Record<Pairing['method'], string> = { number: '按题号配上', stem: '按题干配上 · 请核对', manual: '已改配' };

function targetLabel(t: Target) {
  if (t.exam_year && t.question_no) return `${t.exam_year} 年 · 第 ${t.question_no} 题`;
  if (t.exam_year) return `${t.exam_year} 年真题`;
  return t.question_no ? `习题 · 第 ${t.question_no} 题` : qtypeNames[t.qtype];
}

export default function AnswerFileScreen() {
  const params = useLocalSearchParams<{ subject: string; job?: string; answerJob?: string }>();
  const subjectId = Number(params.subject);
  const targetJob = params.job ? Number(params.job) : undefined;
  const qc = useQueryClient();
  const [answerJob, setAnswerJob] = useState<number | undefined>(params.answerJob ? Number(params.answerJob) : undefined);
  const [files, setFiles] = useState<Picked[]>([]);
  const [agreed, setAgreed] = useState(true);
  const [busy, setBusy] = useState(false);
  const [quota, setQuota] = useState(false);
  const [editing, setEditing] = useState<Pairing>();
  const upgrade = useUpgradeActions(() => setQuota(false));

  const result = useQuery({
    queryKey: ['answer-pairings', answerJob],
    queryFn: () => unwrap(api.GET('/import-jobs/{jobId}/pairings', { params: { path: { jobId: answerJob! } } })),
    enabled: !!answerJob,
    // 解析中每 2 秒刷新一次
    refetchInterval: (q) => (q.state.data && ['queued', 'running'].includes(q.state.data.job.status) ? 2000 : false),
  });

  const update = (key: string, patch: Partial<Picked>) => setFiles((cur) => cur.map((f) => (f.key === key ? { ...f, ...patch } : f)));
  const addPicked = (items: Omit<Picked, 'key' | 'status' | 'progress'>[]) => {
    const err = checkLimits(files, items);
    if (err) {
      toast(err);
      return;
    }
    setFiles((cur) => [...cur, ...items.map((i) => ({ ...i, key: nextKey(), status: 'pending' as const, progress: 0 }))]);
  };
  const pickDocs = async () => {
    const res = await DocumentPicker.getDocumentAsync({
      multiple: true,
      copyToCacheDirectory: true,
      type: ['application/pdf', 'application/vnd.openxmlformats-officedocument.wordprocessingml.document', 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet'],
    });
    if (res.canceled) return;
    try {
      addPicked(res.assets.map((a) => ({ name: a.name, uri: a.uri, size: a.size ?? 0, mimeType: a.mimeType, format: formatOf(a.name, a.mimeType) })));
    } catch (e) {
      toast(e instanceof UnsupportedFile ? e.message : '选文件出了点问题，请重试');
    }
  };
  const fromAssets = (assets: ImagePicker.ImagePickerAsset[], prefix: string) =>
    assets.map((a, i) => ({ name: a.fileName ?? `${prefix} ${files.length + i + 1}.jpg`, uri: a.uri, size: a.fileSize ?? 0, mimeType: a.mimeType ?? 'image/jpeg', format: 'image' as const }));
  const pickAlbum = async () => {
    const res = await ImagePicker.launchImageLibraryAsync({ mediaTypes: ['images'], allowsMultipleSelection: true, selectionLimit: limits.maxImages, quality: 0.85 });
    if (!res.canceled) addPicked(fromAssets(res.assets, '答案'));
  };
  const album = usePermissionPrompt('photos', async () => {
    const r = await ImagePicker.requestMediaLibraryPermissionsAsync();
    return { granted: r.granted, canAskAgain: r.canAskAgain };
  });
  const camera = usePermissionPrompt(
    'camera',
    async () => {
      const r = await ImagePicker.requestCameraPermissionsAsync();
      return { granted: r.granted, canAskAgain: r.canAskAgain };
    },
    { label: '从相册选', onPress: () => void pickAlbum() },
  );
  const takePhoto = async () => {
    if (!(await camera.ensure())) return;
    const res = await ImagePicker.launchCameraAsync({ mediaTypes: ['images'], quality: 0.85 });
    if (!res.canceled) addPicked(fromAssets(res.assets, '答案'));
  };

  const start = async () => {
    setBusy(true);
    try {
      const ids = await uploadAll(subjectId, 'question', files, update);
      const job = await unwrap(api.POST('/subjects/{subjectId}/answer-files', { params: { path: { subjectId } }, body: { material_ids: ids, target_job_id: targetJob } }));
      track('import_start', { mode: 'answers', files: ids.length });
      setAnswerJob(job.id);
      router.setParams({ answerJob: String(job.id) });
    } catch (e) {
      if (e instanceof ApiError && e.isQuotaExceeded) setQuota(true);
      else toast(e instanceof ApiError ? e.message : '上传没成功，请检查网络后重试');
    } finally {
      setBusy(false);
    }
  };

  const confirm = async (n: number) => {
    if (!answerJob) return;
    setBusy(true);
    try {
      const r = await unwrap(api.POST('/import-jobs/{jobId}/pairings/confirm', { params: { path: { jobId: answerJob } } }));
      track('answer_file_import', { paired: r.applied });
      await Promise.all([
        qc.invalidateQueries({ queryKey: ['import-items'] }),
        qc.invalidateQueries({ queryKey: ['import-job'] }),
        qc.invalidateQueries({ queryKey: ['bank'] }),
        qc.invalidateQueries({ queryKey: ['question'] }),
      ]);
      toast(r.applied === n ? `已配上 ${r.applied} 题的答案` : `已配上 ${r.applied} 题`);
      router.back();
    } catch (e) {
      toast(e instanceof ApiError ? e.message : '确认没成功，请重试');
    } finally {
      setBusy(false);
    }
  };

  const header = (
    <PageHeader title="把答案单独导进来" desc="真题汇编的答案在书后面、或者答案是另一个文件，都可以。AI 按年份和题号对应到题目上，对不上的再按题干匹配" onBack={() => router.back()} />
  );

  // 第一步：选答案文件
  if (!answerJob) {
    const sources = [
      { key: 'file', title: '文件', desc: 'Word · PDF · Excel', icon: 'file' as const, onPress: () => void pickDocs() },
      { key: 'album', title: '相册', desc: '截图、照片', icon: 'image' as const, onPress: () => void album.ensure().then(async (ok) => ok && (await pickAlbum())) },
      { key: 'camera', title: '拍照', desc: '书后的答案页', icon: 'camera' as const, onPress: () => void takePhoto() },
    ];
    return (
      <Screen>
        <ScrollView contentContainerStyle={styles.scroll}>
          {header}
          <View style={styles.grid}>
            {sources.map((s) => (
              <Pressable key={s.key} accessibilityRole="button" accessibilityLabel={s.title} onPress={s.onPress} disabled={busy} style={styles.source}>
                <View style={styles.sourceIcon}>
                  <Icon name={s.icon} size={18} />
                </View>
                <View style={styles.flex}>
                  <Text variant="caption" color={semantic.textPrimary} style={styles.bold}>
                    {s.title}
                  </Text>
                  <Text variant="small">{s.desc}</Text>
                </View>
              </Pressable>
            ))}
          </View>
          {files.length > 0 ? (
            <Card style={styles.list}>
              {files.map((f) => (
                <FileRow key={f.key} file={f} onRemove={busy ? undefined : () => setFiles((cur) => cur.filter((x) => x.key !== f.key))} />
              ))}
            </Card>
          ) : null}
          <Text variant="small" style={styles.lh}>
            计入资料解析额度；配上的答案标「来自你的资料」
          </Text>
          <Checkbox checked={agreed} onChange={setAgreed} label="我确认对这些资料有合法的使用权，仅用于本人学习" />
        </ScrollView>
        <Footer>
          <Button title="开始配对" disabled={files.length === 0 || !agreed} loading={busy} onPress={() => void start()} />
        </Footer>
        {album.element}
        {camera.element}
        <QuotaSheet
          visible={quota}
          onClose={() => setQuota(false)}
          title="资料解析额度不够了"
          desc="开通会员每月可解析 1000 页。"
          upgrade={upgrade}
          freeDesc="资料解析额度不够了，可以少选几页，或做到时让 AI 给参考答案。"
          freeOptions={[{ label: '少选几页', onPress: () => setQuota(false) }]}
        />
      </Screen>
    );
  }

  if (result.isPending) return <Screen><Loading rows={5} /></Screen>;
  if (result.isError) return <Screen><ErrorState error={result.error} onRetry={() => void result.refetch()} /></Screen>;
  const r = result.data;
  const job = r.job;
  if (job.status === 'queued' || job.status === 'running') {
    return (
      <Screen>
        {header}
        <AIGenerating title="正在识别答案并配对" steps={['识别文字', '找出每道题的答案', '按题号和题干配到题目上']} current={1} />
      </Screen>
    );
  }
  if (job.status === 'failed') {
    return (
      <Screen>
        {header}
        <EmptyState title="答案文件没能识别" desc="可以换更清晰的照片或文件再试，失败的页不扣额度" actionText="重新选择" onAction={() => setAnswerJob(undefined)} />
      </Screen>
    );
  }
  const paired = r.pairings.filter((p) => p.answer && p.status === 'proposed');
  const total = paired.length + r.still_missing.length;
  const fileName = job.materials[0]?.file_name;
  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        {header}
        {fileName ? (
          <Card style={styles.fileCard}>
            <View style={styles.flex}>
              <Text variant="caption" color={semantic.textPrimary} style={styles.medium} numberOfLines={1}>
                {fileName}
              </Text>
              <Text variant="small">{job.billed_pages ?? 0} 页 · 已解析</Text>
            </View>
            <Button title="更换" kind="text" size="sm" onPress={() => setAnswerJob(undefined)} />
          </Card>
        ) : null}
        <Text variant="caption" color={semantic.textPrimary} style={styles.bold}>
          配对结果 · {total} 题里配上 {paired.length} 题
        </Text>
        {r.pairings.map((p) => (
          <Pressable key={p.id} accessibilityRole="button" onPress={() => setEditing(p)} style={[styles.row, p.method === 'stem' && styles.rowCheck]}>
            <View style={styles.rowHead}>
              <Text variant="small" style={styles.flex}>
                {targetLabel(p.target)}
              </Text>
              <Text variant="small" color={!p.answer ? semantic.textSecondary : p.method === 'stem' ? colors.orange : colors.green} style={styles.medium}>
                {!p.answer ? '已取消' : methodLabel[p.method]}
              </Text>
            </View>
            <Text variant="body" numberOfLines={2}>
              {p.target.stem}
            </Text>
            {p.answer ? <Text variant="small">{sourceText(p.answer.source)?.replace(/^.* · /, '答案') ?? '来自答案文件'}</Text> : null}
          </Pressable>
        ))}
        {r.still_missing.map((t) => (
          <View key={`${t.kind}-${t.id}`} style={styles.row}>
            <View style={styles.rowHead}>
              <Text variant="small" style={styles.flex}>
                {targetLabel(t)}
              </Text>
              <Text variant="small" color={semantic.textSecondary} style={styles.medium}>
                仍缺答案
              </Text>
            </View>
            <Text variant="body" numberOfLines={2}>
              {t.stem}
            </Text>
            <Text variant="small">没找到</Text>
          </View>
        ))}
        <Card tone="fill">
          <Text variant="small" style={styles.lh}>
            配上的答案标「来自你的资料」，采分点由 AI 从答案里提取，在 1.7b 里核对。
            {r.still_missing.length > 0 ? `还缺的 ${r.still_missing.length} 题，做到时由 AI 给参考答案` : ''}
          </Text>
        </Card>
      </ScrollView>
      <Footer>
        <Button title={paired.length > 0 ? `确认配对 ${paired.length} 题` : '没有可以配上的答案'} disabled={paired.length === 0 || job.status === 'confirmed'} loading={busy} onPress={() => void confirm(paired.length)} />
      </Footer>
      {editing ? (
        <PairingSheet
          jobId={job.id}
          pairing={editing}
          spare={r.spare_answers}
          onClose={() => setEditing(undefined)}
          onChanged={() => {
            setEditing(undefined);
            void result.refetch();
          }}
        />
      ) : null}
    </Screen>
  );
}

/** 一条配对：看答案全文，改配成答案文件里的另一份，或取消这一条。 */
function PairingSheet({ jobId, pairing, spare, onClose, onChanged }: { jobId: number; pairing: Pairing; spare: Schemas['PairingAnswer'][]; onClose: () => void; onChanged: () => void }) {
  const [busy, setBusy] = useState(false);
  const save = async (answerId: number | null) => {
    setBusy(true);
    try {
      await unwrap(api.PUT('/import-jobs/{jobId}/pairings/{pairingId}', { params: { path: { jobId, pairingId: pairing.id } }, body: { answer_id: answerId } }));
      onChanged();
    } catch (e) {
      toast(e instanceof ApiError ? e.message : '没改成功，请重试');
    } finally {
      setBusy(false);
    }
  };
  return (
    <BottomSheet visible onClose={onClose} title={targetLabel(pairing.target)}>
      <ScrollView style={styles.sheetBody} contentContainerStyle={styles.gap}>
        <Text variant="body">{pairing.target.stem}</Text>
        {pairing.answer ? (
          <Card tone="fill" style={styles.gap}>
            <Text variant="small">{sourceText(pairing.answer.source) ?? '答案文件'}</Text>
            <Text variant="body">{pairing.answer.text}</Text>
          </Card>
        ) : null}
        {spare.length > 0 ? (
          <>
            <Text variant="caption" color={semantic.textPrimary} style={styles.medium}>
              配错了？换成答案文件里的另一份
            </Text>
            {spare.map((a) => (
              <Pressable key={a.id} accessibilityRole="button" disabled={busy} onPress={() => void save(a.id)} style={styles.spare}>
                <Text variant="small">{a.exam_year && a.question_no ? `${a.exam_year} 年 · 第 ${a.question_no} 题` : a.question_no ? `第 ${a.question_no} 题` : '答案'}</Text>
                <Text variant="caption" numberOfLines={2}>
                  {a.text}
                </Text>
              </Pressable>
            ))}
          </>
        ) : null}
      </ScrollView>
      {pairing.answer ? <Button title="取消这一条" kind="secondary" loading={busy} onPress={() => void save(null)} style={styles.sheetButton} /> : null}
    </BottomSheet>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing.xl, gap: 12 },
  grid: { flexDirection: 'row', flexWrap: 'wrap', gap: 10 },
  source: { flexBasis: '30%', flexGrow: 1, flexDirection: 'row', alignItems: 'center', gap: 10, minHeight: 60, paddingHorizontal: 12, paddingVertical: 10, borderRadius: radius.xl, backgroundColor: semantic.surface, borderWidth: 1, borderColor: semantic.border },
  sourceIcon: { width: 34, height: 34, borderRadius: 10, backgroundColor: semantic.fill, alignItems: 'center', justifyContent: 'center' },
  list: { paddingVertical: 4 },
  flex: { flex: 1 },
  bold: { fontWeight: '700' },
  medium: { fontWeight: '500' },
  lh: { lineHeight: 19 },
  gap: { gap: 10 },
  fileCard: { flexDirection: 'row', alignItems: 'center', gap: 10 },
  row: { gap: 6, paddingVertical: 14, paddingHorizontal: 16, borderRadius: radius.xl, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface },
  rowCheck: { borderColor: colors.orangeLine, backgroundColor: semantic.peachSoft },
  rowHead: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  spare: { gap: 2, padding: 12, borderRadius: radius.lg, borderWidth: 1, borderColor: semantic.border },
  sheetBody: { maxHeight: 420 },
  sheetButton: { marginTop: spacing.md },
});
