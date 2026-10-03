// 主观题：4.4 作答（打字、语音与拍照入口、草稿、字数、剩余次数、限时）、4.6 批改中、4.7 批改结果、4.8 异议。
import type { Schemas } from '@training/api-client';
import { radius, semantic, spacing } from '@training/ui-tokens';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { router } from 'expo-router';
import { useEffect, useRef, useState } from 'react';
import { StyleSheet, Switch, TextInput, View } from 'react-native';
import { AIGenerating, BottomSheet, Button, Tag, Text, toast } from '@/components';
import { importKeys, useQuota } from '@/features/import/api';
import { Checkbox } from '@/features/import/ui';
import { useFeatureFlag } from '@/lib/flags';
import { api, unwrap } from '@/lib/api';
import { getJSON, remove, setJSON } from '@/lib/storage';
import { lossNames, type PracticeQuestion } from './api';

export type GradingResult = Schemas['GradingResult'];

/** 建议字数（PRD 4.4，按题型默认，后台可改）。 */
const suggestedWords: Partial<Record<Schemas['QuestionType'], [number, number?]>> = {
  term: [80, 150],
  short_answer: [300, 500],
  discussion: [800],
};

export function wordHint(qtype: Schemas['QuestionType'], n: number) {
  const s = suggestedWords[qtype];
  if (!s) return `${n} 字`;
  return `${n} 字 · 建议 ${s[1] ? `${s[0]}–${s[1]}` : `${s[0]} 以上`}`;
}

const draftKey = (sessionId: number, qid: number) => `draft:practice:${sessionId}:${qid}`;

/** 草稿每 5 秒存一次（CLAUDE.md 必须遵守第 7 条），提交成功后清除。 */
export function useDraft(sessionId: number, qid: number) {
  const [text, setText] = useState(() => getJSON<string>(draftKey(sessionId, qid)) ?? '');
  const latest = useRef(text);
  useEffect(() => {
    latest.current = text;
  }, [text]);
  useEffect(() => {
    const t = setInterval(() => setJSON(draftKey(sessionId, qid), latest.current), 5000);
    return () => {
      clearInterval(t);
      setJSON(draftKey(sessionId, qid), latest.current);
    };
  }, [sessionId, qid]);
  const clear = () => remove(draftKey(sessionId, qid));
  return { text, setText, clear };
}

export function useGradingRemaining() {
  const quota = useQuota();
  const item = quota.data?.items.find((i) => i.quota_type === 'grading');
  if (!item) return undefined;
  if (item.limit === undefined || item.limit === null) return null;
  return Math.max(item.limit - item.used, 0);
}

export function SubjectiveInput({
  q,
  text,
  onChange,
  timed,
  onTimed,
}: {
  q: PracticeQuestion;
  text: string;
  onChange: (v: string) => void;
  timed: boolean;
  onTimed: (v: boolean) => void;
}) {
  const voice = useFeatureFlag('voice_answer');
  const remaining = useGradingRemaining();
  const n = [...text.replace(/\s/g, '')].length;
  return (
    <View style={styles.gap}>
      <Text variant="caption">
        {q.source_ref ? `出自 ${q.source_ref.file_name} · ` : ''}
        {q.rubric_count > 0 ? `按你资料里的 ${q.rubric_count} 个采分点批改` : '按参考答案批改'}
      </Text>
      <TextInput
        accessibilityLabel="你的答案"
        multiline
        value={text}
        onChangeText={onChange}
        placeholder="写下你的答案"
        placeholderTextColor={semantic.textSecondary}
        style={styles.answer}
        textAlignVertical="top"
      />
      <View style={styles.row}>
        {voice ? <Button title="语音" kind="text" onPress={() => toast('语音作答马上上线')} /> : null}
        <Button title="拍手写稿" kind="text" onPress={() => toast('拍手写稿马上上线')} />
        <Text variant="caption" style={styles.flex}>
          {wordHint(q.qtype, n)}
        </Text>
      </View>
      <View style={styles.row}>
        <Text variant="caption" style={styles.flex}>
          {remaining === undefined ? '' : remaining === null ? '批改次数不限' : `今日免费批改还剩 ${remaining} 次`}
        </Text>
        <Text variant="caption">限时作答</Text>
        <Switch accessibilityLabel="限时作答" value={timed} onValueChange={onTimed} trackColor={{ true: semantic.primary }} />
      </View>
    </View>
  );
}

