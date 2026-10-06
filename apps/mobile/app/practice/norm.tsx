// 4.13 答题规范：按题型讲结构（名词解释三段：定义、特征要点、出处或例子，并标约占分值）；
// 「高分写法」用用户资料里的原文与采分点；「你上次的写法」用用户自己的答案并标出缺了什么；「按结构写一道」由 AI 按结构批改。
import type { Schemas } from '@training/api-client';
import { colors, radius, semantic, spacing } from '@training/ui-tokens';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import * as Crypto from 'expo-crypto';
import { router, useLocalSearchParams } from 'expo-router';
import { useRef, useState } from 'react';
import { ScrollView, StyleSheet, TextInput, View } from 'react-native';
import { Button, Card, EmptyState, ErrorState, Loading, Screen, Tag, Text, toast } from '@/components';
import { importKeys, qtypeNames } from '@/features/import/api';
import { PageHeader, Segments } from '@/features/import/ui';
import { api, unwrap } from '@/lib/api';

type NormType = 'term' | 'short_answer' | 'discussion';
const types: NormType[] = ['term', 'short_answer', 'discussion'];

export default function NormPage() {
  const p = useLocalSearchParams<{ subjectId: string; qtype?: string }>();
  const sid = Number(p.subjectId);
  const [qtype, setQtype] = useState<NormType>(types.includes(p.qtype as NormType) ? (p.qtype as NormType) : 'term');
  const norm = useQuery({
    queryKey: ['norm', sid, qtype],
    queryFn: () => unwrap(api.GET('/subjects/{subjectId}/answer-norms/{qtype}', { params: { path: { subjectId: sid, qtype } } })),
  });
  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        <PageHeader title={`答题规范 · ${qtypeNames[qtype]}`} onBack={() => router.back()} />
        <Segments<NormType> value={qtype} onChange={setQtype} options={types.map((t) => ({ key: t, label: qtypeNames[t] }))} />
        {norm.isLoading ? <Loading rows={6} /> : norm.isError || !norm.data ? <ErrorState error={norm.error} onRetry={() => void norm.refetch()} /> : <NormBody n={norm.data} />}
      </ScrollView>
    </Screen>
  );
}

function NormBody({ n }: { n: Schemas['AnswerNorm'] }) {
  const [writing, setWriting] = useState(false);
  return (
    <View style={styles.gap}>
      <Card style={styles.gap}>
        <Text variant="caption" color={colors.ink} style={styles.bold}>
          {n.elements.length === 3 ? '三段结构' : `${n.elements.length} 个要素`}，照着写更容易拿满分
        </Text>
        {n.elements.map((e, i) => (
          <View key={e.name} style={styles.element}>
            <View style={styles.num}>
              <Text variant="small" color={semantic.textOnBrand} style={styles.bold}>
                {i + 1}
              </Text>
            </View>
            <View style={styles.flex}>
              <Text variant="caption" color={colors.ink} style={styles.bold}>
                {e.name}
              </Text>
              <Text variant="small">
                {e.desc}，约占 {Math.round(e.share * 100)}%
              </Text>
            </View>
          </View>
        ))}
        {n.tips.map((t) => (
          <Text key={t} variant="small">
            · {t}
          </Text>
        ))}
      </Card>

      {n.example ? (
        <View style={[styles.tinted, styles.good]}>
          <View style={styles.row}>
            <Text variant="caption" color="#1F6B4A" style={[styles.bold, styles.flex]}>
              高分写法
            </Text>
            <Text variant="small" color="#1F6B4A">
              用你资料里的原文和采分点 · {n.example.stem}
            </Text>
          </View>
          {n.example.original_text || n.example.reference_answer ? (
            <Text variant="caption" color={colors.ink} style={styles.lh}>
              {n.example.original_text ?? n.example.reference_answer}
            </Text>
          ) : null}
          <View style={styles.tags}>
            {n.example.rubric_points.map((r) => (
              <Tag key={r} label={r} tone="mastered" />
            ))}
          </View>
          {n.example.source_ref ? (
            <Text variant="small">
              出自 {n.example.source_ref.file_name}
              {n.example.source_ref.page ? ` 第 ${n.example.source_ref.page} 页` : ''}
            </Text>
          ) : null}
        </View>
      ) : (
        <EmptyState title="还没有带采分点的同类题" desc="导入真题或讲义后，这里会用你资料里的原文示范高分写法" />
      )}

      {n.last ? (
        <View style={[styles.tinted, styles.bad]}>
          <Text variant="caption" color={semantic.danger} style={styles.bold}>
            你上次的写法{n.last.score !== undefined && n.last.full_score !== undefined ? ` · ${n.last.score} / ${n.last.full_score} 分` : ''}
          </Text>
          <Text variant="caption" color={colors.ink} style={styles.lh}>
            {n.last.answer_text}
          </Text>
          {n.last.missing.length > 0 ? (
            <Text variant="small" color={semantic.danger}>
              缺：{n.last.missing.join('、')}
            </Text>
          ) : (
            <Text variant="small" color={semantic.mastered}>
              采分点都写到了
            </Text>
          )}
        </View>
      ) : null}

      {n.practice_question_id ? (
        writing ? (
          <NormWrite questionId={n.practice_question_id} elements={n.elements} />
        ) : (
          <Button title={`按${n.elements.length === 3 ? '三段' : ''}结构写一道`} onPress={() => setWriting(true)} />
        )
      ) : null}
    </View>
  );
}

