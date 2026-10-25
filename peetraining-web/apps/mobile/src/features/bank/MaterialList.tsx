// 3.1c 资料、6.3 我的资料共用：每份资料的类型、页数、识别出的题数、导入日期；识别不完整时提醒；删除前说明连带影响（3.1d）。
// 每行右侧「更多」：修改名称（方便管理资料）、删除（只在题库的「资料」里）。
import { ApiError } from '@training/api-client';
import { colors, radius, semantic, spacing } from '@training/ui-tokens';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { Pressable, StyleSheet, TextInput, View } from 'react-native';
import { BottomSheet, Button, Icon, Text, toast } from '@/components';
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

export const MATERIAL_NAME_MAX = 60;

/** 修改资料名称：打开时才挂载，每次从当前名称开始编辑。 */
function RenameSheet({ m, subjectId, onClose }: { m: Material; subjectId: number; onClose: () => void }) {
  const qc = useQueryClient();
  const [value, setValue] = useState(m.file_name);
  const trimmed = value.trim();
  const save = useMutation({
    mutationFn: (file_name: string) => unwrap(api.PATCH('/materials/{materialId}', { params: { path: { materialId: m.id } }, body: { file_name } })),
    onSuccess: async () => {
      await Promise.all([qc.invalidateQueries({ queryKey: bankRoot(subjectId) }), qc.invalidateQueries({ queryKey: ['import-job'] })]);
      toast('名称已修改');
      onClose();
    },
    onError: (e) => toast(e instanceof ApiError ? e.message : '保存失败，请重试'),
  });
  return (
    <BottomSheet visible onClose={onClose} title="修改资料名称">
      <View style={styles.box}>
        <TextInput
          accessibilityLabel="资料名称"
          placeholder="如：2024 真题、第三章讲义"
          placeholderTextColor={colors.faint}
          value={value}
          onChangeText={setValue}
          maxLength={MATERIAL_NAME_MAX}
          autoFocus
          selectTextOnFocus
          style={styles.input}
        />
        <Text variant="caption" num color={semantic.textTertiary}>
          {[...value].length}/{MATERIAL_NAME_MAX}
        </Text>
      </View>
      <View style={styles.sheetActions}>
        <Button title="取消" kind="secondary" onPress={onClose} style={styles.flex} />
        <Button title="保存" loading={save.isPending} disabled={!trimmed || trimmed === m.file_name} onPress={() => save.mutate(trimmed)} style={styles.flex} />
      </View>
    </BottomSheet>
  );
}

/** 「更多」弹层：修改名称、删除资料。 */
function ActionSheet({ m, deletable, onRename, onDelete, onClose }: { m: Material; deletable: boolean; onRename: () => void; onDelete: () => void; onClose: () => void }) {
  return (
    <BottomSheet visible onClose={onClose} title={m.file_name}>
      <View style={styles.menu}>
        <Pressable accessibilityRole="button" accessibilityLabel="修改名称" onPress={onRename} style={styles.menuRow}>
          <Icon name="edit" size={20} />
          <Text variant="body">修改名称</Text>
        </Pressable>
        {deletable ? (
          <Pressable accessibilityRole="button" accessibilityLabel="删除资料" onPress={onDelete} style={[styles.menuRow, styles.divider]}>
            <Icon name="trash" size={20} color={semantic.danger} />
            <Text variant="body" color={semantic.danger}>
              删除资料
            </Text>
          </Pressable>
        ) : null}
      </View>
    </BottomSheet>
  );
}

const badgeTone: Record<Material['format'], { bg: string; fg: string }> = {
  pdf: { bg: semantic.dangerSoft, fg: semantic.danger },
  docx: { bg: semantic.infoSoft, fg: colors.blueInk },
  xlsx: { bg: semantic.masteredSoft, fg: colors.green },
  image: { bg: semantic.fill, fg: semantic.textPrimary },
  text: { bg: semantic.fill, fg: semantic.textPrimary },
};

export function MaterialList({ items, subjectId, deletable = true }: { items: Material[]; subjectId: number; deletable?: boolean }) {
  const [acting, setActing] = useState<Material | null>(null);
  const [renaming, setRenaming] = useState<Material | null>(null);
  const [removing, setRemoving] = useState<Material | null>(null);
  // 关掉「更多」再开下一个弹层：iOS 上一个 Modal 还在收起时不能接着弹出另一个
  const after = (open: () => void) => {
    setActing(null);
    setTimeout(open, 300);
  };
  return (
    <View style={styles.list}>
      {items.map((m, i) => {
        const warn = m.status === 'partial' || m.status === 'failed' || m.status === 'rejected';
        return (
          <Pressable key={m.id} accessibilityRole="button" accessibilityLabel={m.file_name} onLongPress={() => setActing(m)} style={[styles.row, i > 0 && styles.divider]}>
            <View style={[styles.badge, { backgroundColor: badgeTone[m.format].bg }]}>
              <Text variant="small" num={600} color={badgeTone[m.format].fg} style={styles.badgeText}>
                {formatBadge[m.format]}
              </Text>
            </View>
            <View style={[styles.flex, styles.gap2]}>
              <Text variant="body" numberOfLines={1}>
                {m.file_name}
              </Text>
              <Text variant="small" color={warn ? colors.orange : semantic.textSecondary}>
                {materialMeta(m)}
                {warn ? '' : ` · ${shortDate(m.created_at)}`}
              </Text>
            </View>
            <Pressable accessibilityRole="button" accessibilityLabel={`更多操作 ${m.file_name}`} onPress={() => setActing(m)} style={styles.more}>
              <View style={styles.dots}>
                <View style={styles.d} />
                <View style={styles.d} />
                <View style={styles.d} />
              </View>
            </Pressable>
          </Pressable>
        );
      })}
      {acting ? (
        <ActionSheet
          m={acting}
          deletable={deletable}
          onClose={() => setActing(null)}
          onRename={() => after(() => setRenaming(acting))}
          onDelete={() => after(() => setRemoving(acting))}
        />
      ) : null}
      {renaming ? <RenameSheet m={renaming} subjectId={subjectId} onClose={() => setRenaming(null)} /> : null}
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
  menu: { paddingBottom: spacing.md },
  menuRow: { flexDirection: 'row', alignItems: 'center', gap: 12, minHeight: 52 },
  box: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, minHeight: 52, paddingHorizontal: 16, borderRadius: radius.card, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface },
  input: { flex: 1, minWidth: 0, minHeight: 44, fontSize: 15, padding: 0, color: semantic.textPrimary },
  sheetActions: { flexDirection: 'row', gap: spacing.sm, paddingVertical: spacing.lg },
});
