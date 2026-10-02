// 3.4 知识点卡片：掌握状态与掌握分、真题出现次数、原文表述（采分关键词下划线）与出处、采分点、AI 解读（标「AI 生成」）、
// 相关题目、三档自评（只作参考）、「来一题检验」。3.5 更多操作：编辑、调整归属、合并、拆分、重新生成解读、删除。
import { ApiError, type Schemas } from '@training/api-client';
import { layout, radius, semantic, spacing } from '@training/ui-tokens';
import { useQueryClient } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, TextInput, View } from 'react-native';
import { BottomSheet, Button, Card, ConfirmDialog, ErrorState, Loading, Screen, Tag, Text, toast } from '@/components';
import { bankKeys, sourceLabel, stateNames, stateTone, useKP, type KPDetail } from '@/features/bank/api';
import { qtypeNames } from '@/features/import/api';
import { PageHeader } from '@/features/import/ui';
import { api, unwrap } from '@/lib/api';

const assess: { key: Schemas['SelfAssessLevel']; label: string }[] = [
  { key: 'unknown', label: '不会' },
  { key: 'vague', label: '模糊' },
  { key: 'mastered', label: '掌握' },
];

/** 原文表述里的采分关键词加下划线。 */
function Underlined({ text, keywords }: { text: string; keywords: string[] }) {
  const parts: { t: string; k: boolean }[] = [];
  let rest = text;
  while (rest) {
    let best = -1;
    let kw = '';
    for (const k of keywords) {
      const i = k ? rest.indexOf(k) : -1;
      if (i >= 0 && (best < 0 || i < best)) {
        best = i;
        kw = k;
      }
    }
    if (best < 0) {
      parts.push({ t: rest, k: false });
      break;
    }
    if (best > 0) parts.push({ t: rest.slice(0, best), k: false });
    parts.push({ t: kw, k: true });
    rest = rest.slice(best + kw.length);
  }
  return (
    <Text variant="body">
      {parts.map((p, i) => (
        <Text key={i} variant="body" style={p.k ? styles.kw : undefined}>
          {p.t}
        </Text>
      ))}
    </Text>
  );
}

type Sheet = 'more' | 'merge' | 'split' | 'move' | 'delete' | null;

function MoreActions({ kp, sheet, setSheet }: { kp: KPDetail; sheet: Sheet; setSheet: (s: Sheet) => void }) {
  const qc = useQueryClient();
  const subjectId = useLocalSearchParams<{ subjectId?: string }>().subjectId;
  const [target, setTarget] = useState('');
  const [parts, setParts] = useState(['', '']);
  const [busy, setBusy] = useState(false);
  const refresh = () => qc.invalidateQueries({ queryKey: ['bank'] });
  const run = async (fn: () => Promise<unknown>, done: string) => {
    setBusy(true);
    try {
      await fn();
      await refresh();
      toast(done);
      return true;
    } catch (e) {
      toast(e instanceof ApiError ? e.message : '操作没成功，请重试');
      return false;
    } finally {
      setBusy(false);
    }
  };
  const path = { params: { path: { kpId: kp.id } } };
  return (
    <>
      <BottomSheet visible={sheet === 'more'} onClose={() => setSheet(null)} title={kp.name}>
        <View style={styles.menu}>
          <Button title="编辑内容" kind="text" onPress={() => { setSheet(null); router.push({ pathname: '/bank/kp/edit', params: { id: String(kp.id), subjectId: subjectId ?? '' } }); }} />
          <Button title="调整归属" kind="text" onPress={() => { setSheet(null); router.push({ pathname: '/bank/kp/edit', params: { id: String(kp.id), subjectId: subjectId ?? '' } }); }} />
          <Button title="合并到其他知识点" kind="text" onPress={() => setSheet('merge')} />
          <Button title="拆分为多个知识点" kind="text" onPress={() => setSheet('split')} />
          <Button
            title="AI 解读不准，重新生成"
            kind="text"
            onPress={() => {
              setSheet(null);
              void run(async () => qc.setQueryData(bankKeys.kp(kp.id), await unwrap(api.POST('/knowledge-points/{kpId}/explanation', path))), '已重新生成');
            }}
          />
          <Button title="删除这个知识点" kind="danger" onPress={() => setSheet('delete')} />
        </View>
      </BottomSheet>
      <BottomSheet visible={sheet === 'merge'} onClose={() => setSheet(null)} title="合并到哪个知识点">
        <Text variant="caption">在题库里搜索目标知识点的名称；题目、掌握度和作答记录会一起迁过去</Text>
        <MergePicker kp={kp} query={target} setQuery={setTarget} onPick={(id) =>
          void run(() => unwrap(api.POST('/knowledge-points/{kpId}/merge', { ...path, body: { target_id: id } })), '已合并').then((ok) => {
            if (ok) {
              setSheet(null);
              router.replace({ pathname: '/bank/kp/[id]', params: { id: String(id), subjectId: subjectId ?? '' } });
            }
          })
        } />
      </BottomSheet>
      <BottomSheet visible={sheet === 'split'} onClose={() => setSheet(null)} title="拆分为多个知识点">
        {parts.map((p, i) => (
          <TextInput key={i} accessibilityLabel={`第 ${i + 1} 个知识点`} placeholder={`第 ${i + 1} 个知识点的名称`} value={p} onChangeText={(v) => setParts(parts.map((x, j) => (j === i ? v : x)))} style={styles.input} maxFontSizeMultiplier={layout.maxFontScale} />
        ))}
        <Button title="再加一个" kind="text" onPress={() => setParts([...parts, ''])} />
        <Button
          title="拆分"
          disabled={parts.filter((p) => p.trim()).length < 2}
          loading={busy}
          onPress={() =>
            void run(() => unwrap(api.POST('/knowledge-points/{kpId}/split', { ...path, body: { parts: parts.filter((p) => p.trim()).map((name) => ({ name: name.trim() })) } })), '已拆分').then((ok) => {
              if (ok) {
                setSheet(null);
                router.back();
              }
            })
          }
        />
      </BottomSheet>
      <ConfirmDialog
        visible={sheet === 'delete'}
        title="删除这个知识点？"
        message="题目会保留，只是不再归到这个知识点下；它的掌握度记录会删除。"
        confirmText="删除"
        danger
        onCancel={() => setSheet(null)}
        onConfirm={() => {
          setSheet(null);
          void run(() => unwrap(api.DELETE('/knowledge-points/{kpId}', path)), '已删除').then((ok) => ok && router.back());
        }}
      />
    </>
  );
}

