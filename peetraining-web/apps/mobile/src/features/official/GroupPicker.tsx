// 添加官方题库后选报考方向（T51）：官方题库里有按专业分组的限选题，而这门课的报考方向一组都对不上（没填或填的专业名不匹配）时，
// 请用户选自己报考的那一组；选中组的适用专业记为报考方向（PATCH /subjects/{id} 的 my_majors），服务端只保留这一组的限选题。
// 选「不区分，全部保留」就不改，各组都留着（以后可以在备考设置里改报考方向）。
import type { Schemas } from '@training/api-client';
import { radius, semantic, spacing } from '@training/ui-tokens';
import { useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { BottomSheet, Button, Icon, Text, toast } from '@/components';
import { api, unwrap } from '@/lib/api';

type Bank = Schemas['OfficialBank'];

/** 这门官方题库添加后要不要请用户选报考方向：有限选题分组，且一组都不匹配。 */
export function needsGroupPick(b?: Pick<Bank, 'groups'>) {
  const gs = b?.groups ?? [];
  return gs.length > 1 && !gs.some((g) => g.matches);
}

export type GroupPick = { subjectId: number; subjectName: string; groups: Schemas['OfficialBankGroup'][] };

export function GroupPicker({ pick, onDone }: { pick?: GroupPick; onDone: () => void }) {
  const qc = useQueryClient();
  const [busy, setBusy] = useState<string>();
  const choose = async (majors: string) => {
    if (!pick) return;
    setBusy(majors);
    try {
      // 服务端改完报考方向会重新对这门课的官方题库，不匹配的限选题下线
      await unwrap(api.PATCH('/subjects/{subjectId}', { params: { path: { subjectId: pick.subjectId } }, body: { my_majors: majors } }));
      toast('已按报考方向保留限选题');
      await Promise.all(['subjects', 'official-banks', 'bank', 'today'].map((k) => qc.invalidateQueries({ queryKey: [k] })));
      onDone();
    } catch {
      toast('保存失败，请重试');
    } finally {
      setBusy(undefined);
    }
  };
  return (
    <BottomSheet visible={!!pick} onClose={onDone} title="你报考的是哪个方向？" dismissible={!busy}>
      <View style={styles.body}>
        <Text variant="caption">
          {pick?.subjectName ? `「${pick.subjectName}」` : '这门课'}的论述题按专业分组，只答自己方向的那一组。选好后只保留你这一组的题，以后可以在备考设置里改。
        </Text>
        {(pick?.groups ?? []).map((g) => (
          <Pressable
            key={`${g.label}-${g.majors}`}
            accessibilityRole="button"
            accessibilityLabel={`${g.label} ${g.majors}`}
            disabled={!!busy}
            onPress={() => void choose(g.majors)}
            style={styles.option}
          >
            <View style={styles.flex}>
              <Text variant="bodyStrong">{g.majors || g.label}</Text>
              {g.majors ? <Text variant="small">{g.label}</Text> : null}
            </View>
            <Icon name="chevron" size={16} color={semantic.textSecondary} />
          </Pressable>
        ))}
        <Button title="不区分，全部保留" kind="text" disabled={!!busy} onPress={onDone} />
      </View>
    </BottomSheet>
  );
}

const styles = StyleSheet.create({
  body: { gap: spacing.md },
  flex: { flex: 1, gap: 2 },
  option: { flexDirection: 'row', alignItems: 'center', gap: spacing.md, minHeight: 56, padding: spacing.lg, borderRadius: radius.card, backgroundColor: semantic.fill },
});
