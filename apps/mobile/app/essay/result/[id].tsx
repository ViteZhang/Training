// 5.5 作文批改中：三步进度（提取立意与结构、按评分细则打分、生成逐段批注），约 30 秒；可先离开，完成后发消息。
// 5.6 作文批改结果：总分 / 满分、较上篇、所用评分标准（→ 5.9）；各维度得分；Tab：总评（亮点、问题、建议）、逐段批注（原文标出问题句并给修改建议）、
// 范文对比（用户导入的同题范文要点）；「存入作文本」「按建议重写」（记为下一稿）；「有异议」与主观题相同。
import type { Schemas } from '@training/api-client';
import { colors, radius, semantic, spacing } from '@training/ui-tokens';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, TextInput, View } from 'react-native';
import { AIGenerating, BottomSheet, Button, Card, ErrorState, Loading, Screen, Segmented, Text, toast } from '@/components';
import { Checkbox, PageHeader } from '@/features/import/ui';
import { essayKeys, fmtScore, useCreateEssay, useEssay, type Essay } from '@/features/essay/api';
import { api, unwrap } from '@/lib/api';

type Tab = 'summary' | 'annotations' | 'models';

const reasons: { key: 'hit_missed' | 'rubric_wrong' | 'score_unfair' | 'other'; text: string }[] = [
  { key: 'score_unfair', text: '分数给得不合理' },
  { key: 'hit_missed', text: '文中写到了，批改没看到' },
  { key: 'rubric_wrong', text: '评分标准本身不对' },
  { key: 'other', text: '其他' },
];

function DisputeSheet({ e, visible, onClose }: { e: Essay; visible: boolean; onClose: () => void }) {
  const qc = useQueryClient();
  const [reason, setReason] = useState<(typeof reasons)[number]['key']>();
  const [note, setNote] = useState('');
  const submit = useMutation({
    mutationFn: () => unwrap(api.POST('/essays/{essayId}/dispute', { params: { path: { essayId: e.id } }, body: { reason: reason!, note: note || undefined } })),
    onSuccess: (v) => {
      qc.setQueryData(essayKeys.essay(e.id), v);
      onClose();
      toast('已重新批改');
    },
    onError: (err) => toast(err instanceof Error ? err.message : '没提交成功，请重试'),
  });
  return (
    <BottomSheet visible={visible} onClose={onClose} title="对批改有异议？">
      <View style={styles.gap}>
        {reasons.map((r) => (
          <Checkbox key={r.key} checked={reason === r.key} onChange={() => setReason(r.key)} label={r.text} />
        ))}
        {reason === 'rubric_wrong' ? (
          <>
            <Text variant="caption">评分标准来自你的资料，可以直接改；改了之后新写的作文按新标准批改。</Text>
            <Button
              title="去修改评分标准 ›"
              onPress={() => {
                onClose();
                router.push({ pathname: '/essay/rubric', params: { subjectId: String(e.subject_id) } });
              }}
            />
          </>
        ) : (
          <>
            <TextInput accessibilityLabel="补充说明" value={note} onChangeText={setNote} placeholder="补充说明（选填）" placeholderTextColor={semantic.textSecondary} style={styles.note} />
            <Text variant="caption">提交后会重新批改一次，不消耗批改次数，以重批结果为准。每篇只能复核一次。</Text>
            <Button title="提交" disabled={!reason} loading={submit.isPending} onPress={() => submit.mutate()} />
          </>
        )}
      </View>
    </BottomSheet>
  );
}

/** 逐段批注：原文按段显示，有批注的句子标出，下面给问题与修改建议。 */
function Annotated({ e }: { e: Essay }) {
  const paras = e.paragraphs ?? [];
  const notes = e.annotations ?? [];
  if (notes.length === 0) return <Text variant="caption">这篇没有需要逐段修改的地方</Text>;
  return (
    <View style={styles.gap}>
      {paras.map((p, i) => {
        const ns = notes.filter((a) => a.paragraph === i + 1);
        if (ns.length === 0) return null;
        return (
          <View key={i} style={styles.para}>
            <Text variant="small">第 {i + 1} 段</Text>
            {ns.map((a, j) => {
              const at = p.indexOf(a.quote);
              return (
                <View key={j} style={styles.gapSm}>
                  <Text variant="body">
                    {at > 0 ? '……' + p.slice(Math.max(0, at - 20), at) : ''}
                    <Text variant="body" style={styles.mark}>
                      {a.quote}
                    </Text>
                    {at >= 0 ? p.slice(at + a.quote.length, at + a.quote.length + 20) + '……' : ''}
                  </Text>
                  <Text variant="caption" color={semantic.danger}>
                    {a.issue}
                  </Text>
                  <Text variant="caption">{a.suggestion}</Text>
                </View>
              );
            })}
          </View>
        );
      })}
    </View>
  );
}