/** 4.6 AI 批改中：三步进度，约 5 秒；写明采分点出处，超过 15 秒换安抚文案（AIGenerating 内置）。 */
export function GradingProgress({ q }: { q: PracticeQuestion }) {
  const [step, setStep] = useState(0);
  useEffect(() => {
    const t = setInterval(() => setStep((s) => Math.min(s + 1, 2)), 1700);
    return () => clearInterval(t);
  }, []);
  return (
    <View style={styles.gap}>
      <Text variant="h2">AI 正在批改</Text>
      <AIGenerating steps={['提取你的答案要点', `比对你资料里的 ${q.rubric_count || 1} 个采分点`, '生成修改建议']} current={step} eta="大约需要 5 秒" />
      {q.source_ref ? <Text variant="caption">采分点来自：{q.source_ref.file_name}{q.source_ref.page ? ` 第 ${q.source_ref.page} 页` : ''}。</Text> : null}
      <Text variant="caption">采分点不对的话，可以在知识点里改，之后按新的批改。</Text>
    </View>
  );
}

const verdictText = { hit: '命中', partial: '部分', miss: '遗漏' } as const;
const verdictTone = { hit: 'mastered', partial: 'progress', miss: 'danger' } as const;
const sourceText: Record<Schemas['RubricSource'], string> = {
  user_confirmed: '你确认过的采分点',
  ai_extracted: 'AI 从你资料里提取、待你确认的采分点',
  ai_generated: 'AI 生成的采分点',
  official: '官方题库的采分点',
  reference_answer: '参考答案（这道题还没有采分点）',
};
const nextAction = { knowledge: '学知识点', norm: '看规范写法', time: '限时再练' } as const;

