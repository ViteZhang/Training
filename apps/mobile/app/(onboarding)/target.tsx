// 1.2 设定目标分：每门专业课选满分（100 / 150 / 300），± 设目标分，默认取满分的 70%；可「先不设这门」或整页「先跳过」。
import type { Schemas } from '@training/api-client';
import { colors, fontFamily, radius, semantic, spacing } from '@training/ui-tokens';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { router } from 'expo-router';
import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { Button, Card, ErrorState, Loading, Screen, Text, toast } from '@/components';
import { setStep } from '@/features/onboarding/api';
import { Footer, StepHeader, Stepper } from '@/features/onboarding/ui';
import { api, unwrap } from '@/lib/api';

type FullScore = 100 | 150 | 300;
interface Draft {
  full: FullScore;
  target: number | null;
}

const defaultTarget = (full: number) => Math.round((full * 0.7) / 5) * 5;

export default function TargetStep() {
  const qc = useQueryClient();
  const subjects = useQuery({ queryKey: ['subjects'], queryFn: () => unwrap(api.GET('/subjects')) });
  const [drafts, setDrafts] = useState<Record<number, Draft>>({});
  const [busy, setBusy] = useState(false);

  const draftOf = (s: Schemas['Subject']): Draft =>
    drafts[s.id] ?? { full: (s.full_score as FullScore) ?? 150, target: s.target_score ?? defaultTarget(s.full_score) };
  const set = (id: number, d: Draft) => setDrafts((all) => ({ ...all, [id]: d }));

  const save = async () => {
    setBusy(true);
    try {
      for (const s of subjects.data?.items ?? []) {
        const d = draftOf(s);
        await unwrap(api.PATCH('/subjects/{subjectId}', { params: { path: { subjectId: s.id } }, body: { full_score: d.full, target_score: d.target } }));
      }
      await qc.invalidateQueries({ queryKey: ['subjects'] });
      await goNext();
    } catch {
      toast('保存失败，请重试');
    } finally {
      setBusy(false);
    }
  };
  const goNext = async () => {
    await setStep('1.3');
    router.push('/(onboarding)/setup');
  };

  if (subjects.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (subjects.isError) return <Screen><ErrorState error={subjects.error} onRetry={() => void subjects.refetch()} /></Screen>;

  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        <StepHeader
          step={2}
          title="给自己定个目标分"
          desc="每门专业课一个目标，首页会用它和预估分比，告诉你还差多少。之后在首页点目标分就能改。"
          onBack={() => router.back()}
        />
        <View style={styles.gap}>
          {subjects.data?.items.map((s) => {
            const d = draftOf(s);
            return (
              <Card key={s.id} style={styles.card}>
                <View style={styles.head}>
                  <Text variant="bodyStrong" style={styles.flex}>
                    {s.code ? <Text variant="bodyStrong" style={styles.code}>{s.code} </Text> : null}
                    {s.name}
                  </Text>
                  <Text variant="small">满分</Text>
                  {([100, 150, 300] as FullScore[]).map((full) => (
                    <Pressable
                      key={full}
                      accessibilityRole="radio"
                      accessibilityState={{ selected: d.full === full }}
                      accessibilityLabel={`满分 ${full}`}
                      onPress={() => set(s.id, { full, target: d.target === null ? null : Math.min(defaultTarget(full), full) })}
                      style={[styles.full, d.full === full && styles.fullOn]}
                      hitSlop={6}
                    >
                      <Text variant="small" color={d.full === full ? colors.ink : semantic.textSecondary} style={d.full === full ? styles.bold : undefined}>
                        {full}
                      </Text>
                    </Pressable>
                  ))}
                </View>
                {d.target !== null ? (
                  <>
                    <Stepper value={d.target} min={0} max={d.full} onChange={(v) => set(s.id, { ...d, target: v })} suffix={`/ ${d.full}`} />
                    <View style={styles.track}>
                      <View style={[styles.marker, { left: `${(d.target / d.full) * 100}%` }]} />
                    </View>
                    <Button title="先不设这门" kind="text" size="sm" onPress={() => set(s.id, { ...d, target: null })} />
                  </>
                ) : (
                  <Button title="设个目标分" kind="soft" size="sm" style={styles.start} onPress={() => set(s.id, { ...d, target: defaultTarget(d.full) })} />
                )}
              </Card>
            );
          })}
        </View>
        <View style={styles.hint}>
          <View style={styles.dot} />
          <Text variant="small" style={styles.flex}>
            不知道定多少？可以参考目标院校往年的复试线，和上岸学长学姐的专业课成绩
          </Text>
        </View>
      </ScrollView>
      <Footer>
        <Button title="下一步" loading={busy} onPress={() => void save()} />
        <Button title="先跳过" kind="text" onPress={() => void goNext()} />
      </Footer>
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing.xl },
  gap: { gap: spacing.md },
  card: { gap: 14 },
  head: { flexDirection: 'row', alignItems: 'center', gap: 6 },
  flex: { flex: 1 },
  bold: { fontWeight: '700' },
  code: { fontFamily: fontFamily.numberSemiBold },
  full: { minWidth: 40, minHeight: 28, paddingHorizontal: 8, borderRadius: radius.pill, alignItems: 'center', justifyContent: 'center' },
  fullOn: { backgroundColor: semantic.fill },
  track: { height: 4, borderRadius: 2, backgroundColor: semantic.border },
  marker: { position: 'absolute', top: -6, width: 2, height: 16, marginLeft: -1, backgroundColor: colors.amber },
  start: { alignSelf: 'flex-start' },
  hint: { flexDirection: 'row', gap: 10, marginTop: spacing.md, padding: 16, borderRadius: radius.xl, backgroundColor: semantic.fill },
  dot: { width: 8, height: 8, borderRadius: 4, marginTop: 5, backgroundColor: colors.amber },
});
