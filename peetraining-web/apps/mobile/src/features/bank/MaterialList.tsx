// 3.1c 资料、6.3 我的资料共用：每份资料的类型、页数、识别出的题数、导入日期；识别不完整时提醒；删除前说明连带影响（3.1d）。
import { ApiError } from '@training/api-client';
import { radius, semantic, spacing } from '@training/ui-tokens';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { BottomSheet, Button, Text, toast } from '@/components';
import { api, unwrap } from '@/lib/api';
import { bankRoot, formatBadge, materialMeta, shortDate, type Material } from './api';

function DeleteSheet({ m, subjectId, onClose }: { m: Material; subjectId: number; onClose: () => void }) {
  const qc = useQueryClient();
  const impact = useQuery({
    queryKey: ['material-impact', m.id],
    queryFn: () => unwrap(api.GET('/materials/{materialId}/deletion-impact', { params: { path: { materialId: m.id } } })),
  }).data;
  const [busy, setBusy] = useState(false);
  const remove = async () => {
    setBusy(true);
    try {
      await unwrap(api.DELETE('/materials/{materialId}', { params: { path: { materialId: m.id } } }));
      await Promise.all([qc.invalidateQueries({ queryKey: bankRoot(subjectId) }), qc.invalidateQueries({ queryKey: ['subjects'] }), qc.invalidateQueries({ queryKey: ['quota'] })]);
      toast('已删除');
      onClose();
    } catch (e) {
      toast(e instanceof ApiError ? e.message : '删除没成功，请重试');
    } finally {
      setBusy(false);
    }
  };
  return (
    <BottomSheet visible centered onClose={onClose} title="删除这份资料？">
      <Text variant="body" style={styles.center}>
        {m.file_name}
      </Text>
      <View style={styles.impact}>
        <Text variant="caption" color={semantic.textPrimary}>· 从它识别出的 {impact?.question_count ?? m.question_count} 道题会一起删除，包括作答记录和错题</Text>
        <Text variant="caption" color={semantic.textPrimary}>· 只来自这份资料的知识点会删除{impact ? `（${impact.kp_delete_count} 个）` : ''}，其他资料里也有的会保留</Text>
        <Text variant="caption" color={semantic.textPrimary}>· 用它做过的整卷成绩保留，预估分会重新计算</Text>
      </View>
      <View style={styles.actions}>
        <Button title="取消" kind="secondary" onPress={onClose} style={styles.flex} />
        <Button title="删除" kind="danger" loading={busy} onPress={() => void remove()} style={styles.flex} />
      </View>
    </BottomSheet>
  );
}

const badgeTone: Record<Material['format'], { bg: string; fg: string }> = {
  pdf: { bg: semantic.dangerSoft, fg: semantic.danger },
  docx: { bg: semantic.infoSoft, fg: '#1D4C77' },
  xlsx: { bg: semantic.masteredSoft, fg: '#1F6B4A' },
  image: { bg: semantic.fill, fg: semantic.textPrimary },
  text: { bg: semantic.fill, fg: semantic.textPrimary },
};

export function MaterialList({ items, subjectId, deletable = true }: { items: Material[]; subjectId: number; deletable?: boolean }) {
  const [removing, setRemoving] = useState<Material | null>(null);
  return (
    <View style={styles.list}>
      {items.map((m, i) => {
        const warn = m.status === 'partial' || m.status === 'failed' || m.status === 'rejected';
        return (
          <Pressable key={m.id} accessibilityRole="button" accessibilityLabel={m.file_name} onLongPress={deletable ? () => setRemoving(m) : undefined} style={[styles.row, i > 0 && styles.divider]}>
            <View style={[styles.badge, { backgroundColor: badgeTone[m.format].bg }]}>
              <Text variant="small" color={badgeTone[m.format].fg} style={styles.badgeText}>
                {formatBadge[m.format]}
              </Text>
            </View>
            <View style={[styles.flex, styles.gap2]}>
              <Text variant="body" numberOfLines={1}>
                {m.file_name}
              </Text>
              <Text variant="small" color={warn ? '#9A5B00' : semantic.textSecondary}>
                {materialMeta(m)}
                {warn ? '' : ` · ${shortDate(m.created_at)}`}
              </Text>
            </View>
            {deletable ? (
              <Pressable accessibilityRole="button" accessibilityLabel={`删除 ${m.file_name}`} onPress={() => setRemoving(m)} style={styles.more}>
                <View style={styles.dots}>
                  <View style={styles.d} />
                  <View style={styles.d} />
                  <View style={styles.d} />
                </View>
              </Pressable>
            ) : null}
          </Pressable>
        );
      })}
      {removing ? <DeleteSheet m={removing} subjectId={subjectId} onClose={() => setRemoving(null)} /> : null}
    </View>
  );
}

const styles = StyleSheet.create({
  list: { borderRadius: radius.card, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface, overflow: 'hidden' },
  row: { flexDirection: 'row', alignItems: 'center', gap: 12, minHeight: 68, paddingLeft: 16, paddingRight: 6, paddingVertical: 12 },
  divider: { borderTopWidth: 1, borderTopColor: semantic.border },
  badge: { width: 34, height: 34, borderRadius: 8, alignItems: 'center', justifyContent: 'center' },
  badgeText: { fontSize: 10, fontWeight: '700' },
  more: { width: 44, height: 44, alignItems: 'center', justifyContent: 'center' },
  dots: { flexDirection: 'row', gap: 3 },
  d: { width: 4, height: 4, borderRadius: 2, backgroundColor: semantic.textSecondary },
  flex: { flex: 1 },
  gap2: { gap: 2 },
  center: { textAlign: 'center' },
  impact: { gap: 6, marginVertical: spacing.md },
  actions: { flexDirection: 'row', gap: 10 },
});
