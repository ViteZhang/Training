// 3.10 作文知识库：分栏 作文知识库 / 作文题 / 资料。写作方法（AI 从笔记和范文归纳，标出处，带掌握状态）、
// 素材库（按主题，可收藏，标真题考过次数，AI 补充的单独标）、范文（按真题题目归类，附结构拆解）；当前评分标准。
import type { Schemas } from '@training/api-client';
import { ApiError } from '@training/api-client';
import { colors, radius, semantic, spacing } from '@training/ui-tokens';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { router } from 'expo-router';
import { useState } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { Button, Card, EmptyState, ErrorState, Icon, Loading, Tag, Text, toast, UnderlineTabs } from '@/components';
import { api, unwrap } from '@/lib/api';
import { useMaterials } from './api';
import { MasteryPill } from './MasteryPill';
import { MaterialList } from './MaterialList';

type KB = Schemas['EssayKnowledgeBase'];
type Tab = 'kb' | 'topics' | 'material';

function where(src?: Schemas['SourceRef']) {
  if (!src) return undefined;
  return `出自${src.file_name.replace(/\.[^.]+$/, '')}${src.page ? ` 第 ${src.page} 页` : ''}`;
}

function Knowledge({ kb, subjectId }: { kb: KB; subjectId: number }) {
  const qc = useQueryClient();
  const [open, setOpen] = useState<string | null>(null);
  const themes = [...new Set(kb.materials.map((m) => m.theme))];
  const toggle = async (id: number, favorite: boolean) => {
    try {
      await unwrap(api.PUT('/essay-materials/{essayMaterialId}/favorite', { params: { path: { essayMaterialId: id } }, body: { favorite } }));
      await qc.invalidateQueries({ queryKey: ['bank', subjectId, 'essay-kb'] });
    } catch (e) {
      toast(e instanceof ApiError ? e.message : '没收藏成功，请重试');
    }
  };
  return (
    <View style={styles.gap}>
      {kb.rubric ? (
        <Pressable accessibilityRole="button" onPress={() => router.push({ pathname: '/essay/rubric', params: { subjectId: String(subjectId) } })} style={styles.rubric}>
          <View style={[styles.flex, styles.gap2]}>
            <Text variant="caption" color={colors.ink} style={styles.bold}>
              评分标准 · {kb.rubric.source === 'generic' ? '通用标准，只作参考' : '来自你的资料'}
            </Text>
            <Text variant="small">
              {kb.rubric.dimensions.map((d) => `${d.name} ${d.score}`).join(' · ')} · 满分 {kb.rubric.full_score}
            </Text>
          </View>
          <Icon name="chevron" size={16} color={semantic.textSecondary} />
        </Pressable>
      ) : null}
      <Card style={styles.section}>
        <View style={styles.head}>
          <View style={[styles.badge, { backgroundColor: semantic.amberSoft }]}>
            <Icon name="pen" size={18} color="#8A4B12" />
          </View>
          <Text variant="h3" style={styles.flex}>
            写作方法
          </Text>
          <Tag label="AI 归纳" tone="ai" />
        </View>
        {kb.methods.length === 0 ? <Text variant="small" style={styles.pad}>导入写作笔记或范文后，AI 会归纳写作方法</Text> : null}
        {kb.methods.map((m) => (
          <Pressable key={m.id} accessibilityRole="button" accessibilityState={{ expanded: open === `m${m.id}` }} onPress={() => setOpen(open === `m${m.id}` ? null : `m${m.id}`)} style={styles.item}>
            <View style={styles.row}>
              <View style={[styles.flex, styles.gap2]}>
                <Text variant="body">{m.title}</Text>
                <Text variant="small">{[m.dimension, where(m.source)].filter(Boolean).join(' · ')}</Text>
              </View>
              <MasteryPill state={m.state} />
            </View>
            {open === `m${m.id}` ? (
              <Text variant="caption" color={colors.ink} style={styles.lh}>
                {m.content}
              </Text>
            ) : null}
          </Pressable>
        ))}
      </Card>
      <Card style={styles.section}>
        <View style={styles.head}>
          <View style={[styles.badge, { backgroundColor: semantic.fill }]}>
            <Icon name="book" size={18} />
          </View>
          <Text variant="h3" style={styles.flex}>
            素材库
          </Text>
          <Text variant="small">按主题 · 可收藏</Text>
        </View>
        {themes.length === 0 ? <Text variant="small" style={styles.pad}>导入素材笔记后按主题整理</Text> : null}
        {themes.map((t) => {
          const items = kb.materials.filter((m) => m.theme === t);
          const fav = items.filter((m) => m.favorite).length;
          const exam = items.reduce((n, m) => n + m.exam_count, 0);
          const isOpen = open === `t${t}`;
          return (
            <View key={t} style={styles.item}>
              <Pressable accessibilityRole="button" accessibilityState={{ expanded: isOpen }} onPress={() => setOpen(isOpen ? null : `t${t}`)} style={styles.row}>
                <View style={[styles.flex, styles.gap2]}>
                  <Text variant="body">{t}</Text>
                  <Text variant="small">
                    {items.length} 条{fav ? ` · 已收藏 ${fav}` : ''}
                    {exam ? ` · 真题考过 ${exam} 次` : ''}
                  </Text>
                </View>
                <View style={isOpen ? styles.down : undefined}>
                  <Icon name="chevron" size={16} color={semantic.textSecondary} />
                </View>
              </Pressable>
              {isOpen
                ? items.map((m) => (
                    <View key={m.id} style={styles.row}>
                      <Text variant="caption" color={colors.ink} style={[styles.flex, styles.lh]}>
                        {m.content}
                        {m.origin === 'ai_generated' ? <Text variant="small" color="#3E3190"> AI 补充</Text> : null}
                      </Text>
                      <Button title={m.favorite ? '已收藏' : '收藏'} kind="text" size="sm" color={m.favorite ? colors.ink : undefined} onPress={() => void toggle(m.id, !m.favorite)} />
                    </View>
                  ))
                : null}
            </View>
          );
        })}
      </Card>
      <View style={styles.models}>
        <Text variant="bodyStrong" color={modelInk} style={styles.bold}>
          范文 · {kb.model_essays.length} 篇
        </Text>
        <Text variant="small" color={modelInk}>
          来自你导入的范文，按真题题目归类，附结构拆解，只有你自己能看到
        </Text>
        {kb.model_essays.map((e) => (
          <View key={e.id} style={styles.model}>
            <Pressable accessibilityRole="button" onPress={() => router.push({ pathname: '/essay/model/[id]', params: { id: String(e.id) } })} style={styles.row}>
              <View style={[styles.flex, styles.gap2]}>
                <Text variant="caption" color={colors.ink}>
                  《{e.title}》
                </Text>
                <Text variant="small">{[e.topic ? `题目：${e.topic}` : '', where(e.source)].filter(Boolean).join(' · ')}</Text>
              </View>
              <Icon name="chevron" size={16} color={modelInk} />
            </Pressable>
            {open === `e${e.id}` && e.structure ? (
              <View style={styles.gap2}>
                {e.structure.opening ? <Text variant="small">开头立意：{e.structure.opening}</Text> : null}
                {e.structure.points?.map((pt, i) => (
                  <Text key={i} variant="small">
                    分论点 {i + 1}：{pt}
                  </Text>
                ))}
                {e.structure.elevation ? <Text variant="small">升华：{e.structure.elevation}</Text> : null}
                {e.structure.ending ? <Text variant="small">结尾：{e.structure.ending}</Text> : null}
              </View>
            ) : e.structure ? (
              <Button title="看结构拆解" kind="text" size="sm" color={modelInk} style={styles.left} onPress={() => setOpen(`e${e.id}`)} />
            ) : null}
          </View>
        ))}
      </View>
      <Text variant="small" style={styles.center}>
        写作方法和素材分类由 AI 从你的笔记和范文里整理，每一条都能查看原文出处
      </Text>
    </View>
  );
}

