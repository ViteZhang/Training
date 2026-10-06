// 3.4 知识点卡片：掌握状态与掌握分、真题出现次数、原文表述（采分关键词下划线）与出处、采分点、AI 解读（标「AI 生成」）、
// 相关题目、三档自评（只作参考）、「来一题检验」。3.5 更多操作：编辑、调整归属、合并、拆分、重新生成解读、删除。
import { ApiError, type Schemas } from '@training/api-client';
import { layout, radius, semantic, spacing } from '@training/ui-tokens';
import { useQueryClient } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, TextInput, View } from 'react-native';
import { ActionList, BottomSheet, Button, Card, ConfirmDialog, ErrorState, InfoCard, Loading, NavBar, RubricLine, Screen, Tag, Text, toast } from '@/components';
import { MasteryPill } from '@/features/bank/MasteryPill';
import { bankKeys, relationNames, sourceLabel, useKP, type KPDetail } from '@/features/bank/api';
import { qtypeNames } from '@/features/import/api';
import { Segments } from '@/features/import/ui';
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

type Sheet = 'more' | 'merge' | 'split' | 'move' | 'delete' | 'relate' | null;

function MoreActions({ kp, sheet, setSheet }: { kp: KPDetail; sheet: Sheet; setSheet: (s: Sheet) => void }) {
  const qc = useQueryClient();
  const subjectId = useLocalSearchParams<{ subjectId?: string }>().subjectId;
  const [target, setTarget] = useState('');
  const [parts, setParts] = useState(['', '']);
  const [relType, setRelType] = useState<Schemas['RelationType']>('contrast');
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
      <BottomSheet visible={sheet === 'more'} onClose={() => setSheet(null)}>
        <ActionList
          items={[
            { label: '编辑内容', icon: 'edit', onPress: () => { setSheet(null); router.push({ pathname: '/bank/kp/edit', params: { id: String(kp.id), subjectId: subjectId ?? '' } }); } },
            { label: '调整归属', icon: 'folder', onPress: () => { setSheet(null); router.push({ pathname: '/bank/kp/edit', params: { id: String(kp.id), subjectId: subjectId ?? '' } }); } },
            { label: '添加关联', icon: 'link', onPress: () => setSheet('relate') },
            { label: '合并到其他知识点', icon: 'merge', onPress: () => setSheet('merge') },
            { label: '拆分为多个知识点', icon: 'split', onPress: () => setSheet('split') },
            {
              label: 'AI 解读不准，重新生成',
              icon: 'refresh',
              onPress: () => {
                setSheet(null);
                void run(async () => qc.setQueryData(bankKeys.kp(kp.id), await unwrap(api.POST('/knowledge-points/{kpId}/explanation', path))), '已重新生成');
              },
            },
            { label: '删除这个知识点', icon: 'trash', danger: true, onPress: () => setSheet('delete') },
          ]}
        />
        <Button title="取消" kind="soft" style={styles.cancel} onPress={() => setSheet(null)} />
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
      <BottomSheet visible={sheet === 'relate'} onClose={() => setSheet(null)} title="添加关联">
        <Segments<Schemas['RelationType']>
          value={relType}
          onChange={setRelType}
          options={(['contrast', 'component', 'sibling', 'related'] as const).map((k) => ({ key: k, label: relationNames[k] }))}
        />
        <MergePicker kp={kp} query={target} setQuery={setTarget} onPick={(id) =>
          void run(() => unwrap(api.POST('/knowledge-points/{kpId}/relations', { ...path, body: { target_id: id, relation_type: relType } })), '已添加关联').then((ok) => {
            if (ok) setSheet(null);
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
        <NavBar
          title={k.path.slice(0, 2).join(' · ')}
          right={
            <Pressable accessibilityRole="button" accessibilityLabel="更多操作" onPress={() => setSheet('more')} style={styles.more}>
              <View style={styles.d} />
              <View style={styles.d} />
              <View style={styles.d} />
            </Pressable>
          }
        />
        <Text variant="h1" style={styles.name}>
          {k.name}
        </Text>
        <View style={styles.row}>
          <MasteryPill state={k.mastery.state} suffix={` · ${Math.round(k.mastery.m)}`} />
          {k.exam_count > 0 ? (
            <View style={styles.line}>
              <Text variant="small" color={semantic.textPrimary}>
                真题出现 {k.exam_count} 次
              </Text>
            </View>
          ) : null}
          {k.needs_review ? <Tag label="待核对" tone="danger" /> : null}
        </View>

        {k.original_text ? (
          <InfoCard label="原文表述">
            <Underlined text={k.original_text} keywords={keywords} />
            {k.source ? (
              <View style={styles.sourceRow}>
                <Text variant="small" style={styles.flex}>
                  出自：{k.source.file_name}
                  {k.source.page ? ` · 第 ${k.source.page} 页` : ''}
                </Text>
                {k.source.page ? (
                  <Button
                    title="查看原文 ›"
                    kind="text"
                    size="sm"
                    color={semantic.textPrimary}
                    style={styles.link}
                    onPress={() => router.push({ pathname: '/bank/page', params: { materialId: String(k.source!.material_id), page: String(k.source!.page), highlight: k.original_text ?? '' } })}
                  />
                ) : null}
              </View>
            ) : null}
          </InfoCard>
        ) : null}

        {k.rubric_points.length > 0 ? (
          <InfoCard label="采分点">
            {k.rubric_points.map((r, i) => (
              <RubricLine key={r.id} index={i} content={r.content} />
            ))}
          </InfoCard>
        ) : null}

        <InfoCard label="AI 帮你理解" tone="fill" right={<Tag label="AI 生成" tone="ai" />}>
          <Text variant="caption" color={semantic.textPrimary} style={styles.lh}>
            {k.ai_explanation ?? 'AI 解读暂时没生成出来，可以在「更多」里重新生成'}
          </Text>
        </InfoCard>

        {k.related_questions.length > 0 ? (
          <>
            <View style={styles.sectionHead}>
              <Text variant="caption" color={semantic.textPrimary} style={[styles.flex, styles.medium]}>
                相关题目 · {k.related_questions.length}
              </Text>
              <Text variant="small">来自你的题库</Text>
            </View>
            <Card style={styles.list}>
              {k.related_questions.map((q, i) => (
                <Pressable key={q.id} accessibilityRole="button" onPress={() => router.push({ pathname: '/bank/question/[id]', params: { id: String(q.id) } })} style={[styles.related, i > 0 && styles.divider]}>
                  <View style={[styles.flex, styles.gap2]}>
                    <Text variant="body" numberOfLines={1}>
                      {qtypeNames[q.qtype]}：{q.stem}
                    </Text>
                    <Text variant="small">{sourceLabel(q.source, q.exam_year)}</Text>
                  </View>
                  {q.source === 'ai_generated' ? <Tag label="AI 出题" tone="ai" /> : q.source === 'exam' ? <Tag label="真题" tone="mastered" /> : null}
                </Pressable>
              ))}
            </Card>
          </>
        ) : null}

        <View style={styles.assessRow}>
          <Text variant="small">自评</Text>
          {assess.map((a) => {
            const on = k.mastery.last_self_assess === a.key;
            return (
              <Pressable key={a.key} accessibilityRole="radio" accessibilityState={{ selected: on }} onPress={() => void selfAssess(a.key)} style={[styles.assess, on && styles.assessOn]}>
                <Text variant="caption" color={on ? semantic.textOnBrand : semantic.textPrimary}>
                  {a.label}
                </Text>
              </Pressable>
            );
          })}
        </View>
        <View style={styles.note}>
          <Text variant="small" color={semantic.info}>
            自评只作参考：在两个不同日期答对相关题目后，才会标记为「已掌握」
          </Text>
        </View>
        <Button title="来一题检验" onPress={() => toast('检验练习在训练模块上线后开放')} />
      </ScrollView>
      <MoreActions kp={k} sheet={sheet} setSheet={setSheet} />
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing.xl, gap: 12 },
  gap: { gap: spacing.sm },
  gap2: { gap: 2 },
  name: { marginTop: spacing.sm },
  row: { flexDirection: 'row', alignItems: 'center', gap: 6, flexWrap: 'wrap' },
  line: { paddingHorizontal: 10, paddingVertical: 3, borderRadius: radius.pill, borderWidth: 1, borderColor: semantic.border },
  more: { width: 44, height: 44, flexDirection: 'row', gap: 3, alignItems: 'center', justifyContent: 'center', marginRight: -10 },
  d: { width: 4, height: 4, borderRadius: 2, backgroundColor: semantic.textPrimary },
  sourceRow: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  link: { paddingHorizontal: 0, minHeight: 32 },
  lh: { lineHeight: 23 },
  medium: { fontWeight: '500' },
  sectionHead: { flexDirection: 'row', alignItems: 'center', marginTop: 4 },
  list: { paddingVertical: 0 },
  divider: { borderTopWidth: 1, borderTopColor: semantic.border },
  flex: { flex: 1 },
  kw: { textDecorationLine: 'underline', textDecorationColor: semantic.progress, fontWeight: '700' },
  related: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, minHeight: 64, paddingVertical: 10 },
  assessRow: { flexDirection: 'row', alignItems: 'center', gap: 8, marginTop: 4 },
  assess: { flex: 1, minHeight: 40, alignItems: 'center', justifyContent: 'center', borderRadius: radius.pill, backgroundColor: semantic.fill },
  assessOn: { backgroundColor: semantic.primary },
  note: { padding: 10, borderRadius: radius.md, backgroundColor: semantic.infoSoft },
  menu: { gap: spacing.xs },
  cancel: { marginTop: spacing.md },
  input: { minHeight: 44, borderWidth: 1, borderColor: semantic.border, borderRadius: radius.md, paddingHorizontal: spacing.md, fontSize: 16, color: semantic.textPrimary, backgroundColor: semantic.surface },
});