/** 4.7 批改结果。 */
export function GradingResultView({ g, kpId, onDispute, onRegrade, regrading }: { g: GradingResult; kpId?: number; onDispute: () => void; onRegrade: () => void; regrading: boolean }) {
  const [showRef, setShowRef] = useState(false);
  return (
    <View style={styles.gap}>
      <View style={styles.row}>
        <Text variant="score">{g.score ?? '—'}</Text>
        <Text variant="body" style={styles.flex}>
          / {g.full_score} 分
        </Text>
        {g.disputed ? <Tag label="已复核" tone="info" /> : <Button title="有异议" kind="text" onPress={onDispute} />}
      </View>
      {g.trigger !== 'submit' ? (
        <Text variant="caption" color={semantic.info}>
          {g.trigger === 'dispute_recheck' ? '复核重批的结果，未消耗批改次数' : g.trigger === 'rubric_changed' ? '按新采分点重批，未消耗批改次数' : '待批改已提交'}
        </Text>
      ) : null}
      {g.counts ? (
        <Text variant="caption">
          命中 {g.counts.hit} · 部分命中 {g.counts.partial} · 遗漏 {g.counts.miss}
        </Text>
      ) : null}
      {g.points.map((p) => (
        <View key={p.seq} style={styles.point}>
          <View style={styles.row}>
            <Text variant="bodyStrong" style={styles.flex}>
              {p.content}
            </Text>
            <Tag label={`${verdictText[p.verdict]} +${p.got}`} tone={verdictTone[p.verdict]} />
          </View>
          {p.quote ? <Text variant="caption">你写的是「{p.quote}」</Text> : null}
          {p.reason ? <Text variant="caption">{p.reason}</Text> : null}
        </View>
      ))}
      <Text variant="caption">
        批改依据：{sourceText[g.rubric_source]}
        {g.rubric_ref ? ` · 出自 ${g.rubric_ref.file_name}${g.rubric_ref.page ? ` 第 ${g.rubric_ref.page} 页` : ''}` : ''}
      </Text>
      {g.rubric_changed ? (
        <Button title="采分点已修改，按新采分点重批" kind="secondary" loading={regrading} onPress={onRegrade} />
      ) : (
        <Button title="采分点不对？" kind="text" onPress={() => router.push({ pathname: '/bank/question/[id]', params: { id: String(g.question_id) } })} />
      )}
      {g.structure_note ? <Text variant="caption">{g.structure_note}</Text> : null}
      {g.loss.length > 0 ? (
        <View style={styles.box}>
          <Text variant="bodyStrong">失分归因</Text>
          {g.loss.map((l) => (
            <View key={l.type} style={styles.row}>
              <View style={styles.flex}>
                <Text variant="body">
                  {lossNames[l.type]} −{l.points}
                </Text>
                <Text variant="caption">{l.reason}</Text>
              </View>
              <Button
                title={nextAction[l.type]}
                kind="text"
                onPress={() =>
                  l.type === 'knowledge' && kpId
                    ? router.push({ pathname: '/bank/kp/[id]', params: { id: String(kpId) } })
                    : toast(l.type === 'norm' ? '答题规范马上上线' : '在答题页打开「限时作答」再练一次')
                }
              />
            </View>
          ))}
        </View>
      ) : null}
      {g.suggestions.length > 0 ? (
        <View style={styles.box}>
          <Text variant="bodyStrong">修改建议</Text>
          {g.suggestions.map((s) => (
            <Text key={s} variant="body">
              · {s}
            </Text>
          ))}
        </View>
      ) : null}
      {g.kp_changes.map((k) => (
        <Text key={k.kp_id} variant="caption">
          {k.name} 掌握分 {Math.round(k.m)}
        </Text>
      ))}
      {g.wrong_book === 'added' || g.wrong_book === 'still' ? <Text variant="caption" color={semantic.danger}>已加入错题本</Text> : null}
      {g.reference_answer ? (
        <View>
          <Button title={showRef ? '收起参考答案' : '参考答案'} kind="text" onPress={() => setShowRef(!showRef)} />
          {showRef ? <Text variant="body">{g.reference_answer}</Text> : null}
        </View>
      ) : null}
      <Text variant="small" color={semantic.textSecondary}>
        AI 批改得分，仅供参考
      </Text>
    </View>
  );
}

const reasons: { key: Schemas['DisputeRequest']['reason']; text: string }[] = [
  { key: 'hit_missed', text: '我其实答到了某个采分点' },
  { key: 'rubric_wrong', text: '采分点本身不对' },
  { key: 'score_unfair', text: '分数给得不合理' },
  { key: 'other', text: '其他' },
];

/** 4.8 批改异议：四选一；「采分点本身不对」引导去改采分点，改完按新采分点重批；其余提交复核重批一次，不扣次数。 */
export function DisputeSheet({ g, visible, onClose, onDone }: { g: GradingResult; visible: boolean; onClose: () => void; onDone: (r: GradingResult) => void }) {
  const [reason, setReason] = useState<Schemas['DisputeRequest']['reason']>();
  const [note, setNote] = useState('');
  const [allow, setAllow] = useState(false);
  const submit = useMutation({
    mutationFn: () => unwrap(api.POST('/gradings/{gradingId}/disputes', { params: { path: { gradingId: g.grading_id } }, body: { reason: reason!, note: note || undefined, allow_access: allow } })),
    onSuccess: (r) => {
      onDone(r);
      onClose();
    },
    onError: (e) => toast(e instanceof Error ? e.message : '没提交成功，请重试'),
  });
  return (
    <BottomSheet visible={visible} onClose={onClose} title="对批改有异议？">
      <View style={styles.gap}>
        {reasons.map((r) => (
          <Checkbox key={r.key} checked={reason === r.key} onChange={() => setReason(r.key)} label={r.text} />
        ))}
        {reason === 'rubric_wrong' ? (
          <>
            <Text variant="caption">采分点来自你的资料，可以直接改，改完这道题按新采分点重批，不消耗批改次数。</Text>
            <Button
              title="去修改 ›"
              onPress={() => {
                onClose();
                router.push({ pathname: '/bank/question/[id]', params: { id: String(g.question_id) } });
              }}
            />
          </>
        ) : (
          <>
            <TextInput accessibilityLabel="补充说明" value={note} onChangeText={setNote} placeholder="补充说明（选填）" placeholderTextColor={semantic.textSecondary} style={styles.note} />
            <Checkbox checked={allow} onChange={setAllow} label="允许后台查看这道题和我的答案，用于排查批改问题" />
            <Text variant="caption">提交后会重新批改一次，不消耗批改次数。</Text>
            <Button title="提交" disabled={!reason} loading={submit.isPending} onPress={() => submit.mutate()} />
          </>
        )}
      </View>
    </BottomSheet>
  );
}

