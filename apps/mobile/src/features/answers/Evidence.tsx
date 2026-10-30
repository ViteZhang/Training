// 官方教材库 APP 06–09：只显示本次生成快照；书名配置不能充当答案依据。
import type { Schemas } from '@training/api-client';
import { colors, radius, semantic, spacing } from '@training/ui-tokens';
import { useQuery } from '@tanstack/react-query';
import { router } from 'expo-router';
import { useState } from 'react';
import { ScrollView, StyleSheet, View, useWindowDimensions } from 'react-native';
import { BottomSheet, Button, ErrorState, Loading, Text } from '@/components';
import { api, unwrap } from '@/lib/api';

type Snapshot = Schemas['AnswerEvidenceSnapshot'];
export const evidenceKey = (kind: 'question' | 'grading' | 'import', id: number) => ['answer-evidence', kind, id] as const;
export function useAnswerEvidence(kind: 'question' | 'grading' | 'import', id: number) {
  return useQuery({
    queryKey: evidenceKey(kind, id),
    queryFn: () => kind === 'question'
      ? unwrap(api.GET('/questions/{questionId}/answer-evidence', { params: { path: { questionId: id } } }))
      : kind === 'grading'
        ? unwrap(api.GET('/gradings/{gradingId}/answer-evidence', { params: { path: { gradingId: id } } }))
        : unwrap(api.GET('/import-items/{itemId}/answer-evidence', { params: { path: { itemId: id } } })),
    enabled: id > 0,
  });
}

export function evidenceLabel(snapshot: Snapshot | null | undefined) {
  if (!snapshot) return '历史答案 · 来源未记录';
  const official = snapshot.sources.some((s) => s.kind === 'official_textbook');
  const personal = snapshot.sources.some((s) => s.kind === 'personal_material');
  if (official && personal) return snapshot.uses_general_knowledge ? '教材、个人资料依据 + AI 补充' : '依据教材与个人资料生成';
  if (official) return snapshot.uses_general_knowledge ? '教材依据 + AI 补充' : '依据教材生成';
  if (personal) return snapshot.uses_general_knowledge ? '个人资料依据 + AI 补充' : '依据你的资料生成';
  return 'AI 通用知识生成';
}

export function AnswerEvidence({ snapshot }: { snapshot: Snapshot | null | undefined }) {
  const [open, setOpen] = useState(false);
  const { height } = useWindowDimensions();
  const sources = snapshot?.sources ?? [];
  return (
    <View style={styles.box}>
      <Text variant="caption" color={semantic.textPrimary} style={styles.strong}>{evidenceLabel(snapshot)}</Text>
      {sources.map((s) => (
        <View key={s.id} style={styles.gap}>
          <Text variant="small">{s.kind === 'official_textbook' ? `《${s.title}》${s.edition ?? ''}` : s.title}</Text>
          {s.heading ? <Text variant="small">{s.heading}</Text> : null}
        </View>
      ))}
      {snapshot && sources.length === 0 ? <Text variant="small">由 AI 整理学科通行观点，请核对后确认。</Text> : null}
      {sources.length > 0 ? <Button title="查看依据摘录 ›" kind="text" color={semantic.textPrimary} style={styles.link} onPress={() => setOpen(true)} /> : null}
      <BottomSheet visible={open} title="答案依据" onClose={() => setOpen(false)}>
        <ScrollView style={{ maxHeight: height * 0.62 }} contentContainerStyle={styles.sheet}>
          <Text variant="small">本次生成使用的相关原文</Text>
          {sources.map((s) => (
            <View key={s.id} style={styles.source}>
              <Text variant="h3">{s.title}</Text>
              <Text variant="caption">{[s.author, s.edition].filter(Boolean).join(' · ')}</Text>
              {s.heading ? <Text variant="caption">{s.heading}</Text> : null}
              {s.unavailable ? <Text variant="small">依据摘录暂不可查看</Text> : (
                <>
                  <View style={styles.quote}><Text variant="body">{s.quote}</Text></View>
                  {s.claim ? <Text variant="small">支持：{s.claim}</Text> : null}
                  {s.printed_page ? <Text variant="small">教材第 {s.printed_page} 页</Text> : null}
                  {s.kind === 'personal_material' && s.material_id && s.page ? <Button title="查看资料原文 ›" kind="text" color={semantic.textPrimary} onPress={() => router.push({ pathname: '/bank/page', params: { materialId: String(s.material_id), page: String(s.page), highlight: s.quote.slice(0, 30) } })} /> : null}
                </>
              )}
            </View>
          ))}
          <Text variant="small">AI 根据相关原文组织答案，仍需你核对。这里只展示本题相关摘录。</Text>
          <Button title="返回答案" onPress={() => setOpen(false)} />
        </ScrollView>
      </BottomSheet>
    </View>
  );
}

export function RecordAnswerEvidence({ kind, id }: { kind: 'question' | 'grading' | 'import'; id: number }) {
  const query = useAnswerEvidence(kind, id);
  if (query.isPending) return <Loading rows={1} />;
  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;
  return <AnswerEvidence snapshot={query.data.snapshot} />;
}

const styles = StyleSheet.create({
  box: { backgroundColor: semantic.fill, borderRadius: radius.md, padding: spacing.md, gap: spacing.xs, marginTop: spacing.sm },
  gap: { gap: 2 }, strong: { fontWeight: '600' },
  link: { alignSelf: 'flex-start', paddingHorizontal: 0 },
  sheet: { gap: spacing.md, paddingBottom: spacing.sm },
  source: { gap: spacing.xs, paddingVertical: spacing.sm },
  quote: { backgroundColor: colors.blueSoft, borderRadius: radius.md, padding: spacing.md, marginVertical: spacing.xs },
});
