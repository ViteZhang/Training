// 3.10 作文知识库：分栏 作文知识库 / 作文题 / 资料。写作方法（AI 从笔记和范文归纳，标出处，带掌握状态）、
// 素材库（按主题，可收藏，标真题考过次数，AI 补充的单独标）、范文（按真题题目归类，附结构拆解）；当前评分标准。
import type { Schemas } from '@training/api-client';
import { ApiError } from '@training/api-client';
import { semantic, spacing } from '@training/ui-tokens';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { router } from 'expo-router';
import { useState } from 'react';
import { StyleSheet, View } from 'react-native';
import { Button, Card, EmptyState, ErrorState, Loading, Tag, Text, toast } from '@/components';
import { Segments } from '@/features/import/ui';
import { api, unwrap } from '@/lib/api';
import { stateNames, stateTone, useMaterials } from './api';
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
        <Card style={styles.gap}>
          <View style={styles.row}>
            <Text variant="bodyStrong" style={styles.flex}>
              评分标准
            </Text>
            <Tag label={kb.rubric.source === 'generic' ? '通用标准 · 只作参考' : '你的资料'} tone={kb.rubric.source === 'generic' ? 'neutral' : 'brand'} />
          </View>
          <Text variant="caption">
            {kb.rubric.dimensions.map((d) => `${d.name} ${d.score}`).join(' · ')} · 满分 {kb.rubric.full_score}
          </Text>
        </Card>
      ) : null}
      <Card style={styles.gap}>
        <View style={styles.row}>
          <Text variant="bodyStrong" style={styles.flex}>
            写作方法
          </Text>
          <Tag label="AI 归纳" tone="ai" />
        </View>
        {kb.methods.length === 0 ? <Text variant="caption">导入写作笔记或范文后，AI 会归纳写作方法</Text> : null}
        {kb.methods.map((m) => (
          <View key={m.id} style={styles.item}>
            <View style={styles.row}>
              <Text variant="body" style={styles.flex}>
                {m.title}
              </Text>
              <Tag label={stateNames[m.state]} tone={stateTone[m.state]} />
            </View>
            <Text variant="caption">{[m.dimension, where(m.source)].filter(Boolean).join(' · ')}</Text>
            {open === `m${m.id}` ? <Text variant="body">{m.content}</Text> : <Button title="展开" kind="text" onPress={() => setOpen(`m${m.id}`)} />}
          </View>
        ))}
      </Card>
      <Card style={styles.gap}>
        <View style={styles.row}>
          <Text variant="bodyStrong" style={styles.flex}>
            素材库
          </Text>
          <Text variant="caption">按主题 · 可收藏</Text>
        </View>
        {themes.length === 0 ? <Text variant="caption">导入素材笔记后按主题整理</Text> : null}
        {themes.map((t) => {
          const items = kb.materials.filter((m) => m.theme === t);
          const fav = items.filter((m) => m.favorite).length;
          const exam = items.reduce((s, m) => s + m.exam_count, 0);
          return (
            <View key={t} style={styles.item}>
              <View style={styles.row}>
                <Text variant="body" style={styles.flex}>
                  {t}
                </Text>
                <Text variant="caption">
                  {items.length} 条{fav ? ` · 已收藏 ${fav}` : ''}
                  {exam ? ` · 真题考过 ${exam} 次` : ''}
                </Text>
                <Button title={open === `t${t}` ? '收起' : '查看'} kind="text" onPress={() => setOpen(open === `t${t}` ? null : `t${t}`)} />
              </View>
              {open === `t${t}`
                ? items.map((m) => (
                    <View key={m.id} style={styles.row}>
                      <Text variant="body" style={styles.flex}>
                        {m.content}
                        {m.origin === 'ai_generated' ? '（AI 补充）' : ''}
                      </Text>
                      <Button title={m.favorite ? '已收藏' : '收藏'} kind="text" onPress={() => void toggle(m.id, !m.favorite)} />
                    </View>
                  ))
                : null}
            </View>
          );
        })}
      </Card>
      <Card style={styles.gap}>
        <Text variant="bodyStrong">范文 · {kb.model_essays.length} 篇</Text>
        <Text variant="caption">来自你导入的范文，按真题题目归类，附结构拆解，只有你自己能看到</Text>
        {kb.model_essays.map((e) => (
          <View key={e.id} style={styles.item}>
            <Text variant="body">《{e.title}》</Text>
            <Text variant="caption">{[e.topic ? `题目：${e.topic}` : '', where(e.source)].filter(Boolean).join(' · ')}</Text>
            {open === `e${e.id}` && e.structure ? (
              <View style={styles.gap}>
                {e.structure.opening ? <Text variant="caption">开头立意：{e.structure.opening}</Text> : null}
                {e.structure.points?.map((pt, i) => (
                  <Text key={i} variant="caption">
                    分论点 {i + 1}：{pt}
                  </Text>
                ))}
                {e.structure.elevation ? <Text variant="caption">升华：{e.structure.elevation}</Text> : null}
                {e.structure.ending ? <Text variant="caption">结尾：{e.structure.ending}</Text> : null}
              </View>
            ) : (
              <Button title="看结构拆解" kind="text" onPress={() => setOpen(`e${e.id}`)} />
            )}
          </View>
        ))}
      </Card>
      <Text variant="small" color={semantic.textSecondary}>
        写作方法和素材分类由 AI 从你的笔记和范文里整理，每一条都能查看原文出处
      </Text>
    </View>
  );
}

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
      <Segments<Tab>
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
          <Card style={styles.gap}>
            {d.topics.map((t) => (
              <View key={t.id} style={styles.item}>
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
  gap: { gap: spacing.sm },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  flex: { flex: 1 },
  item: { gap: 2, paddingVertical: spacing.xs, borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: semantic.border },
});