const modelInk = '#7A4E00';

export function EssayKnowledgeBase({ subjectId, label }: { subjectId: number; label: string }) {
  const [tab, setTab] = useState<Tab>('kb');
  const kb = useQuery({
    queryKey: ['bank', subjectId, 'essay-kb'],
    queryFn: () => unwrap(api.GET('/subjects/{subjectId}/essay-kb', { params: { path: { subjectId } } })),
  });
  const mats = useMaterials(subjectId);
  if (kb.isLoading) return <Loading rows={5} />;
  if (kb.isError || !kb.data) return <ErrorState error={kb.error} onRetry={() => void kb.refetch()} />;
  const d = kb.data;
  const empty = d.methods.length + d.materials.length + d.model_essays.length + d.topics.length === 0;
  return (
    <View style={styles.gap}>
      <UnderlineTabs<Tab>
        value={tab}
        onChange={setTab}
        options={[
          { key: 'kb', label: '作文知识库' },
          { key: 'topics', label: '作文题', count: d.topics.length },
          { key: 'material', label: '资料', count: mats.data?.items.length },
        ]}
      />
      {tab === 'kb' ? (
        empty ? (
          <EmptyState
            title={`${label}还没有资料`}
            desc="导入历年作文真题、范文或写作笔记，AI 帮你整理成作文知识库"
            actionText="导入资料"
            onAction={() => router.push({ pathname: '/import', params: { subjectId: String(subjectId) } })}
          />
        ) : (
          <Knowledge kb={d} subjectId={subjectId} />
        )
      ) : null}
      {tab === 'topics' ? (
        d.topics.length === 0 ? (
          <EmptyState title="还没有作文题" desc="导入历年作文真题后按年份列在这里" />
        ) : (
          <Card style={styles.section}>
            {d.topics.map((t, i) => (
              <View key={t.id} style={[styles.item, i === 0 && styles.first]}>
                <Text variant="body">{t.stem}</Text>
                <Text variant="caption">
                  {[t.exam_year ? `${t.exam_year} 真题` : '', t.required_words ? `不少于 ${t.required_words} 字` : '', t.model_essay_count ? `附 ${t.model_essay_count} 篇范文` : '']
                    .filter(Boolean)
                    .join(' · ')}
                </Text>
              </View>
            ))}
          </Card>
        )
      ) : null}
      {tab === 'material' ? mats.data?.items.length ? <MaterialList items={mats.data.items} subjectId={subjectId} /> : <EmptyState title="还没有资料" /> : null}
    </View>
  );
}

