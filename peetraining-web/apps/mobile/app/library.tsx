// 6.3 我的资料：按专业课列出资料（类型、页数、识别结果）、资料解析额度；还没导入的专业课给导入入口。
// 资料只有自己能看到，不共享、不用于训练模型。删除资料在题库的「资料」里操作（连带规则见 3.1d）。
import { radius, semantic, spacing } from '@training/ui-tokens';
import { useQuery } from '@tanstack/react-query';
import { router } from 'expo-router';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { Button, ErrorState, Icon, Loading, Screen, Text } from '@/components';
import { useMaterials } from '@/features/bank/api';
import { MaterialList } from '@/features/bank/MaterialList';
import { ParseQuotaBar } from '@/features/bank/QuotaBar';
import { PageHeader } from '@/features/import/ui';
import { api, unwrap } from '@/lib/api';

function SubjectMaterials({ s }: { s: { id: number; name: string; code?: string; question_count: number } }) {
  const mats = useMaterials(s.id);
  const items = mats.data?.items ?? [];
  return (
    <View style={styles.card}>
      <View style={styles.row}>
        <Text variant="caption" color={semantic.textPrimary} style={[styles.flex, styles.bold]}>
          {s.code ? `${s.code} ` : ''}
          {s.name}
        </Text>
        <Text variant="small">{items.length ? `${items.length} 份 · ${s.question_count} 题` : '还没导入'}</Text>
      </View>
      {mats.isLoading ? <Loading rows={1} /> : null}
      {items.length ? <MaterialList items={items} subjectId={s.id} deletable={false} /> : null}
      {!mats.isLoading && items.length === 0 ? (
        <Pressable accessibilityRole="button" onPress={() => router.push({ pathname: '/import', params: { subjectId: String(s.id) } })} style={styles.add}>
          <Text variant="caption" color={semantic.textPrimary}>
            ＋ 导入资料
          </Text>
        </Pressable>
      ) : null}
    </View>
  );
}

export default function Library() {
  const subjects = useQuery({ queryKey: ['subjects'], queryFn: () => unwrap(api.GET('/subjects')) });
  if (subjects.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (subjects.isError) return <Screen><ErrorState error={subjects.error} onRetry={() => void subjects.refetch()} /></Screen>;
  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        <PageHeader
          title="我的资料"
          onBack={() => router.back()}
          right={<Button title="+ 导入" kind="text" size="sm" color={semantic.textPrimary} style={styles.link} onPress={() => router.push('/import')} />}
        />
        <View style={styles.privacy}>
          <Icon name="lock" size={16} color="#1F6B4A" />
          <Text variant="small" color="#1F6B4A" style={styles.flex}>
            资料只有你自己能看到，不共享给其他用户，不用于训练模型
          </Text>
        </View>
        <ParseQuotaBar />
        {subjects.data!.items.map((s) => (
          <SubjectMaterials key={s.id} s={s} />
        ))}
        <Text variant="small" style={styles.center}>
          删除资料会一起删除从它识别出的题目和作答记录，在题库的「资料」里操作
        </Text>
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing.xl, gap: 14 },
  card: { gap: 10 },
  row: { flexDirection: 'row', alignItems: 'baseline' },
  flex: { flex: 1 },
  bold: { fontWeight: '700' },
  center: { textAlign: 'center' },
  link: { paddingHorizontal: 0 },
  privacy: { flexDirection: 'row', alignItems: 'center', gap: 10, padding: 14, borderRadius: radius.lg, backgroundColor: semantic.masteredSoft },
  add: { minHeight: 46, alignItems: 'center', justifyContent: 'center', borderRadius: radius.lg, borderWidth: 1, borderStyle: 'dashed', borderColor: '#D6D2C8' },
});
