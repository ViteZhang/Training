// 6.3 我的资料：按专业课列出资料（类型、页数、识别结果）、资料解析额度；还没导入的专业课给导入入口。
// 资料只有自己能看到，不共享、不用于训练模型。删除资料在题库的「资料」里操作（连带规则见 3.1d）。
import { semantic, spacing } from '@training/ui-tokens';
import { useQuery } from '@tanstack/react-query';
import { router } from 'expo-router';
import { ScrollView, StyleSheet, View } from 'react-native';
import { Button, Card, ErrorState, Loading, Screen, Text } from '@/components';
import { useMaterials } from '@/features/bank/api';
import { MaterialList } from '@/features/bank/MaterialList';
import { ParseQuotaBar } from '@/features/bank/QuotaBar';
import { PageHeader } from '@/features/import/ui';
import { api, unwrap } from '@/lib/api';

function SubjectMaterials({ s }: { s: { id: number; name: string; code?: string; question_count: number } }) {
  const mats = useMaterials(s.id);
  const items = mats.data?.items ?? [];
  return (
    <Card style={styles.card}>
      <View style={styles.row}>
        <Text variant="bodyStrong" style={styles.flex}>
          {s.code ? `${s.code} ` : ''}
          {s.name}
        </Text>
        <Text variant="caption">{items.length ? `${items.length} 份 · ${s.question_count} 题` : '还没导入'}</Text>
      </View>
      {mats.isLoading ? <Loading rows={1} /> : null}
      {items.length ? <MaterialList items={items} subjectId={s.id} deletable={false} /> : null}
      {!mats.isLoading && items.length === 0 ? (
        <Button title="+ 导入资料" kind="text" onPress={() => router.push({ pathname: '/import', params: { subjectId: String(s.id) } })} />
      ) : null}
    </Card>
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
          desc="资料只有你自己能看到，不共享给其他用户，不用于训练模型"
          onBack={() => router.back()}
          right={<Button title="+ 导入" kind="text" onPress={() => router.push('/import')} />}
        />
        <ParseQuotaBar />
        {subjects.data!.items.map((s) => (
          <SubjectMaterials key={s.id} s={s} />
        ))}
        <Text variant="caption" color={semantic.textSecondary}>
          删除资料会一起删除从它识别出的题目和作答记录，在题库的「资料」里操作
        </Text>
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing.xl, gap: spacing.md },
  card: { gap: spacing.xs },
  row: { flexDirection: 'row', alignItems: 'baseline' },
  flex: { flex: 1 },
});
