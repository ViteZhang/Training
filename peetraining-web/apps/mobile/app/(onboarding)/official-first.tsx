// 1.1b 首题 · 做一道官方真题（PRD v3 增补，设计稿 n_official_first）。2026-11-10 起 1.1a 添加官方题库后直接进 1.2，引导不再进这一页。
// 原说明：从官方题库取一道名词解释真题（高频考点优先），
// 标「官方」与年份、题型、分值；打字或拍手写稿；提交后显示 4.7 批改结果；右上角可跳过（进 3.1e 题库）。
// 这次批改不扣额度、不占体验次数（服务端按 official_first 会话处理）。批改后进 1.2、1.3（可跳过），再去题库。
import { semantic, spacing } from '@training/ui-tokens';
import { useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { ScrollView, StyleSheet, View } from 'react-native';
import { Button, ErrorState, Loading, NavBar, Screen, Tag, Text } from '@/components';
import { afterFirstGrade, qtypeScore } from '@/features/official/api';
import { DisputeSheet, GradingResultView, useRegrade, type GradingResult } from '@/features/practice/grading';
import { useSession } from '@/features/practice/api';
import { SubjectiveFlow } from '@/features/practice/SubjectiveFlow';
import { track } from '@/lib/analytics';

export default function OfficialFirst() {
  const { sessionId } = useLocalSearchParams<{ sessionId: string }>();
  const id = Number(sessionId);
  const session = useSession(id);
  const [grading, setGrading] = useState<GradingResult>();
  const [done, setDone] = useState(false);
  const [disputing, setDisputing] = useState(false);
  const regrade = useRegrade((g) => setGrading(g));
  const leave = () => void afterFirstGrade('/(tabs)/bank');

  if (session.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (session.isError || !session.data) return <Screen><ErrorState error={session.error} onRetry={() => void session.refetch()} /></Screen>;
  const q = session.data.questions[0];
  if (!q) return <Screen><ErrorState error={new Error('没有取到题目')} onRetry={() => void session.refetch()} /></Screen>;

  const graded = (g: GradingResult) => {
    setGrading(g);
    track('first_grade_done', { source: 'official_first' });
  };

  return (
    <Screen>
      <NavBar back={false} title="先做一道真题，感受一下 AI 批改" right={!grading && !done ? <Button title="跳过" kind="text" size="sm" onPress={leave} /> : undefined} />
      <ScrollView contentContainerStyle={styles.scroll}>
        <View style={styles.meta}>
          <Tag label="官方" tone="mastered" />
          <Text variant="caption">
            {q.exam_year ? `${q.exam_year} 真题 · ` : ''}
            {qtypeScore(q.qtype, q.score ?? 0)}
          </Text>
        </View>
        <Text variant="h2">{q.stem.replace(/^名词解释[：:]/, '')}</Text>
        {q.qtype === 'term' && !grading ? <Text variant="caption">名词解释：写出概念出处、核心含义和关键特征。建议 3–5 分钟。</Text> : null}
        {grading ? (
          <>
            <GradingResultView
              g={grading}
              kpId={q.knowledge_points[0]?.id}
              norm={{ subjectId: session.data.subject_id, qtype: q.qtype }}
              onDispute={() => setDisputing(true)}
              onRegrade={() => regrade.mutate(grading.grading_id)}
              regrading={regrade.isPending}
            />
            <DisputeSheet g={grading} visible={disputing} onClose={() => setDisputing(false)} onDone={setGrading} />
          </>
        ) : done ? (
          <Text variant="body" color={semantic.info}>已记下。官方题库已经加进题库，可以接着练。</Text>
        ) : (
          <SubjectiveFlow q={q} sessionId={id} onGraded={graded} onQueued={() => setDone(true)} onSelfAssess={() => setDone(true)} />
        )}
      </ScrollView>
      {grading || done ? (
        <View style={styles.footer}>
          <Button title="继续" onPress={leave} />
        </View>
      ) : null}
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing.xl, gap: spacing.md },
  meta: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  footer: { paddingTop: spacing.md, paddingBottom: spacing.xxl },
});
