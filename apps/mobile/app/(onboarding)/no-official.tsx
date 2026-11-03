// 1.1c 暂无官方题库 · 三个选择（PRD v3 增补，设计稿 n_no_official）：体验一道题（主推荐 → 1.9）、用示例题试试（→ 1.10）、
// 申请收录（→ 1.11）；底部「直接导入我的资料」→ 1.4。体验次数用完后前两个入口显示「已体验」、不可点。
// 开关：trial 关闭时不显示体验一道题；sample_trial 关闭时不显示示例题；course_apply 关闭时不显示申请收录。
import { colors, radius, semantic, spacing } from '@training/ui-tokens';
import { router } from 'expo-router';
import { ScrollView, StyleSheet, View, Pressable } from 'react-native';
import { Button, Icon, Screen, Tag, Text } from '@/components';
import type { IconName } from '@/components/Icon';
import { useTrialStatus } from '@/features/official/api';
import { setStep } from '@/features/onboarding/api';
import { loadDraft } from '@/features/onboarding/draft';
import { Footer, StepHeader } from '@/features/onboarding/ui';
import { useFeatureFlag } from '@/lib/flags';

export default function NoOfficial() {
  const draft = loadDraft();
  const course = draft.noOfficial?.[0];
  const trialOn = useFeatureFlag('trial');
  const sampleOn = useFeatureFlag('sample_trial');
  const applyOn = useFeatureFlag('course_apply');
  const status = useTrialStatus(trialOn || sampleOn);
  const used = status.data ? status.data.remaining <= 0 : false;

  const importOwn = async () => {
    await setStep('1.4');
    router.push('/(onboarding)/import');
  };
  const head = [draft.school, course?.major, course ? `${course.code ? `${course.code} ` : ''}${course.name}` : ''].filter(Boolean).join(' · ');

  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        <StepHeader step={1} title="这门课还没有官方题库" desc={head} onBack={() => router.back()} />
        {trialOn ? (
          <Entry
            primary
            icon="edit"
            title="用你手里的一道题，试试 AI 批改"
            meta="约 2 分钟 · 不消耗免费额度"
            desc="粘贴或拍一道名词解释、简答或论述题，写下你的答案，看看按采分点你能拿几分。"
            done={used}
            onPress={() => router.push({ pathname: '/trial/input', params: { subjectId: course ? String(course.subjectId) : undefined } })}
          />
        ) : null}
        {sampleOn ? (
          <Entry
            icon="book"
            title="手边没有题？用示例题试试"
            meta="哲学、文学按方向出题"
            done={used}
            onPress={() => router.push({ pathname: '/trial/samples', params: { subjectId: course ? String(course.subjectId) : undefined } })}
          />
        ) : null}
        {applyOn ? (
          <Entry icon="bell" title="申请收录这门课" meta="收录上线后第一时间通知你" onPress={() => router.push({ pathname: '/apply', params: { entry: 'onboarding' } })} />
        ) : null}
      </ScrollView>
      <Footer>
        <Button title="直接导入我的资料" kind={trialOn || sampleOn ? 'secondary' : 'primary'} onPress={() => void importOwn()} />
      </Footer>
    </Screen>
  );
}

function Entry({ icon, title, meta, desc, primary, done, onPress }: { icon: IconName; title: string; meta: string; desc?: string; primary?: boolean; done?: boolean; onPress: () => void }) {
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityState={{ disabled: !!done }}
      disabled={done}
      onPress={onPress}
      style={[styles.entry, primary && styles.primary, done && styles.done]}
    >
      <View style={[styles.icon, primary && styles.iconPrimary]}>
        <Icon name={icon} size={20} color={primary ? colors.white : semantic.textPrimary} />
      </View>
      <View style={styles.flex}>
        <Text variant="bodyStrong" color={primary ? colors.white : semantic.textPrimary}>{title}</Text>
        <Text variant="small" color={primary ? colors.faint : semantic.textSecondary}>{meta}</Text>
        {desc ? <Text variant="caption" color={primary ? colors.line : semantic.textSecondary}>{desc}</Text> : null}
      </View>
      {done ? <Tag label="已体验" style={styles.center} /> : <Icon name="chevron" size={16} color={primary ? colors.white : semantic.textSecondary} />}
    </Pressable>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing.xl, gap: spacing.md },
  entry: { flexDirection: 'row', alignItems: 'center', gap: spacing.md, minHeight: 72, padding: spacing.lg, borderRadius: radius.card, borderWidth: 1, borderColor: semantic.border },
  primary: { backgroundColor: semantic.primary, borderColor: semantic.primary, alignItems: 'flex-start' },
  done: { opacity: 0.55 },
  icon: { width: 40, height: 40, borderRadius: radius.md, alignItems: 'center', justifyContent: 'center', backgroundColor: semantic.fill },
  iconPrimary: { backgroundColor: colors.inkSoft },
  flex: { flex: 1, gap: 4 },
  center: { alignSelf: 'center' },
});