function Result({ e }: { e: Essay }) {
  const [tab, setTab] = useState<Tab>('summary');
  const [dispute, setDispute] = useState(false);
  const rewrite = useCreateEssay();
  const generic = e.rubric?.source === 'generic';
  const models = e.model_essays ?? [];
  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        <PageHeader title="作文批改" onBack={() => router.back()} right={!e.disputed ? <Button title="有异议" kind="text" size="sm" style={styles.link} onPress={() => setDispute(true)} /> : undefined} />
        <Text variant="small" numberOfLines={1}>
          {e.topic} · {e.word_count} 字{e.draft_no > 1 ? ` · 第 ${e.draft_no} 稿` : ''}
        </Text>
        <View style={styles.headRow}>
          <View style={[styles.rowBase, styles.flex]}>
            <Text variant="score" color={semantic.textPrimary} style={styles.big}>
              {fmtScore(e.score)}
            </Text>
            <Text variant="caption"> / {fmtScore(e.full_score)}</Text>
          </View>
          <View style={styles.right}>
            {e.prev_delta !== undefined ? (
              <View style={[styles.deltaPill, { backgroundColor: e.prev_delta >= 0 ? semantic.masteredSoft : semantic.dangerSoft }]}>
                <Text variant="small" color={e.prev_delta >= 0 ? '#1F6B4A' : semantic.danger}>
                  较上篇 {e.prev_delta >= 0 ? `+${e.prev_delta}` : e.prev_delta}
                </Text>
              </View>
            ) : null}
            <Pressable accessibilityRole="button" onPress={() => router.push({ pathname: '/essay/rubric', params: { subjectId: String(e.subject_id) } })}>
              <Text variant="small">评分标准 ›</Text>
            </Pressable>
          </View>
        </View>
        <Text variant="small">AI 批改得分 · 仅供参考</Text>
        <Pressable accessibilityRole="button" onPress={() => router.push({ pathname: '/essay/rubric', params: { subjectId: String(e.subject_id) } })}>
          <Text variant="small" color={semantic.textPrimary}>
            {generic ? '按通用五维度标准 · 分数只作参考，不计入预估分 ›' : `按你的评分细则 · ${e.counts_for_estimate ? '计入预估分' : '真题限时完成的才计入预估分'} ›`}
          </Text>
        </Pressable>
        {e.disputed && e.score_before !== undefined ? <Text variant="small">已复核：原来 {fmtScore(e.score_before)} 分，以复核结果为准</Text> : null}
        <Card style={styles.gapSm}>
          {(e.dimensions ?? []).map((d) => {
            const weak = d.name === e.weakest_dimension;
            return (
              <View key={d.name} style={styles.dimRow}>
                <Text variant="small" color={semantic.textPrimary} style={styles.dimName} numberOfLines={1}>
                  {d.name}
                </Text>
                <View style={styles.bar}>
                  <View style={[styles.fill, { width: `${(d.max ? d.score / d.max : 0) * 100}%`, backgroundColor: weak ? colors.amber : colors.indigo }]} />
                </View>
                <Text variant="small" style={styles.frac}>
                  {fmtScore(d.score)}/{fmtScore(d.max)}
                </Text>
              </View>
            );
          })}
          {e.weakest_dimension ? (
            <Text variant="small" color="#8A4B12">
              失分主项：{e.weakest_dimension}
            </Text>
          ) : null}
        </Card>

        <Segmented<Tab>
          options={[
            { key: 'summary', label: '总评' },
            { key: 'annotations', label: `逐段批注${e.annotations?.length ? ` ${e.annotations.length}` : ''}` },
            { key: 'models', label: `范文对比${models.length ? ` ${models.length}` : ''}` },
          ]}
          value={tab}
          onChange={setTab}
        />
        {tab === 'summary' ? (
          <View style={styles.gap}>
            {e.thesis ? <Text variant="small">立意：{e.thesis}</Text> : null}
            {[
              { title: '亮点', items: e.highlights ?? [], color: semantic.mastered },
              { title: '问题', items: e.problems ?? [], color: semantic.danger },
              { title: '建议', items: e.suggestions ?? [], color: semantic.textPrimary },
            ].flatMap((g) =>
              g.items.map((t, i) => (
                <View key={`${g.title}${i}`} style={styles.fb}>
                  <Text variant="caption" color={g.color} style={styles.fbLabel}>
                    {g.title}
                  </Text>
                  <Text variant="caption" color={semantic.textPrimary} style={[styles.flex, styles.lh]}>
                    {t}
                  </Text>
                </View>
              )),
            )}
          </View>
        ) : null}
        {tab === 'annotations' ? (
          <Card>
            <Annotated e={e} />
          </Card>
        ) : null}
        {tab === 'models' ? (
          <View style={styles.models}>
            {models.length === 0 ? (
              <Text variant="caption">{e.topic_source === 'exam' ? '这道题还没有你导入的范文' : '只有真题题目会对照你导入的同题范文'}</Text>
            ) : (
              <>
                <Text variant="caption" color="#7A4E00" style={styles.bold}>
                  你导入的同题范文 · {models.length} 篇
                </Text>
                {models.map((m) => (
                  <Pressable key={m.id} accessibilityRole="button" onPress={() => router.push({ pathname: '/essay/model/[id]', params: { id: String(m.id) } })} style={styles.model}>
                    <Text variant="caption" color={semantic.textPrimary} style={styles.bold}>
                      《{m.title}》 ›
                    </Text>
                    {m.structure?.opening ? <Text variant="caption">开头：{m.structure.opening}</Text> : null}
                    {(m.structure?.points ?? []).map((p) => (
                      <Text key={p} variant="caption">
                        · {p}
                      </Text>
                    ))}
                  </Pressable>
                ))}
              </>
            )}
          </View>
        ) : null}
      </ScrollView>
      <View style={styles.nav}>
        <Button
          title="存入作文本"
          kind="secondary"
          style={styles.flex}
          onPress={() => {
            toast('已在作文本里');
            router.push({ pathname: '/essay/book', params: { subjectId: String(e.subject_id) } });
          }}
        />
        <Button title="按建议重写" style={styles.flex2} loading={rewrite.isPending} onPress={() => rewrite.mutate({ parent_essay_id: e.id })} />
      </View>
      <DisputeSheet e={e} visible={dispute} onClose={() => setDispute(false)} />
    </Screen>
  );
}

