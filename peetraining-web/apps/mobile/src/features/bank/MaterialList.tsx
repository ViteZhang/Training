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
    <BottomSheet visible onClose={onClose} title="删除这份资料？">
      <Text variant="bodyStrong">{m.file_name}</Text>
      <View style={styles.impact}>
        <Text variant="body">· 从它识别出的 {impact?.question_count ?? m.question_count} 道题会一起删除，包括作答记录和错题</Text>
        <Text variant="body">· 只来自这份资料的知识点会删除{impact ? `（${impact.kp_delete_count} 个）` : ''}，其他资料里也有的会保留</Text>
        <Text variant="body">· 用它做过的整卷成绩保留，预估分会重新计算</Text>
      </View>
      <View style={styles.actions}>
        <Button title="取消" kind="secondary" onPress={onClose} style={styles.flex} />
        <Button title="删除" kind="danger" loading={busy} onPress={() => void remove()} style={styles.flex} />
      </View>
    </BottomSheet>
  );
}

export function MaterialList({ items, subjectId, deletable = true }: { items: Material[]; subjectId: number; deletable?: boolean }) {
  const [removing, setRemoving] = useState<Material | null>(null);
  return (
    <View>
      {items.map((m) => {
        const warn = m.status === 'partial' || m.status === 'failed' || m.status === 'rejected';
        return (
          <Pressable key={m.id} accessibilityRole="button" accessibilityLabel={m.file_name} onLongPress={deletable ? () => setRemoving(m) : undefined} style={styles.row}>
            <View style={styles.badge}>
              <Text variant="small" color={semantic.primary}>
                {formatBadge[m.format]}
              </Text>
            </View>
            <View style={styles.flex}>
              <Text variant="bodyStrong" numberOfLines={1}>
                {m.file_name}
              </Text>
              <Text variant="caption" color={warn ? semantic.danger : semantic.textSecondary}>
                {materialMeta(m)} · {shortDate(m.created_at)}
              </Text>
            </View>
            {deletable ? <Button title="删除" kind="text" onPress={() => setRemoving(m)} /> : null}
          </Pressable>
        );
      })}
      {removing ? <DeleteSheet m={removing} subjectId={subjectId} onClose={() => setRemoving(null)} /> : null}
    </View>
  );
}

const styles = StyleSheet.create({
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, paddingVertical: spacing.sm, borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: semantic.border },
  badge: { width: 44, height: 44, borderRadius: radius.md, backgroundColor: semantic.primarySoft, alignItems: 'center', justifyContent: 'center' },
  flex: { flex: 1 },
  impact: { gap: spacing.xs, marginVertical: spacing.md },
  actions: { flexDirection: 'row', gap: spacing.sm },
});