export function useRegrade(onDone: (r: GradingResult) => void) {
  return useMutation({
    mutationFn: (gradingId: number) => unwrap(api.POST('/gradings/{gradingId}/regrade', { params: { path: { gradingId } } })),
    onSuccess: onDone,
    onError: (e) => toast(e instanceof Error ? e.message : '重批失败，未扣除次数'),
  });
}

export function useGrading(id: number) {
  return useQuery({ queryKey: ['grading', id], queryFn: () => unwrap(api.GET('/gradings/{gradingId}', { params: { path: { gradingId: id } } })) });
}

export function useSubmitSubjective(sessionId: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: Schemas['SubmitSubjectiveRequest']) => unwrap(api.POST('/practice-sessions/{sessionId}/gradings', { params: { path: { sessionId } }, body })),
    onSettled: () => void qc.invalidateQueries({ queryKey: importKeys.quota }),
  });
}

const styles = StyleSheet.create({
  gap: { gap: spacing.sm },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  flex: { flex: 1 },
  answer: { minHeight: 180, borderWidth: 1, borderColor: semantic.border, borderRadius: radius.md, padding: spacing.md, fontSize: 16, lineHeight: 24, color: semantic.textPrimary, backgroundColor: semantic.surface },
  point: { gap: 2, paddingVertical: spacing.sm, borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: semantic.border },
  box: { gap: spacing.xs, padding: spacing.md, borderRadius: radius.md, backgroundColor: semantic.surface, borderWidth: 1, borderColor: semantic.border },
  note: { minHeight: 44, borderWidth: 1, borderColor: semantic.border, borderRadius: radius.md, paddingHorizontal: spacing.md, fontSize: 16, color: semantic.textPrimary },
});

/** 待批改（4.9「明天再批改」存下的答案）：训练页一键提交。 */
export function PendingGradingsCard() {
  const qc = useQueryClient();
  const pending = useQuery({ queryKey: ['gradings', 'pending'], queryFn: () => unwrap(api.GET('/gradings/pending')) });
  const submit = useMutation({
    mutationFn: () => unwrap(api.POST('/gradings/pending/submit')),
    onSuccess: (r) => {
      void qc.invalidateQueries({ queryKey: ['gradings', 'pending'] });
      void qc.invalidateQueries({ queryKey: importKeys.quota });
      if (r.graded === 0) toast('今天的批改次数已用完，明天再提交');
      else {
        toast(`已批改 ${r.graded} 道${r.remaining_pending ? `，还有 ${r.remaining_pending} 道待批改` : ''}`);
        router.push({ pathname: '/practice/grading/[id]', params: { id: String(r.results[0]!.grading_id) } });
      }
    },
    onError: (e) => toast(e instanceof Error ? e.message : '提交失败，请重试'),
  });
  const items = pending.data?.items ?? [];
  if (items.length === 0) return null;
  const left = pending.data?.remaining_today;
  return (
    <View style={styles.box}>
      <Text variant="bodyStrong">{items.length} 道主观题待批改</Text>
      <Text variant="caption">{left === 0 ? '今天的批改次数已用完，明天 0 点后可以提交' : '答案已保存，按存入顺序批改'}</Text>
      <Button title="一键提交" kind="secondary" disabled={left === 0} loading={submit.isPending} onPress={() => submit.mutate()} />
    </View>
  );
}
