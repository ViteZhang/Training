// 5.5 作文批改中：三步进度（提取立意与结构、按评分细则打分、生成逐段批注），约 30 秒；可先离开，完成后发消息。
// 5.6 作文批改结果：总分 / 满分、较上篇、所用评分标准（→ 5.9）；各维度得分；Tab：总评（亮点、问题、建议）、逐段批注（原文标出问题句并给修改建议）、
// 范文对比（用户导入的同题范文要点）；「存入作文本」「按建议重写」（记为下一稿）；「有异议」与主观题相同。
import type { Schemas } from '@training/api-client';
import { colors, radius, semantic, spacing } from '@training/ui-tokens';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, TextInput, View } from 'react-native';
import { AIGenerating, BottomSheet, Button, Card, ErrorState, Loading, ProgressBar, Screen, Text, toast } from '@/components';
import { Checkbox, PageHeader, Segments } from '@/features/import/ui';
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
        <PageHeader title="作文批改" onBack={() => router.back()} right={!e.disputed ? <Button title="有异议" kind="text" onPress={() => setDispute(true)} /> : undefined} />
        <Text variant="caption" numberOfLines={2}>
          {e.topic} · {e.word_count} 字{e.draft_no > 1 ? ` · 第 ${e.draft_no} 稿` : ''}
        </Text>
        <Card style={styles.gap}>
          <Text variant="small">AI 批改得分 · 仅供参考</Text>
          <View style={styles.rowBase}>
            <Text variant="score">{fmtScore(e.score)}</Text>
            <Text variant="caption"> / {fmtScore(e.full_score)}</Text>
            {e.prev_delta !== undefined ? (
              <Text variant="caption" color={e.prev_delta >= 0 ? semantic.mastered : semantic.danger} style={styles.delta}>
                较上篇 {e.prev_delta >= 0 ? `+${e.prev_delta}` : e.prev_delta}
              </Text>
            ) : null}
          </View>
          {e.disputed && e.score_before !== undefined ? <Text variant="caption">已复核：原来 {fmtScore(e.score_before)} 分，以复核结果为准</Text> : null}
          <Pressable accessibilityRole="button" onPress={() => router.push({ pathname: '/essay/rubric', params: { subjectId: String(e.subject_id) } })}>
            <Text variant="caption" color={semantic.primary}>
              {generic ? '按通用五维度标准 · 分数只作参考，不计入预估分 ›' : `按你的评分细则 · ${e.counts_for_estimate ? '计入预估分' : '真题限时完成的才计入预估分'} ›`}
            </Text>
          </Pressable>
          {(e.dimensions ?? []).map((d) => (
            <View key={d.name} style={styles.dim}>
              <View style={styles.row}>
                <Text variant="body" style={styles.flex}>
                  {d.name}
                  {d.name === e.weakest_dimension ? <Text variant="caption" color={semantic.danger}>  失分主项</Text> : null}
                </Text>
                <Text variant="bodyStrong">
                  {fmtScore(d.score)}/{fmtScore(d.max)}
                </Text>
              </View>
              <ProgressBar value={d.max ? d.score / d.max : 0} />
              {d.comment ? <Text variant="small">{d.comment}</Text> : null}
            </View>
          ))}
        </Card>

        <Segments<Tab>
          options={[
            { key: 'summary', label: '总评' },
            { key: 'annotations', label: '逐段批注', count: e.annotations?.length },
            { key: 'models', label: '范文对比', count: models.length },
          ]}
          value={tab}
          onChange={setTab}
        />
        {tab === 'summary' ? (
          <Card style={styles.gap}>
            {e.thesis ? <Text variant="caption">立意：{e.thesis}</Text> : null}
            {[
              { title: '亮点', items: e.highlights ?? [] },
              { title: '问题', items: e.problems ?? [] },
              { title: '建议', items: e.suggestions ?? [] },
            ].map((g) =>
              g.items.length ? (
                <View key={g.title} style={styles.gapSm}>
                  <Text variant="bodyStrong">{g.title}</Text>
                  {g.items.map((t) => (
                    <Text key={t} variant="body">
                      · {t}
                    </Text>
                  ))}
                </View>
              ) : null,
            )}
          </Card>
        ) : null}
        {tab === 'annotations' ? (
          <Card>
            <Annotated e={e} />
          </Card>
        ) : null}
        {tab === 'models' ? (
          <Card style={styles.gap}>
            {models.length === 0 ? (
              <Text variant="caption">{e.topic_source === 'exam' ? '这道题还没有你导入的范文' : '只有真题题目会对照你导入的同题范文'}</Text>
            ) : (
              <>
                <Text variant="caption">你导入的同题范文 · {models.length} 篇</Text>
                {models.map((m) => (
                  <Pressable key={m.id} accessibilityRole="button" onPress={() => router.push({ pathname: '/essay/model/[id]', params: { id: String(m.id) } })} style={styles.model}>
                    <Text variant="bodyStrong">{m.title} ›</Text>
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
          </Card>
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
        <Button title="按建议重写" style={styles.flex} loading={rewrite.isPending} onPress={() => rewrite.mutate({ parent_essay_id: e.id })} />
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
  scroll: { gap: spacing.md, paddingBottom: spacing.xl },
  gap: { gap: spacing.sm },
  gapSm: { gap: spacing.xs },
  flex: { flex: 1 },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  rowBase: { flexDirection: 'row', alignItems: 'baseline', flexWrap: 'wrap' },
  delta: { marginLeft: spacing.sm },
  dim: { gap: 4, paddingVertical: 2 },
  para: { gap: spacing.xs, paddingVertical: spacing.sm, borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: semantic.border },
  mark: { backgroundColor: semantic.dangerSoft, textDecorationLine: 'underline', textDecorationColor: colors.red },
  model: { gap: 4, padding: spacing.md, borderRadius: radius.md, backgroundColor: semantic.background },
  note: { minHeight: 44, padding: spacing.md, borderRadius: radius.md, borderWidth: 1, borderColor: semantic.border },
  nav: { flexDirection: 'row', gap: spacing.sm },
});