const styles = StyleSheet.create({
  gap: { gap: 12 },
  gap2: { gap: 2 },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  flex: { flex: 1 },
  bold: { fontWeight: '700' },
  lh: { lineHeight: 21 },
  center: { textAlign: 'center' },
  left: { alignSelf: 'flex-start', paddingHorizontal: 0 },
  down: { transform: [{ rotate: '90deg' }] },
  pad: { paddingHorizontal: 18, paddingBottom: 14 },
  rubric: { flexDirection: 'row', alignItems: 'center', gap: 10, padding: 16, borderRadius: radius.xl, backgroundColor: semantic.fill },
  section: { paddingHorizontal: 0, paddingVertical: 0, overflow: 'hidden' },
  head: { flexDirection: 'row', alignItems: 'center', gap: 12, paddingHorizontal: 18, paddingVertical: 14 },
  badge: { width: 36, height: 36, borderRadius: 12, alignItems: 'center', justifyContent: 'center' },
  item: { gap: 8, paddingHorizontal: 18, paddingVertical: 12, borderTopWidth: 1, borderTopColor: semantic.border },
  first: { borderTopWidth: 0 },
  models: { gap: 6, padding: 16, borderRadius: radius.xl, backgroundColor: semantic.amberSoft },
  model: { gap: 4, paddingTop: 8 },
});
