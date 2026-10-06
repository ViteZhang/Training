// 1.8 题库建好了：题数、知识点数、待核对数；可选「先做个摸底测」（从题库抽 20 题，可跳过）；
// 「开始今天的训练」；还有专业课没导入时提示继续导入。
import { colors, fontFamily, radius, semantic, spacing } from '@training/ui-tokens';
import { useQuery } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { useEffect } from 'react';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { Button, Icon, Screen, Text } from '@/components';
import { useImportFlow } from '@/features/import/store';
import { setStep } from '@/features/onboarding/api';
import { useStartPractice } from '@/features/practice/api';
import { Footer } from '@/features/onboarding/ui';
import { api, unwrap } from '@/lib/api';

function Stat({ n, label, warn }: { n: string; label: string; warn?: boolean }) {
  return (
    <View style={[styles.stat, warn && styles.statWarn]}>
      <Text style={[styles.statNum, warn && styles.warnText]}>{n}</Text>
      <Text variant="small" color={warn ? warnInk : undefined}>
        {label}
      </Text>
    </View>
  );
}

const warnInk = '#8A4B12';

export default function DoneScreen() {
  const p = useLocalSearchParams<{ id: string; questions?: string; kps?: string; review?: string; papers?: string; without?: string }>();
  const job = useQuery({
    queryKey: ['import', 'job', Number(p.id)],
    queryFn: () => unwrap(api.GET('/import-jobs/{jobId}', { params: { path: { jobId: Number(p.id) } } })),
    enabled: !!p.id,
  });
  const start = useStartPractice();
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
        <View style={styles.check}>
          <Icon name="check" size={28} color={colors.green} />
        </View>
        <View style={styles.gap6}>
          <Text variant="h1">题库建好了</Text>
          <Text variant="caption" style={styles.desc}>
            {Number(p.papers) > 0 ? `已按年份组成 ${p.papers} 套真题卷，做完一套就能估分。` : '接下来按你的阶段安排每天练什么。'}
          </Text>
        </View>
        <View style={styles.stats}>
          <Stat n={p.questions ?? '0'} label="道题" />
          <Stat n={p.kps ?? '0'} label="个知识点" />
          <Stat n={p.review ?? '0'} label="处待核对" warn={Number(p.review) > 0} />
        </View>
        <Pressable
          accessibilityRole="button"
          disabled={!job.data?.subject_id || start.isPending}
          onPress={() => {
            reset();
            start.mutate({ subject_id: job.data!.subject_id!, kind: 'placement' });
          }}
          style={styles.placement}
        >
          <View style={styles.dot} />
          <View style={[styles.flex, styles.gap2]}>
            <Text variant="caption" color={colors.ink} style={styles.bold}>
              先做个摸底测？
            </Text>
            <Text variant="small">从题库抽 20 题，找出薄弱的地方，计划会更准。可以跳过</Text>
          </View>
          <Icon name="chevron" size={16} color={semantic.textSecondary} />
        </Pressable>
      </ScrollView>
      <Footer>
        <Button title="开始今天的训练" onPress={() => finish('/(tabs)/today')} />
        {pending.map((x) => (
          <Button
            key={x.id}
            title={`继续导入 ${x.code ? `${x.code} ` : ''}${x.name}的资料`}
            kind="text"
            onPress={() => {
              reset();
              router.replace({ pathname: '/import', params: { subjectId: String(x.id) } });
            }}
          />
        ))}
      </Footer>
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing.xl, gap: 22 },
  check: { marginTop: 40, width: 56, height: 56, borderRadius: 18, backgroundColor: semantic.masteredSoft, alignItems: 'center', justifyContent: 'center' },
  gap6: { gap: 6 },
  gap2: { gap: 2 },
  desc: { fontSize: 14, lineHeight: 21 },
  bold: { fontWeight: '700' },
  stats: { flexDirection: 'row', gap: 8 },
  stat: { flex: 1, gap: 2, padding: 14, borderRadius: radius.xl, backgroundColor: semantic.fill },
  statWarn: { backgroundColor: semantic.amberSoft },
  statNum: { fontFamily: fontFamily.numberSemiBold, fontSize: 28, lineHeight: 34, color: colors.ink },
  warnText: { color: warnInk },
  placement: { flexDirection: 'row', alignItems: 'center', gap: 12, padding: 16, borderRadius: radius.xl, backgroundColor: semantic.fill },
  dot: { width: 8, height: 8, borderRadius: 4, backgroundColor: colors.amber },
  flex: { flex: 1 },
});