function NormWrite({ questionId, elements }: { questionId: number; elements: Schemas['NormElement'][] }) {
  const qc = useQueryClient();
  const [text, setText] = useState('');
  const key = useRef('');
  const q = useQuery({ queryKey: ['question', questionId], queryFn: () => unwrap(api.GET('/questions/{questionId}', { params: { path: { questionId } } })) });
  const check = useMutation({
    mutationFn: () => {
      if (!key.current) key.current = Crypto.randomUUID();
      return unwrap(api.POST('/questions/{questionId}/norm-check', { params: { path: { questionId } }, body: { answer_text: text, idempotency_key: key.current } }));
    },
    onSuccess: () => void qc.invalidateQueries({ queryKey: importKeys.quota }),
    onError: (e) => {
      key.current = '';
      toast(e instanceof Error ? e.message : '批改失败，未扣除次数');
    },
  });
  const r = check.data;
  return (
    <Card style={styles.gap}>
      <Text variant="bodyStrong">{q.data?.stem ?? '…'}</Text>
      <Text variant="caption">按顺序写：{elements.map((e) => e.name).join(' → ')}；AI 只看结构，扣 1 次批改次数</Text>
      <TextInput accessibilityLabel="按结构作答" multiline value={text} onChangeText={setText} editable={!r} style={styles.input} textAlignVertical="top" />
      {r ? (
        <View style={styles.gap}>
          <Text variant="h3" color={r.complete ? semantic.mastered : semantic.danger}>
            {r.complete ? '结构完整' : '结构还不完整'}
          </Text>
          {r.elements.map((e) => (
            <View key={e.name} style={styles.gap}>
              <Text variant="bodyStrong" color={e.present ? semantic.mastered : semantic.danger}>
                {e.present ? '✓' : '✗'} {e.name}
              </Text>
              {e.quote ? <Text variant="caption">你写的是「{e.quote}」</Text> : null}
              {e.suggestion ? <Text variant="caption">{e.suggestion}</Text> : null}
            </View>
          ))}
          {r.suggestions.map((s) => (
            <Text key={s} variant="body">
              · {s}
            </Text>
          ))}
        </View>
      ) : (
        <Button title="提交，看结构" disabled={!text.trim()} loading={check.isPending} onPress={() => check.mutate()} />
      )}
    </Card>
  );
}

const styles = StyleSheet.create({
  scroll: { gap: spacing.md, paddingBottom: spacing.xl },
  gap: { gap: spacing.sm },
  flex: { flex: 1 },
  element: { flexDirection: 'row', alignItems: 'center', gap: 12 },
  num: { width: 22, height: 22, borderRadius: 11, alignItems: 'center', justifyContent: 'center', backgroundColor: semantic.primary },
  row: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  bold: { fontWeight: '700' },
  lh: { lineHeight: 23 },
  tinted: { gap: 8, padding: 16, borderRadius: radius.xl },
  good: { backgroundColor: semantic.masteredSoft },
  bad: { backgroundColor: semantic.dangerSoft },
  tags: { flexDirection: 'row', flexWrap: 'wrap', gap: spacing.xs },
  input: { minHeight: 160, borderWidth: 1, borderColor: semantic.border, borderRadius: radius.xl, padding: 14, fontSize: 15, lineHeight: 26, color: semantic.textPrimary, backgroundColor: semantic.surface },
});
