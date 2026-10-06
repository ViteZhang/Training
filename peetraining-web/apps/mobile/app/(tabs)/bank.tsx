// 3.1 题库：大标题 + 考情分析 / 图谱 / 导入 +「添加官方题库」（official_bank 开关，T30）+ 专业课切换 + 知识点 / 题目 / 资料三个分栏。作文课的作文知识库（3.10）在 T14。
import { radius, semantic, spacing } from '@training/ui-tokens';
import { useQuery } from '@tanstack/react-query';
import { router } from 'expo-router';
import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { Button, Card, EmptyState, ErrorState, Icon, Loading, Screen, Segmented, Text, UnderlineTabs } from '@/components';
import { useOverview, useMaterials } from '@/features/bank/api';
import { EssayKnowledgeBase } from '@/features/bank/EssayKB';
import { MaterialList } from '@/features/bank/MaterialList';
import { QuestionList } from '@/features/bank/QuestionList';
import { ParseQuotaBar } from '@/features/bank/QuotaBar';
import { KnowledgeTree } from '@/features/bank/Tree';
import { Segments } from '@/features/import/ui';
import { api, unwrap } from '@/lib/api';
import { useFeatureFlag } from '@/lib/flags';

type Tab = 'kp' | 'question' | 'material';
type TreeFilter = 'all' | 'unmastered' | 'exam' | 'needs_review';

function KnowledgeTab({ subjectId }: { subjectId: number }) {
  const [filter, setFilter] = useState<TreeFilter>('all');
  const tree = useQuery({
    queryKey: ['bank', subjectId, 'tree', filter],
    queryFn: () => unwrap(api.GET('/subjects/{subjectId}/knowledge-tree', { params: { path: { subjectId }, query: { filter } } })),
  });
  if (tree.isLoading) return <Loading rows={5} />;
  if (tree.isError) return <ErrorState error={tree.error} onRetry={() => void tree.refetch()} />;
  const data = tree.data!;
  return (
    <View style={styles.gap}>
      <Segments<TreeFilter>
        value={filter}
        onChange={setFilter}
        options={[
          { key: 'all', label: '全部' },
          { key: 'unmastered', label: '未掌握' },
          { key: 'exam', label: '真题考过' },
          { key: 'needs_review', label: '待核对' },
        ]}
      />
      {data.needs_review_count > 0 && filter !== 'needs_review' ? (
        <Pressable accessibilityRole="button" onPress={() => setFilter('needs_review')} style={styles.banner}>
          <View style={styles.dot} />
          <Text variant="caption" color="#6B4A12" style={styles.flex}>
            <Text variant="caption" color="#6B4A12" style={styles.bold}>
              {data.needs_review_count} 处待核对
            </Text>{' '}
            · 采分点核对后批改更准
          </Text>
          <Icon name="chevron" size={16} color="#6B4A12" />
        </Pressable>
      ) : null}
      {data.sections.length === 0 ? (
        <EmptyState title={filter === 'all' ? '还没有知识点' : '这一类没有知识点'} desc={filter === 'all' ? '导入讲义、笔记或真题后，AI 会整理成知识点' : undefined} />
      ) : (
        <KnowledgeTree sections={data.sections} expandAll={filter !== 'all'} subjectId={subjectId} />
      )}
      <Text variant="small" color={semantic.textSecondary}>
        板块和章节由 AI 按你资料的目录整理，可以在知识点里调整归属
      </Text>
    </View>
  );
}

function MaterialTab({ subjectId, label }: { subjectId: number; label: string }) {
  const mats = useMaterials(subjectId);
  if (mats.isLoading) return <Loading rows={3} />;
  if (mats.isError) return <ErrorState error={mats.error} onRetry={() => void mats.refetch()} />;
  return (
    <View style={styles.gap}>
      {mats.data!.items.length === 0 ? <EmptyState title="还没有资料" /> : <MaterialList items={mats.data!.items} subjectId={subjectId} />}
      <Pressable accessibilityRole="button" onPress={() => router.push({ pathname: '/import', params: { subjectId: String(subjectId) } })} style={styles.append}>
        <Text variant="caption" color={semantic.textPrimary}>
          ＋ 追加资料到 {label}
        </Text>
      </Pressable>
      <ParseQuotaBar />
    </View>
  );
}