function MergePicker({ kp, query, setQuery, onPick }: { kp: KPDetail; query: string; setQuery: (s: string) => void; onPick: (id: number) => void }) {
  const [results, setResults] = useState<{ id: number; name: string; path?: string[] }[]>([]);
  const subjectId = useLocalSearchParams<{ subjectId?: string }>().subjectId;
  const search = async () => {
    if (!query.trim() || !subjectId) return;
    try {
      const r = await unwrap(api.GET('/subjects/{subjectId}/search', { params: { path: { subjectId: Number(subjectId) }, query: { q: query.trim() } } }));
      setResults(r.knowledge_points.filter((k) => k.id !== kp.id));
    } catch (e) {
      toast(e instanceof ApiError ? e.message : '搜索没成功');
    }
  };
  return (
    <View style={styles.gap}>
      <TextInput accessibilityLabel="搜索知识点" placeholder="输入知识点名称" value={query} onChangeText={setQuery} onSubmitEditing={() => void search()} returnKeyType="search" style={styles.input} maxFontSizeMultiplier={layout.maxFontScale} />
      {!subjectId ? <Text variant="caption">从题库页进入知识点卡片后可以合并</Text> : null}
      {results.map((r) => (
        <Button key={r.id} title={`${r.name}${r.path?.length ? ` · ${r.path.join(' / ')}` : ''}`} kind="secondary" onPress={() => onPick(r.id)} />
      ))}
    </View>
  );
}

