// 1.4 选择导入方式：手里的是「题目」还是「资料」，放进哪门专业课。作文课不用事先声明，由 AI 按资料判断（PRD 11.12）。
import { semantic, spacing } from '@training/ui-tokens';
import { useQuery } from '@tanstack/react-query';
import { router } from 'expo-router';
import { useState } from 'react';
import { ScrollView, StyleSheet, View } from 'react-native';
import { Button, ErrorState, Loading, Screen, Text } from '@/components';
import { setStep } from '@/features/onboarding/api';
import { Footer, OptionCard, StepHeader } from '@/features/onboarding/ui';
import { api, unwrap } from '@/lib/api';
import type { ImportMode } from './api';
import { useImportFlow } from './store';
import { PageHeader } from './ui';

export const modes: { key: ImportMode; name: string; eg: string; what: string }[] = [
  { key: 'question', name: '题目', eg: '真题汇编、习题册、题库文件', what: '识别题目和答案，导入就能刷' },
  { key: 'reference', name: '资料', eg: '讲义、笔记、参考书章节', what: 'AI 拆成知识点和采分点，再按知识点出题' },
];

export function ModeStep({ onboarding, subjectId: initialSubject }: { onboarding: boolean; subjectId?: number }) {
  const subjects = useQuery({ queryKey: ['subjects'], queryFn: () => unwrap(api.GET('/subjects')) });
  const [mode, setMode] = useState<ImportMode>('question');
  const [picked, setPicked] = useState<number | undefined>(initialSubject);
  const start = useImportFlow((s) => s.start);
  const list = subjects.data?.items ?? [];
  const subjectId = picked ?? list[0]?.id;

  const next = () => {
    if (!subjectId) return;
    start(mode, subjectId, onboarding);
    if (onboarding) void setStep('1.5');
    router.push('/import/files');
  };

  if (subjects.isLoading) return <Screen><Loading rows={5} /></Screen>;
  if (subjects.isError) return <Screen><ErrorState error={subjects.error} onRetry={() => void subjects.refetch()} /></Screen>;

  const desc = 'AI 会把它整理成能刷、能批改的题库，仅你本人可见。';
  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        {onboarding ? (
          <StepHeader step={4} title="导入你的第一份资料" desc={desc} onBack={() => router.back()} />
        ) : (
          <PageHeader title="导入资料" desc={desc} onBack={() => router.back()} />
        )}
        <Text variant="bodyStrong" style={styles.label}>
          你手里的是
        </Text>
        <View style={styles.gap}>
          {modes.map((m) => (
            <OptionCard key={m.key} label={m.name} selected={m.key === mode} onPress={() => setMode(m.key)}>
              <Text variant="h3">{m.name}</Text>
              <Text variant="caption">{m.eg}</Text>
              <Text variant="body" color={semantic.textSecondary}>
                {m.what}
              </Text>
            </OptionCard>
          ))}
        </View>
        <Text variant="caption" style={styles.hint}>
          两种都有？先导入一种，建好后还能继续追加。作文真题、范文、评分细则也可以直接选，AI 会认出来按作文整理
        </Text>

        <Text variant="bodyStrong" style={styles.label}>
          放进哪门专业课
        </Text>
        <View style={styles.gap}>
          {list.map((s) => (
            <OptionCard key={s.id} label={s.name} selected={s.id === subjectId} onPress={() => setPicked(s.id)}>
              <View style={styles.row}>
                {s.code ? <Text variant="number">{s.code}</Text> : null}
                <Text variant="bodyStrong">{s.name}</Text>
              </View>
            </OptionCard>
          ))}
        </View>
      </ScrollView>
      <Footer>
        <Button title="下一步：选择文件" disabled={!subjectId} onPress={next} />
        {onboarding ? (
          <Button
            title="先跳过，进入 App"
            kind="text"
            style={styles.center}
            onPress={() => {
              void setStep('done').then(() => router.replace('/(tabs)/today'));
            }}
          />
        ) : null}
      </Footer>
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing.xl },
  label: { marginTop: spacing.xl, marginBottom: spacing.sm },
  gap: { gap: spacing.sm },
  hint: { marginTop: spacing.sm },
  row: { flexDirection: 'row', alignItems: 'baseline', gap: spacing.sm },
  center: { alignSelf: 'center' },
});