export default function EssayResultPage() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const essay = useEssay(Number(id));
  if (essay.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (essay.isError || !essay.data) return <Screen><ErrorState error={essay.error} onRetry={() => void essay.refetch()} /></Screen>;
  const e: Schemas['Essay'] = essay.data;
  if (e.status === 'grading') {
    const generic = e.rubric?.source === 'generic';
    return (
      <Screen>
        <PageHeader title="作文批改" onBack={() => router.back()} />
        <Text variant="caption">
          {e.topic} · {e.word_count} 字 · 大约 30 秒
        </Text>
        <AIGenerating
          steps={['通读全文，提取立意与结构', generic ? '按通用五维度打分' : '按你资料里的评分细则打分', '生成逐段批注与修改建议']}
          current={1}
          eta="大约 30 秒"
          onLeave={() => router.back()}
        />
        <Text variant="caption">可以先离开，批改完成后会通知你</Text>
      </Screen>
    );
  }
  if (e.status !== 'graded') {
    return (
      <Screen>
        <PageHeader title="作文批改" onBack={() => router.back()} />
        <Card style={styles.gap}>
          <Text variant="body">{e.fail_reason ?? '这篇还没提交批改'}</Text>
          <Button title="回去修改并提交" onPress={() => router.replace({ pathname: '/essay/write/[id]', params: { id: String(e.id) } })} />
        </Card>
      </Screen>
    );
  }
  return <Result e={e} />;
}

const styles = StyleSheet.create({
  scroll: { gap: 12, paddingBottom: spacing.xl },
  gap: { gap: 12 },
  gapSm: { gap: 10 },
  flex: { flex: 1 },
  flex2: { flex: 1.6 },
  bold: { fontWeight: '700' },
  lh: { lineHeight: 22 },
  link: { paddingHorizontal: 0 },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  headRow: { flexDirection: 'row', alignItems: 'flex-end' },
  rowBase: { flexDirection: 'row', alignItems: 'baseline' },
  big: { fontSize: 52, lineHeight: 58 },
  right: { alignItems: 'flex-end', gap: 4, paddingBottom: 8 },
  deltaPill: { paddingHorizontal: 10, paddingVertical: 3, borderRadius: radius.pill },
  delta: { marginLeft: spacing.sm },
  dim: { gap: 4 },
  dimRow: { flexDirection: 'row', alignItems: 'center', gap: 10, minHeight: 24 },
  dimName: { width: 76 },
  bar: { flex: 1, height: 6, borderRadius: 3, backgroundColor: semantic.border, overflow: 'hidden' },
  fill: { height: 6, borderRadius: 3 },
  frac: { width: 44, textAlign: 'right' },
  fb: { flexDirection: 'row', gap: 10 },
  fbLabel: { fontWeight: '700', width: 30, lineHeight: 22 },
  models: { gap: 8, padding: 16, borderRadius: radius.xl, backgroundColor: semantic.amberSoft },
  model: { gap: 2, paddingTop: 6 },
  nav: { flexDirection: 'row', gap: 10, paddingVertical: spacing.md },
  para: { gap: spacing.xs, paddingVertical: spacing.sm, borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: semantic.border },
  mark: { backgroundColor: semantic.dangerSoft, textDecorationLine: 'underline', textDecorationColor: colors.red },
  note: { minHeight: 88, padding: 12, borderRadius: 14, borderWidth: 1, borderColor: semantic.border, fontSize: 14, color: semantic.textPrimary },
});
