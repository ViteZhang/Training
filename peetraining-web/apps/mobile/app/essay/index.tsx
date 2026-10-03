// 5.1 作文训练：本周目标篇数与完成情况、平均分；当前评分标准及来源（→ 5.9）；三个 Tab：真题题目（年份、已写稿数、最高分、附了几篇范文）、
// AI 命题（标「AI 出题」，可「再出一道」）、自拟题目（粘贴或拍照识别）；作文本入口。
// 5.1b 还没导入资料：引导导入作文资料；没有资料也可以 AI 命题或自拟先写，按通用五维度评分，分数只作参考、不计入预估分。
import { radius, semantic, spacing } from '@training/ui-tokens';
import { router, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, TextInput, View } from 'react-native';
import { BottomSheet, Button, Card, ErrorState, Loading, QuotaSheet, Screen, Tag, Text, toast } from '@/components';
import { PageHeader, Segments } from '@/features/import/ui';
import { fmtScore, useCreateEssay, useEssayHome, useGenerateTopic } from '@/features/essay/api';
import { HandwritingFlow } from '@/features/practice/handwriting';

type Tab = 'exam' | 'ai' | 'custom';

function TopicRow({ title, desc, tags, action, onPress, busy }: { title: string; desc: string; tags?: React.ReactNode; action: string; onPress: () => void; busy?: boolean }) {
  return (
    <View style={styles.topic}>
      <View style={styles.row}>
        {tags}
        <View style={styles.flex} />
        <Button title={action} kind="text" loading={busy} onPress={onPress} />
      </View>
      <Text variant="body">{title}</Text>
      <Text variant="caption">{desc}</Text>
    </View>
  );
}

