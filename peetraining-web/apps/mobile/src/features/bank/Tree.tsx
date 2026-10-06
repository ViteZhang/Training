// 3.1 知识点：板块 → 章节 → 知识点三级，板块默认折叠，显示知识点数、待巩固数、平均掌握度，低于 40% 标「短板」；
// 知识点行显示真题出现次数和掌握状态。
import { radius, semantic } from '@training/ui-tokens';
import { router } from 'expo-router';
import { useState } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { Icon, Tag, Text } from '@/components';
import type { KnowledgeNode } from './api';
import { MasteryPill } from './MasteryPill';

function PointRow({ n, subjectId, first }: { n: KnowledgeNode; subjectId: number; first?: boolean }) {
  return (
    <Pressable accessibilityRole="button" accessibilityLabel={n.name} onPress={() => router.push({ pathname: '/bank/kp/[id]', params: { id: String(n.id), subjectId: String(subjectId) } })} style={[styles.point, first && styles.noBorder]}>
      <Text variant="body" numberOfLines={1} style={styles.pointName}>
        {n.name}
      </Text>
      {n.needs_review ? <View style={styles.reviewDot} accessibilityLabel="待核对" /> : null}
      <View style={styles.flex} />
      {n.is_new ? <Tag label="新" tone="progress" /> : null}
      {n.official ? <Tag label="官方" tone="brand" /> : null}
      {n.exam_count ? <Text variant="small">真题 {n.exam_count} 次</Text> : null}
      {n.state ? <MasteryPill state={n.state} /> : null}
    </Pressable>
  );
}

function Section({ n, initialOpen, subjectId, first }: { n: KnowledgeNode; initialOpen: boolean; subjectId: number; first: boolean }) {
  const [open, setOpen] = useState(initialOpen);
  const desc = [`${n.kp_count ?? 0} 个知识点`];
  if (n.is_weak) desc.push('短板');
  else if (n.consolidating_count) desc.push(`${n.consolidating_count} 个待巩固`);
  const pct = Math.round(n.avg_mastery ?? 0);
  return (
    <View style={[styles.section, !first && styles.divider]}>
      <Pressable accessibilityRole="button" accessibilityState={{ expanded: open }} accessibilityLabel={n.name} onPress={() => setOpen(!open)} style={styles.sectionHead}>
        <View style={[styles.flex, styles.gap2]}>
          <Text variant="body" style={styles.medium}>
            {n.name}
          </Text>
          <Text variant="small" color={n.is_weak ? weakInk : semantic.textSecondary}>
            {desc.join(' · ')}
          </Text>
        </View>
        <Text variant="caption" color={n.is_weak ? weakInk : semantic.textPrimary} style={styles.bold}>
          {pct}%
        </Text>
        <View style={open ? styles.up : undefined}>
          <Icon name="down" size={16} color={semantic.textSecondary} />
        </View>
      </Pressable>
      {open
        ? n.children.map((c) =>
            c.level === 'chapter' ? (
              <View key={c.id} style={styles.chapter}>
                <Text variant="small" style={styles.chapterTitle}>
                  {c.name}
                </Text>
                {c.children.map((p, i) => (
                  <PointRow key={p.id} n={p} subjectId={subjectId} first={i === 0} />
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

const weakInk = '#9A5B00';

export function KnowledgeTree({ sections, expandAll, subjectId }: { sections: KnowledgeNode[]; expandAll: boolean; subjectId: number }) {
  return (
    <View style={styles.tree}>
      {sections.map((s, i) =>
        s.level === 'point' ? <PointRow key={s.id} n={s} subjectId={subjectId} first={i === 0} /> : <Section key={s.id} n={s} initialOpen={expandAll} subjectId={subjectId} first={i === 0} />,
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  tree: { backgroundColor: semantic.surface, borderRadius: radius.card, borderWidth: 1, borderColor: semantic.border, overflow: 'hidden' },
  section: { paddingHorizontal: 18 },
  divider: { borderTopWidth: 1, borderTopColor: semantic.border },
  sectionHead: { flexDirection: 'row', alignItems: 'center', gap: 10, minHeight: 56, paddingVertical: 10 },
  up: { transform: [{ rotate: '180deg' }] },
  chapter: { paddingBottom: 4 },
  chapterTitle: { marginTop: 4, marginBottom: 2 },
  point: { flexDirection: 'row', alignItems: 'center', gap: 8, minHeight: 48 },
  noBorder: {},
  pointName: { flexShrink: 1 },
  reviewDot: { width: 6, height: 6, borderRadius: 3, backgroundColor: '#B26A00' },
  flex: { flex: 1 },
  gap2: { gap: 2 },
  medium: { fontWeight: '500' },
  bold: { fontWeight: '700' },
});
