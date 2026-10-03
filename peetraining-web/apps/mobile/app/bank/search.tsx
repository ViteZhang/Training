// 3.2 搜索：在当前专业课内搜索；结果分知识点、题目、资料原文三组，匹配高亮；显示最近搜索。
import type { Schemas } from '@training/api-client';
import { layout, radius, semantic, spacing } from '@training/ui-tokens';
import { useQuery } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, TextInput, View } from 'react-native';
import { Button, EmptyState, ErrorState, Loading, Screen, Text } from '@/components';
import { qtypeNames } from '@/features/import/api';
import { api, unwrap } from '@/lib/api';
import { storage } from '@/lib/storage';
import { Marked } from '@/features/bank/Marked';

const recentKey = (s: number) => `bank_recent_search_${s}`;

function loadRecent(s: number): string[] {
  try {
    return JSON.parse(storage.getString(recentKey(s)) ?? '[]') as string[];
  } catch {
    return [];
  }
}

function Hit({ hit }: { hit: Schemas['SearchHit'] }) {
  return <Marked text={hit.text} highlights={hit.highlights} low={[]} />;
}

export default function SearchScreen() {
  const subjectId = Number(useLocalSearchParams<{ subjectId: string }>().subjectId);
  const [input, setInput] = useState('');
  const [q, setQ] = useState('');
  const [recent, setRecent] = useState(() => loadRecent(subjectId));
  const res = useQuery({
    queryKey: ['bank', subjectId, 'search', q],
    queryFn: () => unwrap(api.GET('/subjects/{subjectId}/search', { params: { path: { subjectId }, query: { q } } })),
    enabled: q.length > 0,
  });
  const submit = (v: string) => {
    const t = v.trim().slice(0, 64);
    if (!t) return;
    setInput(t);
    setQ(t);
    const next = [t, ...recent.filter((x) => x !== t)].slice(0, 10);
    setRecent(next);
    storage.set(recentKey(subjectId), JSON.stringify(next));
  };
  const r = res.data;
  return (
    <Screen>
      <View style={styles.bar}>
        <TextInput
          accessibilityLabel="搜索"
          autoFocus
          placeholder="搜索知识点、题目或原文"
          value={input}
          onChangeText={setInput}
          onSubmitEditing={() => submit(input)}
          returnKeyType="search"
          style={styles.input}
          maxFontSizeMultiplier={layout.maxFontScale}
        />
        <Button title="取消" kind="text" onPress={() => router.back()} />
      </View>
      <ScrollView contentContainerStyle={styles.scroll} keyboardShouldPersistTaps="handled">
        {!q ? (
          <View style={styles.gap}>
            {recent.length > 0 ? <Text variant="caption">最近搜索</Text> : null}
            <View style={styles.row}>
              {recent.map((t) => (
                <Button key={t} title={t} kind="secondary" onPress={() => submit(t)} />
              ))}
            </View>
          </View>
        ) : res.isLoading ? (
          <Loading rows={4} />
        ) : res.isError ? (
          <ErrorState error={res.error} onRetry={() => void res.refetch()} />
        ) : r && r.knowledge_points.length + r.questions.length + r.material_pages.length === 0 ? (
          <EmptyState title={`没有找到「${q}」`} desc="换个关键词试试" />
        ) : r ? (
          <>
            {r.knowledge_points.length > 0 ? <Text variant="bodyStrong">知识点 · {r.knowledge_points.length}</Text> : null}
            {r.knowledge_points.map((k) => (
              <Pressable key={k.id} accessibilityRole="button" onPress={() => router.push({ pathname: '/bank/kp/[id]', params: { id: String(k.id), subjectId: String(subjectId) } })} style={styles.item}>
                <Hit hit={k.hit} />
                {k.path?.length ? <Text variant="caption">{k.path.join(' · ')}</Text> : null}
              </Pressable>
            ))}
            {r.questions.length > 0 ? <Text variant="bodyStrong">题目 · {r.questions.length}</Text> : null}
            {r.questions.map((x) => (
              <Pressable key={x.id} accessibilityRole="button" onPress={() => router.push({ pathname: '/bank/question/[id]', params: { id: String(x.id) } })} style={styles.item}>
                <Text variant="caption">{qtypeNames[x.qtype]}</Text>
                <Hit hit={x.hit} />
              </Pressable>
            ))}
            {r.material_pages.length > 0 ? <Text variant="bodyStrong">资料原文 · {r.material_pages.length}</Text> : null}
            {r.material_pages.map((p) => (
              <Pressable
                key={`${p.material_id}-${p.page_no}`}
                accessibilityRole="button"
                onPress={() => router.push({ pathname: '/bank/page', params: { materialId: String(p.material_id), page: String(p.page_no), highlight: q } })}
                style={styles.item}
              >
                <Hit hit={p.hit} />
                <Text variant="caption">
                  {p.file_name} · 第 {p.page_no} 页
                </Text>
              </Pressable>
            ))}
          </>
        ) : null}
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  bar: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, paddingVertical: spacing.sm },
  input: { flex: 1, minHeight: 44, borderRadius: radius.lg, paddingHorizontal: spacing.md, fontSize: 16, color: semantic.textPrimary, backgroundColor: semantic.surface, borderWidth: 1, borderColor: semantic.border },
  scroll: { paddingBottom: spacing.xl, gap: spacing.sm },
  gap: { gap: spacing.sm },
  row: { flexDirection: 'row', flexWrap: 'wrap', gap: spacing.xs },
  item: { paddingVertical: spacing.sm, gap: 2, borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: semantic.border },
});
