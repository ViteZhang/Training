// 1.7 按专业分组的论述题（「第一组：请选择××专业的考生作答」，回答非报考专业不得分）：
// 顶部卡片提示分了几组、只保留报考方向那一组；选择弹层可多选或全部保留。选择记在专业课上，下次导入同一门课时自动标出「你的方向」。
import { ApiError, type Schemas } from '@training/api-client';
import { colors, radius, semantic, spacing } from '@training/ui-tokens';
import { useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { BottomSheet, Button, Icon, Tag, Text, toast } from '@/components';
import { api, unwrap } from '@/lib/api';
import { track } from '@/lib/analytics';
import { importKeys } from './api';

type Group = Schemas['ImportGroup'];

const name = (g: Group) => (g.majors ? `${g.label} · ${g.majors}` : g.label);

export function GroupBanner({ jobId, groups }: { jobId: number; groups: Group[] }) {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  if (groups.length <= 1) return null;

  const kept = groups.filter((g) => g.kept > 0);
  const chosen = kept.length < groups.length; // 已经选过方向
  const match = groups.find((g) => g.matches);

  const keep = async (labels: string[]) => {
    setBusy(true);
    try {
      const r = await unwrap(api.POST('/import-jobs/{jobId}/groups', { params: { path: { jobId } }, body: { labels } }));
      track('confirm_edit', { action: 'keep_groups', count: labels.length });
      await Promise.all([qc.invalidateQueries({ queryKey: ['import-items', jobId] }), qc.invalidateQueries({ queryKey: importKeys.job(jobId) }), qc.invalidateQueries({ queryKey: ['subjects'] })]);
      toast(labels.length === 0 ? '各组都保留' : `已只保留 ${labels.length} 组，不收录 ${r.deleted} 题`);
      setOpen(false);
    } catch (e) {
      toast(e instanceof ApiError ? e.message : '没保存成功，请重试');
    } finally {
      setBusy(false);
    }
  };

  return (
    <View style={styles.banner}>
      <Text variant="bodyStrong">论述题按专业分了 {groups.length} 组</Text>
      {chosen ? (
        <Text variant="caption">
          已只保留 <Text variant="caption" color={semantic.textPrimary}>{kept.map(name).join('、') || '无'}</Text>，其他组不收录
        </Text>
      ) : match ? (
        <Text variant="caption">
          按你的报考方向，只保留 <Text variant="caption" color={semantic.textPrimary}>{name(match)}</Text>？其他组回答了也不得分
        </Text>
      ) : (
        <Text variant="caption">回答非报考专业不得分。选你报考的方向，其他组不收录，练的时候不会混进来</Text>
      )}
      <View style={styles.actions}>
        {!chosen && match ? <Button title="只保留这一组" size="sm" loading={busy} onPress={() => void keep([match.label])} /> : null}
        <Button title={chosen ? '改' : match ? '选其他' : '选我的方向'} kind={!chosen && !match ? 'primary' : 'secondary'} size="sm" onPress={() => setOpen(true)} />
      </View>
      {open ? <GroupSheet groups={groups} busy={busy} onClose={() => setOpen(false)} onKeep={(l) => void keep(l)} /> : null}
    </View>
  );
}

function GroupSheet({ groups, busy, onClose, onKeep }: { groups: Group[]; busy: boolean; onClose: () => void; onKeep: (labels: string[]) => void }) {
  const initial = groups.filter((g) => g.kept > 0).map((g) => g.label);
  const allKept = initial.length === groups.length;
  const [picked, setPicked] = useState<Set<string>>(new Set(allKept ? groups.filter((g) => g.matches).map((g) => g.label) : initial));
  const toggle = (l: string) =>
    setPicked((cur) => {
      const next = new Set(cur);
      if (next.has(l)) next.delete(l);
      else next.add(l);
      return next;
    });
  return (
    <BottomSheet visible onClose={onClose} title="选你报考的方向">
      <Text variant="caption" style={styles.sheetDesc}>
        只保留选中的组，其他组的题不收录。选择会记在这门专业课上，下次导入自动选好。
      </Text>
      <View style={styles.options}>
        {groups.map((g) => {
          const on = picked.has(g.label);
          return (
            <Pressable key={g.label} accessibilityRole="checkbox" accessibilityState={{ checked: on }} onPress={() => toggle(g.label)} style={[styles.option, on && styles.optionOn]}>
              <View style={[styles.box, on && styles.boxOn]}>{on ? <Icon name="check" size={12} color={semantic.textOnBrand} /> : null}</View>
              <View style={styles.flex}>
                <Text variant="body">{g.label}</Text>
                {g.majors ? <Text variant="small">{g.majors}</Text> : null}
              </View>
              {g.matches ? <Tag label="你的方向" tone="progress" /> : null}
              <Text variant="small">{g.total} 题</Text>
            </Pressable>
          );
        })}
      </View>
      <Button title={picked.size > 0 ? `只保留选中的 ${picked.size} 组` : '先选一组'} disabled={picked.size === 0} loading={busy} onPress={() => onKeep(groups.filter((g) => picked.has(g.label)).map((g) => g.label))} />
      <Button title="各组都保留" kind="text" onPress={() => onKeep([])} />
    </BottomSheet>
  );
}

const styles = StyleSheet.create({
  banner: { gap: 6, paddingVertical: 14, paddingHorizontal: 16, borderRadius: radius.xl, backgroundColor: semantic.infoSoft },
  actions: { flexDirection: 'row', gap: spacing.sm, marginTop: 4 },
  sheetDesc: { marginBottom: spacing.md },
  options: { gap: spacing.sm, marginBottom: spacing.lg },
  option: { flexDirection: 'row', alignItems: 'center', gap: 12, minHeight: 56, paddingVertical: 10, paddingHorizontal: 14, borderRadius: radius.xl, borderWidth: 1, borderColor: semantic.border },
  optionOn: { borderColor: semantic.textPrimary },
  box: { width: 20, height: 20, borderRadius: 10, borderWidth: 1.5, borderColor: colors.faint, alignItems: 'center', justifyContent: 'center' },
  boxOn: { backgroundColor: semantic.textPrimary, borderColor: semantic.textPrimary },
  flex: { flex: 1, gap: 2 },
});
