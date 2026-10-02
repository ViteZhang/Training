// 1.8 题库建好了：题数、知识点数、待核对数；可选「先做个摸底测」（从题库抽 20 题，可跳过，T17 接通）；
// 「开始今天的训练」；还有专业课没导入时提示继续导入。
import { semantic, spacing } from '@training/ui-tokens';
import { useQuery } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { useEffect } from 'react';
import { ScrollView, StyleSheet, View } from 'react-native';
import { Button, Card, Screen, Text, toast } from '@/components';
import { useImportFlow } from '@/features/import/store';
import { setStep } from '@/features/onboarding/api';
import { Footer } from '@/features/onboarding/ui';
import { api, unwrap } from '@/lib/api';

function Stat({ n, label, danger }: { n: string; label: string; danger?: boolean }) {
  return (
    <View style={styles.stat}>
      <Text variant="score" color={danger ? semantic.danger : semantic.textPrimary}>
        {n}
      </Text>
      <Text variant="caption">{label}</Text>
    </View>
  );
}

export default function DoneScreen() {
  const p = useLocalSearchParams<{ questions?: string; kps?: string; review?: string; papers?: string; without?: string }>();
  const { onboarding, reset } = useImportFlow();
  const subjects = useQuery({ queryKey: ['subjects'], queryFn: () => unwrap(api.GET('/subjects')) });
  const without = (p.without ?? '').split(',').filter(Boolean).map(Number);
  const pending = subjects.data?.items.filter((s) => without.includes(s.id)) ?? [];

  useEffect(() => {
    // 建好第一个题库，引导结束（PRD 1.8）。
    if (onboarding) void setStep('done');
  }, [onboarding]);

  const finish = (to: '/(tabs)/today' | '/import') => {
    reset();
    router.replace(to);
  };

  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        <Text variant="h1" style={styles.title}>
          题库建好了
        </Text>
        <Text variant="body" color={semantic.textSecondary}>
          {Number(p.papers) > 0 ? `已按年份组成 ${p.papers} 套真题卷，做完一套就能估分。` : '接下来按你的阶段安排每天练什么。'}
        </Text>
        <Card style={styles.stats}>
          <Stat n={p.questions ?? '0'} label="道题" />
          <Stat n={p.kps ?? '0'} label="个知识点" />
          <Stat n={p.review ?? '0'} label="处待核对" danger={Number(p.review) > 0} />
        </Card>
        <Card style={styles.card}>
          <Text variant="bodyStrong">先做个摸底测？</Text>
          <Text variant="caption">从题库抽 20 题，找出薄弱的地方，计划会更准。可以跳过</Text>
          <Button title="做摸底测" kind="secondary" onPress={() => toast('摸底测马上上线，先开始今天的训练吧')} />
        </Card>
        {pending.length > 0 ? (
          <Card style={styles.card}>
            <Text variant="bodyStrong">继续导入</Text>
            {pending.map((s) => (
              <View key={s.id} style={styles.row}>
                <Text variant="body" style={styles.flex}>
                  {s.code ? `${s.code} ` : ''}
                  {s.name} 的资料
                </Text>
                <Button
                  title="导入"
                  kind="text"
                  onPress={() => {
                    reset();
                    router.replace({ pathname: '/import', params: { subjectId: String(s.id) } });
                  }}
                />
              </View>
            ))}
          </Card>
        ) : null}
      </ScrollView>
      <Footer>
        <Button title="开始今天的训练" onPress={() => finish('/(tabs)/today')} />
      </Footer>
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing.xl, gap: spacing.md },
  title: { marginTop: spacing.xl },
  stats: { flexDirection: 'row', justifyContent: 'space-around', paddingVertical: spacing.lg },
  stat: { alignItems: 'center', gap: spacing.xs },
  card: { gap: spacing.sm },
  row: { flexDirection: 'row', alignItems: 'center' },
  flex: { flex: 1 },
});