export default function KPCard() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const kpId = Number(id);
  const qc = useQueryClient();
  const kp = useKP(kpId);
  const [sheet, setSheet] = useState<Sheet>(null);

  if (kp.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (kp.isError || !kp.data) return <Screen><ErrorState error={kp.error} onRetry={() => void kp.refetch()} /></Screen>;
  const k = kp.data;
  const keywords = k.rubric_points.flatMap((r) => r.keywords ?? []);

  const selfAssess = async (level: Schemas['SelfAssessLevel']) => {
    try {
      const m = await unwrap(api.PUT('/knowledge-points/{kpId}/self-assessment', { params: { path: { kpId } }, body: { level } }));
      qc.setQueryData(bankKeys.kp(kpId), { ...k, mastery: m });
      void qc.invalidateQueries({ queryKey: ['bank'] });
    } catch (e) {
      toast(e instanceof ApiError ? e.message : '没记录上，请重试');
    }
  };

  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        <PageHeader title={k.name} desc={k.path.join(' · ')} onBack={() => router.back()} right={<Button title="更多" kind="text" onPress={() => setSheet('more')} />} />
        <View style={styles.row}>
          <Tag label={stateNames[k.mastery.state]} tone={stateTone[k.mastery.state]} />
          <Text variant="number">{Math.round(k.mastery.m)}</Text>
          {k.exam_count > 0 ? <Text variant="caption">真题出现 {k.exam_count} 次</Text> : null}
          {k.needs_review ? <Tag label="待核对" tone="danger" /> : null}
        </View>

        {k.original_text ? (
          <Card style={styles.gap}>
            <Text variant="bodyStrong">原文表述</Text>
            <Underlined text={k.original_text} keywords={keywords} />
            {k.source ? (
              <View style={styles.row}>
                <Text variant="caption" style={styles.flex}>
                  出自：{k.source.file_name}
                  {k.source.page ? ` · 第 ${k.source.page} 页` : ''}
                </Text>
                {k.source.page ? (
                  <Button
                    title="查看原文"
                    kind="text"
                    onPress={() => router.push({ pathname: '/bank/page', params: { materialId: String(k.source!.material_id), page: String(k.source!.page), highlight: k.original_text ?? '' } })}
                  />
                ) : null}
              </View>
            ) : null}
          </Card>
        ) : null}

        {k.rubric_points.length > 0 ? (
          <Card style={styles.gap}>
            <Text variant="bodyStrong">采分点</Text>
            {k.rubric_points.map((r, i) => (
              <View key={r.id} style={styles.row}>
                <Text variant="number" color={semantic.textSecondary}>
                  {String(i + 1).padStart(2, '0')}
                </Text>
                <Text variant="body" style={styles.flex}>
                  {r.content}
                </Text>
              </View>
            ))}
          </Card>
        ) : null}

        <Card style={styles.gap}>
          <View style={styles.row}>
            <Text variant="bodyStrong" style={styles.flex}>
              AI 帮你理解
            </Text>
            <Tag label="AI 生成" tone="ai" />
          </View>
          <Text variant="body">{k.ai_explanation ?? 'AI 解读暂时没生成出来，可以在「更多」里重新生成'}</Text>
        </Card>

        {k.related_questions.length > 0 ? (
          <Card style={styles.gap}>
            <Text variant="bodyStrong">相关题目 · {k.related_questions.length}</Text>
            {k.related_questions.map((q) => (
              <Pressable key={q.id} accessibilityRole="button" onPress={() => router.push({ pathname: '/bank/question/[id]', params: { id: String(q.id) } })} style={styles.related}>
                <Text variant="body" numberOfLines={1} style={styles.flex}>
                  {qtypeNames[q.qtype]}：{q.stem}
                </Text>
                <Tag label={sourceLabel(q.source, q.exam_year)} tone={q.source === 'ai_generated' ? 'ai' : 'neutral'} />
              </Pressable>
            ))}
          </Card>
        ) : null}

        <Card style={styles.gap}>
          <Text variant="bodyStrong">自评</Text>
          <View style={styles.row}>
            {assess.map((a) => (
              <Pressable
                key={a.key}
                accessibilityRole="radio"
                accessibilityState={{ selected: k.mastery.last_self_assess === a.key }}
                onPress={() => void selfAssess(a.key)}
                style={[styles.assess, k.mastery.last_self_assess === a.key && styles.assessOn]}
              >
                <Text variant="bodyStrong" color={k.mastery.last_self_assess === a.key ? semantic.textOnBrand : semantic.textPrimary}>
                  {a.label}
                </Text>
              </Pressable>
            ))}
          </View>
          <Text variant="caption">自评只作参考：在两个不同日期答对相关题目后，才会标记为「已掌握」</Text>
        </Card>
        <Button title="来一题检验" onPress={() => toast('检验练习在训练模块上线后开放')} />
      </ScrollView>
      <MoreActions kp={k} sheet={sheet} setSheet={setSheet} />
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing.xl, gap: spacing.md },
  gap: { gap: spacing.sm },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, flexWrap: 'wrap' },
  flex: { flex: 1 },
  kw: { textDecorationLine: 'underline', textDecorationColor: semantic.progress },
  related: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, minHeight: 44 },
  assess: { flex: 1, minHeight: 44, alignItems: 'center', justifyContent: 'center', borderRadius: radius.md, borderWidth: 1, borderColor: semantic.border },
  assessOn: { backgroundColor: semantic.primary, borderColor: semantic.primary },
  menu: { gap: spacing.xs },
  input: { minHeight: 44, borderWidth: 1, borderColor: semantic.border, borderRadius: radius.md, paddingHorizontal: spacing.md, fontSize: 16, color: semantic.textPrimary, backgroundColor: semantic.surface },
});
