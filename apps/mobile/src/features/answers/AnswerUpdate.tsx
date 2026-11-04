// 官方教材库 APP 10：生成候选成功扣一次，采用不再扣；退出不覆盖原答案。
import { ApiError, type Schemas } from '@training/api-client';
import { semantic, spacing } from '@training/ui-tokens';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import * as Crypto from 'expo-crypto';
import { useRef, useState } from 'react';
import { ScrollView, StyleSheet, View, useWindowDimensions } from 'react-native';
import { BottomSheet, Button, ConfirmDialog, ErrorState, InfoCard, Loading, QuotaSheet, RubricLine, Text, toast } from '@/components';
import { bankKeys, type QuestionDetail } from '@/features/bank/api';
import { api, unwrap } from '@/lib/api';
import { AnswerEvidence, evidenceKey, useAnswerEvidence } from './Evidence';

type Candidate = Schemas['AnswerUpdateCandidate'];
const candidateKey = (id: number) => ['answer-candidates', id] as const;
export function AnswerUpdate({ question }: { question: QuestionDetail }) {
  const qc = useQueryClient();
  const { height } = useWindowDimensions();
  const [open, setOpen] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);
  const [deciding, setDeciding] = useState(false);
  const [quotaOut, setQuotaOut] = useState(false);
  const [retryCreate, setRetryCreate] = useState(false);
  const [error, setError] = useState<unknown>();
  const createKey = useRef<string | undefined>(undefined);
  const decisionKeys = useRef<Record<string, string>>({});
  const oldEvidence = useAnswerEvidence('question', question.id);
  const list = useQuery({
    queryKey: candidateKey(question.id),
    queryFn: () => unwrap(api.GET('/questions/{questionId}/answer-candidates', { params: { path: { questionId: question.id } } })),
    refetchInterval: (q) => q.state.data?.items?.some((c) => c.status === 'queued' || c.status === 'processing') ? 2000 : false,
  });
  const candidate = list.data?.items?.find((c) => c.status === 'ready' || c.status === 'queued' || c.status === 'processing' || c.status === 'failed');
  const generating = candidate?.status === 'queued' || candidate?.status === 'processing';
  const create = async () => {
    setConfirm(false); setBusy(true); setError(undefined); setRetryCreate(false); setOpen(true);
    createKey.current ??= Crypto.randomUUID();
    try {
      const saved = await unwrap(api.POST('/questions/{questionId}/answer-candidates', { params: { path: { questionId: question.id } }, body: { idempotency_key: createKey.current } }));
      qc.setQueryData(candidateKey(question.id), { items: [saved, ...(list.data?.items ?? []).filter((c) => c.id !== saved.id)] });
      createKey.current = undefined;
    } catch (e) {
      if (e instanceof ApiError && e.isQuotaExceeded) { setOpen(false); setQuotaOut(true); }
      else { setError(e); setRetryCreate(true); }
      // A quota or validation rejection cannot have created a candidate.
      if (e instanceof ApiError && e.status > 0 && e.status < 500) createKey.current = undefined;
      void list.refetch();
    } finally { setBusy(false); }
  };
  const decide = async (c: Candidate, adopt: boolean) => {
    setDeciding(true); setError(undefined); setRetryCreate(false);
    const token = `${c.id}:${adopt}`;
    decisionKeys.current[token] ??= Crypto.randomUUID();
    const init = { params: { path: { questionId: question.id, candidateId: c.id } }, body: { idempotency_key: decisionKeys.current[token] } };
    try {
      await unwrap(adopt ? api.POST('/questions/{questionId}/answer-candidates/{candidateId}/adopt', init) : api.POST('/questions/{questionId}/answer-candidates/{candidateId}/discard', init));
      await Promise.all([
        qc.invalidateQueries({ queryKey: bankKeys.question(question.id) }),
        qc.invalidateQueries({ queryKey: candidateKey(question.id) }),
        qc.invalidateQueries({ queryKey: evidenceKey('question', question.id) }),
        qc.invalidateQueries({ queryKey: ['bank'] }),
      ]);
      setOpen(false);
      toast(adopt ? '已采用，新答案和采分点仍待确认' : '已保留原答案');
    } catch (e) { setError(e); await qc.invalidateQueries({ queryKey: bankKeys.question(question.id) }); }
    finally { setDeciding(false); }
  };
  return (
    <View>
      <Button title={candidate && candidate.status !== 'failed' ? generating ? '正在生成，查看进度' : '查看新答案候选' : '按当前教材重新生成'} kind="text" color={semantic.textPrimary} style={styles.link} onPress={() => candidate && candidate.status !== 'failed' ? setOpen(true) : setConfirm(true)} />
      <ConfirmDialog visible={confirm} title="更新参考答案？" message="成功生成可查看的新候选会扣 1 次 AI 生成额度；采用不再扣次，生成失败不扣次，放弃候选不退次。比较后再决定，原答案和历史批改暂不改变。" confirmText="生成新候选" onCancel={() => setConfirm(false)} onConfirm={() => void create()} />
      <QuotaSheet visible={quotaOut} onClose={() => setQuotaOut(false)} title="今天的 AI 出题次数用完了" desc="原答案可以继续使用，明天 0 点恢复次数后再生成新候选。" freeOptions={[{ label: '继续使用原答案', onPress: () => setQuotaOut(false) }]} />
      <BottomSheet visible={open} title="比较答案" onClose={() => setOpen(false)}>
        <ScrollView style={{ maxHeight: height * 0.7 }} contentContainerStyle={styles.content}>
          <Text variant="small">比较后再决定是否采用，之前的批改记录不变。</Text>
          <InfoCard label="原答案">
            <Text variant="body">{question.answer}</Text>
            {question.rubric_points.map((p, i) => <RubricLine key={p.id} index={i} content={p.content} score={p.score} />)}
            {oldEvidence.isSuccess ? <AnswerEvidence snapshot={oldEvidence.data.snapshot} /> : null}
          </InfoCard>
          {busy || generating ? <View style={styles.content}><Loading rows={3} /><Text variant="bodyStrong">正在生成参考答案</Text><Text variant="small">正在整理相关内容与采分点，你可以稍后回来查看。</Text></View> : null}
          {list.isError ? <ErrorState error={list.error} onRetry={() => void list.refetch()} /> : null}
          {error ? <ErrorState error={error} onRetry={() => { if (retryCreate) void create(); else { setError(undefined); void list.refetch(); } }} /> : null}
          {candidate?.status === 'failed' && !busy ? <View style={styles.content}><Text variant="body">{candidate.failure_message || '生成失败，未扣除次数'}</Text><Button title="重新生成" onPress={() => setConfirm(true)} /></View> : null}
          {candidate?.status === 'ready' && !busy ? (
            <>
              <InfoCard label="新答案">
                <Text variant="body">{candidate.answer}</Text>
                {candidate.points?.map((p, i) => <RubricLine key={i} index={i} content={p.content} score={p.score} />)}
                <AnswerEvidence snapshot={candidate.snapshot} />
              </InfoCard>
              <Text variant="small">采用后，新答案及采分点仍待确认。之前的作答与评分保留原来的依据。</Text>
              <Button title="采用新答案" loading={deciding} onPress={() => void decide(candidate, true)} />
              <Button title="保留原答案" kind="secondary" disabled={deciding} onPress={() => void decide(candidate, false)} />
            </>
          ) : null}
          <Button title="稍后查看" kind="text" disabled={deciding} onPress={() => setOpen(false)} />
        </ScrollView>
      </BottomSheet>
    </View>
  );
}
const styles = StyleSheet.create({ link: { alignSelf: 'flex-start', paddingHorizontal: 0 }, content: { gap: spacing.md, paddingVertical: spacing.sm } });
