// 1.9 体验一道题 · 输入题目（PRD v3 增补，设计稿 n_try_input）：粘贴文字或拍照识别一道题；AI 识别题型与分值，用户可改；
// 参考答案选填，不填由 AI 生成并标「AI 参考答案」；右上角显示剩余体验次数；只支持名词解释、简答、论述。
// 「这道题只用于这次体验，你决定要不要存进题库」。体验不消耗任何免费额度。
import { ApiError } from '@training/api-client';
import { colors, layout, radius, semantic, spacing } from '@training/ui-tokens';
import { router, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { ScrollView, StyleSheet, TextInput, View } from 'react-native';
import { AIGenerating, Button, ErrorState, Loading, NavBar, Screen, Segmented, Tag, Text, toast } from '@/components';
import { qtypeLabel, useTrialStatus } from '@/features/official/api';
import { Chips, Stepper } from '@/features/onboarding/ui';
import { HandwritingFlow } from '@/features/practice/handwriting';
import { track } from '@/lib/analytics';
import { api, unwrap } from '@/lib/api';

type QType = 'term' | 'short_answer' | 'discussion';
const qtypes: QType[] = ['term', 'short_answer', 'discussion'];

export default function TrialInput() {
  const { subjectId } = useLocalSearchParams<{ subjectId?: string }>();
  const status = useTrialStatus();
  const [mode, setMode] = useState<'paste' | 'photo'>('paste');
  const [photo, setPhoto] = useState(false);
  const [text, setText] = useState('');
  const [stem, setStem] = useState('');
  const [qtype, setQtype] = useState<QType>();
  const [score, setScore] = useState(10);
  const [unsupported, setUnsupported] = useState(false);
  const [editing, setEditing] = useState(false);
  const [answer, setAnswer] = useState('');
  const [busy, setBusy] = useState<'recognize' | 'create'>();

  const recognize = async (raw = text) => {
    if (!raw.trim()) return;
    setBusy('recognize');
    try {
      const r = await unwrap(api.POST('/trials/recognize', { body: { text: raw } }));
      setStem(r.stem);
      setUnsupported(!r.supported);
      if (r.supported) setQtype(r.qtype as QType);
      setScore(Math.round(r.score) || 10);
      if (r.answer && !answer) setAnswer(r.answer);
    } catch (e) {
      toast(e instanceof ApiError ? e.message : '识别失败，请重试');
    } finally {
      setBusy(undefined);
    }
  };

  const create = async () => {
    if (!qtype || !stem.trim()) return;
    setBusy('create');
    try {
      const t = await unwrap(
        api.POST('/trials', { body: { input_mode: mode, qtype, score, stem: stem.trim(), reference_answer: answer.trim() || undefined } }),
      );
      track('trial_start', { type: 'own', input: mode });
      router.replace({ pathname: '/trial/[id]', params: { id: String(t.id), subjectId } });
    } catch (e) {
      toast(e instanceof ApiError ? e.message : '没能准备好这道题，请重试');
    } finally {
      setBusy(undefined);
    }
  };

  if (status.isLoading) return <Screen><Loading rows={5} /></Screen>;
  if (status.isError) return <Screen><ErrorState error={status.error} onRetry={() => void status.refetch()} /></Screen>;
  if (busy === 'create') {
    return (
      <Screen>
        <AIGenerating title="AI 正在整理采分点" desc={answer.trim() ? '按你填的参考答案提取采分点' : '没有参考答案，AI 先写一份参考答案再提取采分点'} steps={['读题', '整理参考答案', '提取采分点']} current={1} />
      </Screen>
    );
  }
  if (photo) {
    return (
      <Screen>
        <NavBar title="拍照识别题目" onBack={() => setPhoto(false)} />
        <HandwritingFlow
          confirmLabel="用这段文字"
          onCancel={() => setPhoto(false)}
          onConfirm={(t) => {
            setPhoto(false);
            setText(t);
            void recognize(t);
          }}
        />
      </Screen>
    );
  }

  const remaining = status.data?.remaining ?? 0;
  return (
    <Screen>
      <NavBar title="体验一道题" onBack={() => router.back()} right={<Tag label={`剩 ${remaining} 次`} tone="info" />} />
      <ScrollView keyboardShouldPersistTaps="handled" contentContainerStyle={styles.scroll}>
        {remaining <= 0 ? <Text variant="body" color={semantic.danger}>体验次数已用完，导入你自己的资料就能继续练。</Text> : null}
        <Segmented
          options={[{ key: 'paste' as const, label: '粘贴文字' }, { key: 'photo' as const, label: '拍照识别' }]}
          value={mode}
          onChange={(v) => {
            setMode(v);
            if (v === 'photo') setPhoto(true);
          }}
        />
        <Text variant="caption" color={semantic.textPrimary} style={styles.label}>题目</Text>
        <TextInput
          accessibilityLabel="题目"
          multiline
          placeholder="粘贴一道名词解释、简答或论述题，如：简述康德的「哥白尼式革命」。（15 分）"
          placeholderTextColor={colors.faint}
          value={stem || text}
          onChangeText={(v) => {
            setText(v);
            setStem('');
            setQtype(undefined);
            setUnsupported(false);
          }}
          style={[styles.area, styles.stem]}
          maxLength={2000}
          maxFontSizeMultiplier={layout.maxFontScale}
        />
        {!stem ? (
          <Button title="识别题目" kind="soft" disabled={!text.trim()} loading={busy === 'recognize'} onPress={() => void recognize()} />
        ) : unsupported ? (
          <Text variant="caption" color={semantic.danger}>体验只支持名词解释、简答、论述，换一道试试。</Text>
        ) : qtype ? (
          <View style={styles.recognized}>
            <View style={styles.row}>
              <Text variant="caption">识别为</Text>
              <Text variant="bodyStrong" style={styles.flex}>{qtypeLabel[qtype]} {score} 分</Text>
              <Button title={editing ? '完成' : '改'} kind="text" size="sm" onPress={() => setEditing(!editing)} />
            </View>
            {editing ? (
              <>
                <Chips options={qtypes} value={qtype} onChange={setQtype} format={(v) => qtypeLabel[v] ?? v} />
                <Stepper value={score} min={1} max={150} step={1} onChange={setScore} suffix="分" />
              </>
            ) : null}
          </View>
        ) : null}
        <Text variant="caption" color={semantic.textPrimary} style={styles.label}>
          参考答案 <Text variant="caption">选填</Text>
        </Text>
        <TextInput
          accessibilityLabel="参考答案"
          multiline
          placeholder="有老师给的参考答案就贴上；不填由 AI 生成，标「AI 参考答案」"
          placeholderTextColor={colors.faint}
          value={answer}
          onChangeText={setAnswer}
          style={styles.area}
          maxLength={6000}
          maxFontSizeMultiplier={layout.maxFontScale}
        />
        <Text variant="small">这道题只用于这次体验，你决定要不要存进题库</Text>
      </ScrollView>
      <View style={styles.footer}>
        <Button title="下一步：写答案" disabled={!qtype || unsupported || remaining <= 0} onPress={() => void create()} />
      </View>
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing.xl, gap: spacing.md },
  label: { fontWeight: '500' },
  area: { minHeight: 96, padding: spacing.lg, borderRadius: radius.lg, borderWidth: 1, borderColor: semantic.inputBorder, fontSize: 15, lineHeight: 22, color: semantic.textPrimary, textAlignVertical: 'top' },
  stem: { minHeight: 120 },
  recognized: { gap: spacing.sm, padding: spacing.md, borderRadius: radius.md, backgroundColor: semantic.fill },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  flex: { flex: 1 },
  footer: { paddingTop: spacing.md, paddingBottom: spacing.xxl },
});