export default function BankTab() {
  const subjects = useQuery({ queryKey: ['subjects'], queryFn: () => unwrap(api.GET('/subjects')) });
  const [picked, setPicked] = useState<number>();
  const [tab, setTab] = useState<Tab>('kp');
  const list = subjects.data?.items ?? [];
  const subject = list.find((s) => s.id === picked) ?? list[0];
  const overview = useOverview(subject?.id);
  const official = useFeatureFlag('official_bank');

  if (subjects.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (subjects.isError) return <Screen><ErrorState error={subjects.error} onRetry={() => void subjects.refetch()} /></Screen>;
  if (!subject) return <Screen><EmptyState title="还没有专业课" actionText="去添加" onAction={() => router.push('/settings/prep')} /></Screen>;

  const label = `${subject.code ? `${subject.code} ` : ''}${subject.name}`;
  const o = overview.data;
  const empty = o && o.question_count === 0 && o.kp_count === 0 && o.material_count === 0;
  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll} keyboardShouldPersistTaps="handled">
        <View style={styles.title}>
          <Text variant="h1" style={styles.flex}>
            题库
          </Text>
          <Pressable accessibilityRole="button" onPress={() => router.push({ pathname: '/bank/profile', params: { subjectId: String(subject.id) } })} style={[styles.chip, styles.chipGreen]}>
            <Text variant="caption" color="#1F5C3E" style={styles.medium}>
              考情分析
            </Text>
          </Pressable>
          <Pressable accessibilityRole="button" onPress={() => router.push({ pathname: '/bank/graph', params: { subjectId: String(subject.id) } })} style={styles.chip}>
            <Text variant="caption" color={semantic.textPrimary}>
              图谱
            </Text>
          </Pressable>
          <Pressable accessibilityRole="button" accessibilityLabel="导入" onPress={() => router.push({ pathname: '/import', params: { subjectId: String(subject.id) } })} style={[styles.chip, styles.chipInk]}>
            <Text variant="caption" color={semantic.textOnBrand}>
              ＋ 导入
            </Text>
          </Pressable>
        </View>
        {official && !subject.is_essay ? (
          <Pressable accessibilityRole="button" onPress={() => router.push('/bank/official')} style={styles.official}>
            <Text variant="bodyStrong" style={styles.flex}>
              添加官方题库
            </Text>
            <Text variant="caption">和自建的一起练</Text>
          </Pressable>
        ) : null}
        {list.length > 1 ? (
          <Segmented options={list.map((s) => ({ key: s.id, label: `${s.code ? `${s.code} ` : ''}${s.name}` }))} value={subject.id} onChange={setPicked} />
        ) : null}

        {subject.is_essay ? (
          <EssayKnowledgeBase subjectId={subject.id} label={label} />
        ) : empty ? (
          <Card style={styles.gap}>
            <Text variant="h3">{label} 还没有资料</Text>
            <Text variant="body" color={semantic.textSecondary}>
              {subject.is_essay ? '导入历年作文真题、范文或写作笔记，AI 帮你整理成作文知识库' : '导入真题、讲义或笔记，AI 帮你整理成题库'}
            </Text>
            <Button title="导入资料" onPress={() => router.push({ pathname: '/import', params: { subjectId: String(subject.id) } })} />
          </Card>
        ) : (
          <>
            <UnderlineTabs<Tab>
              value={tab}
              onChange={setTab}
              options={[
                { key: 'kp', label: '知识点', count: o?.kp_count },
                { key: 'question', label: '题目', count: o?.question_count },
                { key: 'material', label: '资料', count: o?.material_count },
              ]}
            />
            {tab === 'kp' ? (
              <Pressable accessibilityRole="search" onPress={() => router.push({ pathname: '/bank/search', params: { subjectId: String(subject.id) } })} style={styles.search}>
                <Icon name="search" size={18} color={semantic.textSecondary} />
                <Text variant="caption">搜索知识点、题目或原文</Text>
              </Pressable>
            ) : null}
            {tab === 'kp' ? <KnowledgeTab subjectId={subject.id} /> : null}
            {tab === 'question' ? <QuestionList subjectId={subject.id} /> : null}
            {tab === 'material' ? <MaterialTab subjectId={subject.id} label={label} /> : null}
          </>
        )}
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing.xl, gap: 14 },
  title: { flexDirection: 'row', alignItems: 'center', gap: 6, marginTop: spacing.sm },
  flex: { flex: 1 },
  bold: { fontWeight: '700' },
  medium: { fontWeight: '500' },
  gap: { gap: 12 },
  chip: { minHeight: 34, paddingHorizontal: 12, borderRadius: radius.pill, justifyContent: 'center', backgroundColor: semantic.fill },
  chipGreen: { backgroundColor: semantic.masteredSoft },
  chipInk: { backgroundColor: semantic.primary },
  search: { flexDirection: 'row', alignItems: 'center', gap: 8, minHeight: 44, paddingHorizontal: 16, borderRadius: radius.lg, backgroundColor: semantic.fill },
  official: { flexDirection: 'row', alignItems: 'center', minHeight: 44, paddingHorizontal: spacing.md, borderRadius: radius.lg, backgroundColor: semantic.primarySoft },
  banner: { flexDirection: 'row', alignItems: 'center', gap: 10, minHeight: 44, paddingVertical: 12, paddingHorizontal: 16, borderRadius: radius.xl, backgroundColor: semantic.amberSoft },
  dot: { width: 8, height: 8, borderRadius: 4, backgroundColor: '#B26A00' },
  append: { minHeight: 46, alignItems: 'center', justifyContent: 'center', borderRadius: radius.lg, borderWidth: 1, borderStyle: 'dashed', borderColor: '#D6D2C8' },
});
