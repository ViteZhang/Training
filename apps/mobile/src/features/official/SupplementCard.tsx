// 3.1e 补充资料提示（PRD v3 增补「补充资料提示」，设计稿 n_bank_official）：已添加官方题库的课，某板块错得多且官方题少时，
// 提示导入本校老师的讲义或笔记；每次只显示一条；「以后再说」后同一板块 7 天内不再提示（规则在服务端）。
import { colors, radius, spacing } from '@training/ui-tokens';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { router } from 'expo-router';
import { useEffect } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { Icon, Text } from '@/components';
import { track } from '@/lib/analytics';
import { api, unwrap } from '@/lib/api';
import { officialKeys } from './api';

type Prompt = { reason: 'section_gap' | 'dispute' | 'wrong_book'; section_id?: number; section_name?: string; official_count?: number };

/** 提示文案：标题与说明。 */
export function promptText(p: Prompt): { title: string; desc: string } {
  switch (p.reason) {
    case 'section_gap':
      return {
        title: `「${p.section_name ?? '这一块'}」你错得多，官方题只有 ${p.official_count ?? 0} 道`,
        desc: '有本校老师这一块的讲义或笔记吗？导入后能多练这一块，资料只有你自己能看到。',
      };
    case 'dispute':
      return { title: '和你们老师讲的不一样？', desc: '把老师的讲义或笔记导进来，之后按老师的表述批改，资料只有你自己能看到。' };
    default:
      return { title: '错题攒得有点多了', desc: '导入本校老师的讲义或笔记，针对性地多练几道，资料只有你自己能看到。' };
  }
}

export function SupplementCard({ subjectId }: { subjectId: number }) {
  const qc = useQueryClient();
  const q = useQuery({
    queryKey: officialKeys.supplement(subjectId),
    queryFn: () => unwrap(api.GET('/subjects/{subjectId}/supplement-prompt', { params: { path: { subjectId } } })),
    staleTime: 5 * 60_000,
  });
  const p = q.data?.prompt;
  useEffect(() => {
    if (p) track('supplement_prompt', { action: 'show', reason: p.reason });
  }, [p]);
  if (!p) return null;
  const t = promptText(p);
  const dismiss = () => {
    track('supplement_prompt', { action: 'dismiss', reason: p.reason });
    qc.setQueryData(officialKeys.supplement(subjectId), {});
    void unwrap(api.POST('/subjects/{subjectId}/supplement-prompt/dismiss', { params: { path: { subjectId } }, body: { reason: p.reason, section_id: p.section_id } })).catch(() => undefined);
  };
  return (
    <View style={styles.card}>
      <View style={styles.row}>
        <Icon name="sparkle" size={18} color={colors.orange} />
        <Text variant="bodyStrong" color={colors.orangeDeep} style={styles.flex}>{t.title}</Text>
      </View>
      <Text variant="caption" color={colors.orangeDeep}>{t.desc}</Text>
      <View style={styles.row}>
        <Pressable
          accessibilityRole="button"
          onPress={() => {
            track('supplement_prompt', { action: 'click', reason: p.reason });
            router.push({ pathname: '/import', params: { subjectId: String(subjectId) } });
          }}
          style={styles.primary}
        >
          <Text variant="caption" color={colors.white}>补充我的资料</Text>
        </Pressable>
        <Pressable accessibilityRole="button" onPress={dismiss} style={styles.later}>
          <Text variant="caption" color={colors.orangeDeep}>以后再说</Text>
        </Pressable>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  card: { gap: 10, paddingHorizontal: spacing.lg, paddingVertical: 14, borderRadius: radius.xl + 2, backgroundColor: colors.orangeSoft },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  flex: { flex: 1 },
  primary: { minHeight: 44, paddingHorizontal: 14, borderRadius: 22, justifyContent: 'center', backgroundColor: colors.ink },
  later: { minHeight: 44, paddingHorizontal: 14, justifyContent: 'center' },
});
