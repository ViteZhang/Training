// 3.1 知识点：板块 → 章节 → 知识点三级，板块默认折叠，显示知识点数、待巩固数、平均掌握度，低于 40% 标「短板」；
// 知识点行显示真题出现次数和掌握状态。
import { semantic, spacing } from '@training/ui-tokens';
import { router } from 'expo-router';
import { useState } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { Tag, Text } from '@/components';
import { stateNames, stateTone, type KnowledgeNode } from './api';

function PointRow({ n, subjectId }: { n: KnowledgeNode; subjectId: number }) {
  return (
    <Pressable accessibilityRole="button" accessibilityLabel={n.name} onPress={() => router.push({ pathname: '/bank/kp/[id]', params: { id: String(n.id), subjectId: String(subjectId) } })} style={styles.point}>
      <Text variant="body" style={styles.flex} numberOfLines={1}>
        {n.name}
      </Text>
      {n.is_new ? <Tag label="新" tone="progress" /> : null}
      {n.official ? <Tag label="官方" tone="brand" /> : null}
      {n.needs_review ? <Tag label="待核对" tone="danger" /> : null}
      {n.exam_count ? <Text variant="caption">真题 {n.exam_count} 次</Text> : null}
      {n.state ? <Tag label={stateNames[n.state]} tone={stateTone[n.state]} /> : null}
    </Pressable>
  );
}

function Section({ n, initialOpen, subjectId }: { n: KnowledgeNode; initialOpen: boolean; subjectId: number }) {
  const [open, setOpen] = useState(initialOpen);
  const desc = [`${n.kp_count ?? 0} 个知识点`];
  if (n.is_weak) desc.push('短板');
  else if (n.consolidating_count) desc.push(`${n.consolidating_count} 个待巩固`);
  return (
    <View style={styles.section}>
      <Pressable accessibilityRole="button" accessibilityState={{ expanded: open }} accessibilityLabel={n.name} onPress={() => setOpen(!open)} style={styles.sectionHead}>
        <View style={styles.flex}>
          <Text variant="bodyStrong">{n.name}</Text>
          <Text variant="caption" color={n.is_weak ? semantic.danger : semantic.textSecondary}>
            {desc.join(' · ')}
          </Text>
        </View>
        <Text variant="number" color={n.is_weak ? semantic.danger : semantic.textPrimary}>
          {Math.round(n.avg_mastery ?? 0)}%
        </Text>
        <Text variant="caption">{open ? '收起' : '展开'}</Text>
      </Pressable>
      {open
        ? n.children.map((c) =>
            c.level === 'chapter' ? (
              <View key={c.id} style={styles.chapter}>
                <Text variant="caption" style={styles.chapterTitle}>
                  {c.name}
                </Text>
                {c.children.map((p) => (
                  <PointRow key={p.id} n={p} subjectId={subjectId} />
                ))}
              </View>
            ) : (
              <PointRow key={c.id} n={c} subjectId={subjectId} />
            ),
          )
        : null}
    </View>
  );
}

export function KnowledgeTree({ sections, expandAll, subjectId }: { sections: KnowledgeNode[]; expandAll: boolean; subjectId: number }) {
  return (
    <View style={styles.tree}>
      {sections.map((s) => (s.level === 'point' ? <PointRow key={s.id} n={s} subjectId={subjectId} /> : <Section key={s.id} n={s} initialOpen={expandAll} subjectId={subjectId} />))}
    </View>
  );
}

const styles = StyleSheet.create({
  tree: { gap: spacing.sm },
  section: { backgroundColor: semantic.surface, borderRadius: 12, borderWidth: StyleSheet.hairlineWidth, borderColor: semantic.border, overflow: 'hidden' },
  sectionHead: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, padding: spacing.md, minHeight: 56 },
  chapter: { paddingHorizontal: spacing.md },
  chapterTitle: { marginTop: spacing.sm },
  point: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, minHeight: 48, paddingHorizontal: spacing.md, borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: semantic.border },
  flex: { flex: 1 },
});
