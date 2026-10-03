// 4.4 主观题作答 → 4.6 批改中 → 4.7（由答题页显示）；次数用完 4.9；批改失败显示「未扣除次数」可重试；「看参考答案」进入自评。
import { ApiError } from '@training/api-client';
import { semantic, spacing } from '@training/ui-tokens';
import * as Crypto from 'expo-crypto';
import { useRef, useState } from 'react';
import { StyleSheet, View } from 'react-native';
import { AIFailed, Button, QuotaSheet, Text, toast } from '@/components';
import type { PracticeQuestion } from './api';
import { GradingProgress, SubjectiveInput, useDraft, useSubmitSubjective, type GradingResult } from './grading';

type Level = 'unknown' | 'vague' | 'mastered';

export function SubjectiveFlow({
  q,
  sessionId,
  onGraded,
  onQueued,
  onSelfAssess,
}: {
  q: PracticeQuestion;
  sessionId: number;
  onGraded: (g: GradingResult) => void;
  onQueued: () => void;
  onSelfAssess: (level: Level) => void;
}) {
  const draft = useDraft(sessionId, q.id);
  const [timed, setTimed] = useState(false);
  const [showRef, setShowRef] = useState(false);
  const [quotaOut, setQuotaOut] = useState(false);
  const [failed, setFailed] = useState(false);
  const [started] = useState(() => Date.now());
  const key = useRef('');
  const submit = useSubmitSubjective(sessionId);

  const send = () => {
    setFailed(false);
    if (!key.current) key.current = Crypto.randomUUID();
    submit.mutate(
      {
        question_id: q.id,
        idempotency_key: key.current,
        answer_text: draft.text,
        answer_mode: 'typed',
        duration_seconds: Math.round((Date.now() - started) / 1000),
        timed,
      },
      {
        onSuccess: (g) => {
          draft.clear();
          if (g.status === 'queued_quota') setQuotaOut(true);
          else onGraded(g);
        },
        onError: (e) => {
          // 批改失败不扣次数、不保存；换一个幂等键重试。
          key.current = '';
          if (e instanceof ApiError && e.code === 'AI_FAILED') setFailed(true);
          else toast(e instanceof Error ? e.message : '提交失败，请重试');
        },
      },
    );
  };

  if (submit.isPending) return <GradingProgress q={q} />;
  if (failed) return <AIFailed onRetry={send} />;

  if (showRef) {
    return (
      <View style={styles.gap}>
        <View style={styles.ref}>
          <Text variant="bodyStrong">参考答案</Text>
          <Text variant="body">{q.answer ?? '这道题还没有参考答案'}</Text>
        </View>
        <Text variant="bodyStrong">对照参考答案，你掌握得怎么样？</Text>
        <View style={styles.row}>
          {(
            [
              ['unknown', '不会'],
              ['vague', '模糊'],
              ['mastered', '掌握'],
            ] as const
          ).map(([k, label]) => (
            <Button key={k} title={label} kind="secondary" style={styles.flex} onPress={() => onSelfAssess(k)} />
          ))}
        </View>
      </View>
    );
  }

  return (
    <View style={styles.gap}>
      <SubjectiveInput q={q} text={draft.text} onChange={draft.setText} timed={timed} onTimed={setTimed} />
      <Button title="提交批改" disabled={!draft.text.trim()} onPress={send} />
      <Button title="看参考答案" kind="text" onPress={() => setShowRef(true)} />
      <QuotaSheet
        visible={quotaOut}
        onClose={() => {
          setQuotaOut(false);
          onQueued();
        }}
        title="今天的免费批改次数用完了"
        desc="免费版每天可批改 3 道主观题，明天 0 点恢复。你的答案已保存。"
        onUpgrade={() => toast('会员马上上线')}
        freeOptions={[
          {
            label: '先对照参考答案',
            onPress: () => {
              setQuotaOut(false);
              setShowRef(true);
            },
          },
          {
            label: '明天再批改',
            onPress: () => {
              setQuotaOut(false);
              onQueued();
            },
          },
        ]}
      />
      {timed ? <Text variant="caption" color={semantic.textSecondary}>限时作答：按建议用时计时，没写完的部分会算作「时间不够」</Text> : null}
    </View>
  );
}

const styles = StyleSheet.create({
  gap: { gap: spacing.sm },
  row: { flexDirection: 'row', gap: spacing.sm },
  flex: { flex: 1 },
  ref: { gap: spacing.xs, padding: spacing.md, borderRadius: 12, backgroundColor: semantic.infoSoft },
});