export default function EssayHomePage() {
  const { subjectId } = useLocalSearchParams<{ subjectId: string }>();
  const sid = Number(subjectId);
  const home = useEssayHome(sid);
  const create = useCreateEssay();
  const [quotaOut, setQuotaOut] = useState(false);
  const gen = useGenerateTopic(sid, () => setQuotaOut(true));
  const [tab, setTab] = useState<Tab>();
  const [custom, setCustom] = useState('');
  const [photo, setPhoto] = useState(false);

  if (home.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (home.isError || !home.data) return <Screen><ErrorState error={home.error} onRetry={() => void home.refetch()} /></Screen>;
  const h = home.data;
  const current: Tab = tab ?? (h.exam_topics.length > 0 ? 'exam' : 'ai');
  const generic = h.rubric.source === 'generic';
  const busyId = create.isPending ? create.variables : undefined;

  const header = (
    <PageHeader title="作文" onBack={() => router.back()} right={<Button title="作文本" kind="text" onPress={() => router.push({ pathname: '/essay/book', params: { subjectId: String(sid) } })} />} />
  );

  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll} keyboardShouldPersistTaps="handled">
        {header}
        {h.no_material ? (
          <Card style={styles.gap}>
            <Text variant="h3">导入作文资料，练得更准</Text>
            <Text variant="caption">历年作文真题、评分细则、范文、素材笔记都可以。有了真题，AI 才知道你的学校怎么命题、怎么给分</Text>
            <Button title="导入作文资料" onPress={() => router.push({ pathname: '/import', params: { subjectId: String(sid) } })} />
            <Text variant="small">没有资料也可以先写：没导入评分细则时，按通用五维度评分（立意、结构、内容与论证、语言、文采各 30 分），分数只作参考，不计入预估分</Text>
          </Card>
        ) : (
          <Card style={styles.gap}>
            <Text variant="bodyStrong">本周目标 {h.weekly_goal} 篇</Text>
            <Text variant="caption">
              已完成 {h.week_done}
              {h.avg_score !== undefined ? ` · 平均 ${fmtScore(h.avg_score)} 分` : ''}
              {h.weekly_remaining !== null && h.weekly_remaining !== undefined ? ` · 本周还能批改 ${h.weekly_remaining} 篇` : ''}
            </Text>
          </Card>
        )}
        <Pressable accessibilityRole="button" onPress={() => router.push({ pathname: '/essay/rubric', params: { subjectId: String(sid) } })} style={styles.rubric}>
          <Text variant="caption" style={styles.flex}>
            评分标准：{generic ? '通用五维度（分数只作参考）' : `按你资料里的评分细则（满分 ${h.rubric.full_score}）`}
          </Text>
          <Text variant="caption" color={semantic.primary}>
            查看 ›
          </Text>
        </Pressable>

        {h.drafts.length > 0 ? (
          <Card style={styles.gap}>
            <Text variant="bodyStrong">没写完的</Text>
            {h.drafts.map((d) => (
              <Pressable key={d.id} accessibilityRole="button" onPress={() => router.push({ pathname: '/essay/write/[id]', params: { id: String(d.id) } })} style={styles.draft}>
                <Text variant="body" numberOfLines={1} style={styles.flex}>
                  {d.topic}
                </Text>
                <Text variant="caption">{d.status === 'failed' ? '批改失败，重新提交' : `${d.word_count} 字 · 继续写`}</Text>
              </Pressable>
            ))}
          </Card>
        ) : null}

        <Segments<Tab>
          options={[
            { key: 'exam', label: '真题题目', count: h.exam_topics.length },
            { key: 'ai', label: 'AI 命题' },
            { key: 'custom', label: '自拟题目' },
          ]}
          value={current}
          onChange={setTab}
        />

        {current === 'exam' ? (
          h.exam_topics.length === 0 ? (
            <Card style={styles.gap}>
              <Text variant="caption">还没有作文真题。导入历年作文真题后，按年份列在这里</Text>
              <Button title="导入作文真题" kind="secondary" onPress={() => router.push({ pathname: '/import', params: { subjectId: String(sid) } })} />
            </Card>
          ) : (
            h.exam_topics.map((t) => (
              <TopicRow
                key={t.id}
                title={t.stem}
                tags={t.exam_year ? <Tag label={`${t.exam_year} 真题`} /> : <Tag label="真题" />}
                desc={[
                  t.drafts > 0 ? `已写 ${t.drafts} 稿` : '未写',
                  t.best !== undefined ? `最高 ${fmtScore(t.best)} 分` : '',
                  t.model_essay_count > 0 ? `附 ${t.model_essay_count} 篇你导入的范文` : '',
                ]
                  .filter(Boolean)
                  .join(' · ')}
                action="去写"
                busy={busyId?.question_id === t.id}
                onPress={() => create.mutate({ subject_id: sid, topic_source: 'exam', question_id: t.id })}
              />
            ))
          )
        ) : null}

        {current === 'ai' ? (
          <>
            <Text variant="caption">AI 按你真题的命题方式出题{h.exam_topics.length === 0 ? '（没有真题时按常见的考研作文命题方式）' : ''}，每出一道计 1 道 AI 出题</Text>
            {h.ai_topics.map((t) => (
              <TopicRow
                key={t.id}
                title={t.topic}
                tags={<Tag label="AI 出题" tone="ai" />}
                desc={`${t.drafts ? `已写 ${t.drafts} 篇` : '未写'} · 不少于 ${t.required_words} 字`}
                action="去写"
                busy={busyId?.ai_topic_id === t.id}
                onPress={() => create.mutate({ subject_id: sid, topic_source: 'ai', ai_topic_id: t.id })}
              />
            ))}
            <Button title={h.ai_topics.length ? '再出一道' : 'AI 出一道题'} kind="secondary" loading={gen.isPending} onPress={() => gen.mutate()} />
          </>
        ) : null}

        {current === 'custom' ? (
          <Card style={styles.gap}>
            <Text variant="caption">手里有模拟题、老师布置的题目，贴进来就能写、能批改</Text>
            <TextInput
              accessibilityLabel="自拟题目"
              multiline
              value={custom}
              onChangeText={setCustom}
              placeholder="粘贴题目与写作要求"
              placeholderTextColor={semantic.textSecondary}
              style={styles.input}
              textAlignVertical="top"
            />
            <View style={styles.row}>
              <Button title="拍照识别题目" kind="secondary" style={styles.flex} onPress={() => setPhoto(true)} />
              <Button
                title="开始写"
                style={styles.flex}
                disabled={custom.trim().length < 4}
                loading={create.isPending && create.variables?.topic_source === 'custom'}
                onPress={() => create.mutate({ subject_id: sid, topic_source: 'custom', topic_text: custom.trim() })}
              />
            </View>
          </Card>
        ) : null}
      </ScrollView>
      <BottomSheet visible={photo} onClose={() => setPhoto(false)} title="拍照识别题目">
        <HandwritingFlow
          onConfirm={(text) => {
            setCustom(text);
            setPhoto(false);
            toast('已识别，核对后开始写');
          }}
          onCancel={() => setPhoto(false)}
        />
      </BottomSheet>
      <QuotaSheet
        visible={quotaOut}
        onClose={() => setQuotaOut(false)}
        title="今天的 AI 出题次数用完了"
        desc="免费版每天可让 AI 出 20 道题，明天 0 点恢复。可以先写真题或自拟题目。"
        onUpgrade={() => toast('会员马上上线')}
        freeOptions={[{ label: '写自拟题目', onPress: () => setTab('custom') }]}
      />
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { gap: spacing.md, paddingBottom: spacing.xl },
  gap: { gap: spacing.sm },
  flex: { flex: 1 },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  rubric: { flexDirection: 'row', alignItems: 'center', minHeight: 44, paddingHorizontal: spacing.md, borderRadius: radius.md, backgroundColor: semantic.surface },
  topic: { gap: spacing.xs, padding: spacing.md, borderRadius: radius.lg, backgroundColor: semantic.surface, borderWidth: 1, borderColor: semantic.border },
  draft: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, minHeight: 44 },
  input: { minHeight: 120, padding: spacing.md, borderRadius: radius.md, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface, fontSize: 16, lineHeight: 24 },
});
