// 1.9a 体验一道题 · 作答 与 1.9b 体验结果（PRD v3 增补，设计稿 n_try_answer、n_try_result）。
// 作答：题目与题型分值；「AI 已整理出 N 个采分点，提交后再给你看」；打字或拍手写稿（规则同 4.4 / 4.5）；「提交批改（不扣额度）」。
// 结果：得分、命中统计、采分点逐条结果（同 4.7b，标「按 AI 参考答案批改」「仅供参考」）；询问「把这道题加入你的自建题库？」：
// 加入并导入整套真题 → 1.5；只加入这道题 → 2.1；不保存 → 2.1c（题目与作答全部删除）。之后先走 1.2、1.3（可跳过）。
import { ApiError } from '@training/api-client';
import { colors, layout, radius, semantic, spacing } from '@training/ui-tokens';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import * as Crypto from 'expo-crypto';
import { router, useLocalSearchParams, type Href } from 'expo-router';
import { useEffect, useRef, useState } from 'react';
import { ScrollView, StyleSheet, TextInput, View } from 'react-native';
import { AIFailed, AIGenerating, Button, Card, ErrorState, Loading, NavBar, Screen, Tag, Text, toast } from '@/components';
import { afterFirstGrade, officialKeys, qtypeScore, type Trial } from '@/features/official/api';
import { HandwritingFlow } from '@/features/practice/handwriting';
import { track } from '@/lib/analytics';
import { api, unwrap } from '@/lib/api';
import { getJSON, setJSON, storage } from '@/lib/storage';

const verdictColor = { hit: colors.green, partial: colors.orange, miss: colors.red } as const;
const verdictMark = { hit: '中', partial: '半', miss: '漏' } as const;

export default function TrialPage() {
  const { id, subjectId } = useLocalSearchParams<{ id: string; subjectId?: string }>();
  const trialId = Number(id);
  const qc = useQueryClient();
  const trial = useQuery({ queryKey: officialKeys.trial(trialId), queryFn: () => unwrap(api.GET('/trials/{trialId}', { params: { path: { trialId } } })) });
  const subjects = useQuery({ queryKey: ['subjects'], queryFn: () => unwrap(api.GET('/subjects')) });

  if (trial.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (trial.isError || !trial.data) return <Screen><ErrorState error={trial.error} onRetry={() => void trial.refetch()} /></Screen>;
  const t = trial.data;
  const update = (next: Trial) => qc.setQueryData(officialKeys.trial(trialId), next);
  const subject = subjects.data?.items.find((s) => s.id === Number(subjectId)) ?? subjects.data?.items.find((s) => !s.is_essay);
  return t.status === 'ready' ? <Answer t={t} onGraded={update} /> : <Result t={t} subject={subject ? { id: subject.id, name: subject.name } : undefined} />;
}

const draftKey = (id: number) => `trial:draft:${id}`;

function Answer({ t, onGraded }: { t: Trial; onGraded: (t: Trial) => void }) {
  const [text, setText] = useState(() => getJSON<string>(draftKey(t.id)) ?? '');
  const [photo, setPhoto] = useState(false);
  const [keys, setKeys] = useState<string[]>();
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const idem = useRef(Crypto.randomUUID());
  // 每 5 秒存一次草稿（CLAUDE.md 必须遵守第 7 条）。
  const latest = useRef(text);
  useEffect(() => {
    latest.current = text;
  }, [text]);
  useEffect(() => {
    const timer = setInterval(() => setJSON(draftKey(t.id), latest.current), 5000);
    return () => clearInterval(timer);
  }, [t.id]);

  const submit = async (answer = text, photoKeys = keys) => {
    if (!answer.trim()) return;
    setBusy(true);
    setFailed(false);
    try {
      const r = await unwrap(
        api.POST('/trials/{trialId}/submit', {
          params: { path: { trialId: t.id } },
          body: { answer, mode: photoKeys?.length ? 'photo' : 'typed', photo_keys: photoKeys, idempotency_key: idem.current },
        }),
      );
      storage.remove(draftKey(t.id));
      track('trial_submit', { type: t.kind, input: photoKeys?.length ? 'photo' : 'typed' });
      track('first_grade_done', { source: t.kind === 'own' ? 'trial' : 'sample' });
      onGraded(r);
    } catch (e) {
      if (e instanceof ApiError && (e.status === 409 || e.status === 400)) toast(e.message);
      else setFailed(true);
    } finally {
      setBusy(false);
    }
  };

  if (busy) return <Screen><AIGenerating title="AI 正在批改" steps={['提取要点', `比对 ${t.point_count} 个采分点`, '生成建议']} current={1} eta="约 20 秒" /></Screen>;
  if (failed) return <Screen><AIFailed onRetry={() => void submit()} /></Screen>;
  if (photo) {
    return (
      <Screen>
        <NavBar title="拍手写稿" onBack={() => setPhoto(false)} />
        <HandwritingFlow
          onCancel={() => setPhoto(false)}
          onConfirm={(recognized, photoKeys) => {
            setPhoto(false);
            setText(recognized);
            setKeys(photoKeys);
            void submit(recognized, photoKeys);
          }}
        />
      </Screen>
    );
  }
  const n = [...text.replace(/\s/g, '')].length;
  return (
    <Screen>
      <NavBar title="体验一道题" onBack={() => router.back()} />
      <ScrollView keyboardShouldPersistTaps="handled" contentContainerStyle={styles.scroll}>
        <Card tone="fill" style={styles.gap}>
          <View style={styles.row}>
            <Text variant="caption" style={styles.flex}>你的题目</Text>
            {t.kind === 'ai_live' ? <Tag label="AI 出题" tone="ai" /> : null}
            <Text variant="caption">{qtypeScore(t.qtype, t.score)}</Text>
          </View>
          <Text variant="h3">{t.stem}</Text>
        </Card>
        <View style={styles.row}>
          <Text variant="caption" style={styles.flex}>
            AI 已整理出 <Text variant="caption" num color={semantic.textPrimary}>{t.point_count}</Text> 个采分点，提交后再给你看
          </Text>
          {t.answer_source === 'ai' ? <Tag label="AI 生成" tone="ai" /> : null}
        </View>
        <Text variant="caption" color={semantic.textPrimary} style={styles.label}>你的答案</Text>
        <TextInput
          accessibilityLabel="你的答案"
          multiline
          placeholder="按考试的写法作答"
          placeholderTextColor={colors.faint}
          value={text}
          onChangeText={(v) => {
            setText(v);
            setKeys(undefined);
          }}
          style={styles.area}
          maxLength={10000}
          maxFontSizeMultiplier={layout.maxFontScale}
        />
        <View style={styles.row}>
          <Text variant="small" num style={styles.flex}>{n} 字</Text>
          <Button title="拍手写稿" kind="soft" size="sm" onPress={() => setPhoto(true)} />
        </View>
      </ScrollView>
      <View style={styles.footer}>
        <Button title="提交批改（不扣额度）" disabled={!text.trim()} onPress={() => void submit()} />
      </View>
    </Screen>
  );
}

function Result({ t, subject }: { t: Trial; subject?: { id: number; name: string } }) {
  const [busy, setBusy] = useState<string>();
  const points = t.points ?? [];
  const count = (v: 'hit' | 'partial' | 'miss') => points.filter((p) => p.verdict === v).length;

  const save = async (action: 'import_all' | 'only' | 'discard', dest: Href) => {
    setBusy(action);
    try {
      if (t.status !== 'saved') {
        await unwrap(api.POST('/trials/{trialId}/save', { params: { path: { trialId: t.id } }, body: { action, subject_id: action === 'discard' ? undefined : subject?.id } }));
      }
      track('trial_save', { choice: action });
      await afterFirstGrade(dest);
    } catch (e) {
      toast(e instanceof ApiError ? e.message : '保存失败，请重试');
    } finally {
      setBusy(undefined);
    }
  };

  return (
    <Screen>
      <NavBar title="批改结果" back={false} />
      <ScrollView contentContainerStyle={styles.scroll}>
        <View style={styles.scoreRow}>
          <Text variant="score" num>{t.total ?? '—'}</Text>
          <Text variant="caption" num>/ {t.score} 分</Text>
          <View style={[styles.flex, styles.gap4]}>
            <Text variant="small">命中 {count('hit')} · 部分 {count('partial')} · 遗漏 {count('miss')}</Text>
            {t.answer_source === 'ai' ? <Tag label="按 AI 参考答案批改 · 仅供参考" tone="ai" /> : <Tag label="仅供参考，不计入预估分" tone="neutral" />}
          </View>
        </View>
        <Card style={styles.points}>
          {points.map((p, i) => (
            <View key={p.seq} style={[styles.point, i > 0 && styles.divider]}>
              <View style={[styles.mark, { backgroundColor: verdictColor[p.verdict] }]}>
                <Text variant="small" color={colors.white}>{verdictMark[p.verdict]}</Text>
              </View>
              <View style={styles.flex}>
                <View style={styles.row}>
                  <Text variant="bodyStrong" style={styles.flex}>{p.content}</Text>
                  <Text variant="caption" num color={verdictColor[p.verdict]}>+{p.got}</Text>
                </View>
                {p.reason ? <Text variant="small">{p.reason}</Text> : null}
              </View>
            </View>
          ))}
        </Card>
        {t.suggestions?.length ? (
          <Card tone="fill" style={styles.gap}>
            {t.suggestions.map((s) => <Text key={s} variant="caption" color={semantic.textPrimary}>· {s}</Text>)}
          </Card>
        ) : null}
        <Card style={styles.ask}>
          <Text variant="h3">把这道题加入你的自建题库？</Text>
          {subject ? (
            <Text variant="caption">会存进「{subject.name}」，成为你题库的第一题，之后可以继续练、继续批改。仅你本人可见。</Text>
          ) : (
            <Text variant="caption">你还没有添加专业课，先导入自己的资料，或者先进入 App 看看。</Text>
          )}
          <Card tone="fill" style={styles.gap4}>
            <Text variant="bodyStrong">想多练几道？把整套真题导进来</Text>
            <Text variant="small">AI 会把每道题都整理好采分点，像刚才一样批改</Text>
          </Card>
          {subject ? (
            <>
              <Button title="加入，并导入整套真题" loading={busy === 'import_all'} disabled={!!busy} onPress={() => void save('import_all', '/(onboarding)/import')} />
              <Button title="只加入这道题" kind="secondary" loading={busy === 'only'} disabled={!!busy} onPress={() => void save('only', '/(tabs)/today')} />
            </>
          ) : (
            <Button title="导入我的资料" loading={busy === 'import'} disabled={!!busy} onPress={() => void save('discard', '/(onboarding)/import')} />
          )}
          <Button title={subject ? '不保存' : '先进入 App'} kind="text" disabled={!!busy} onPress={() => void save('discard', '/(tabs)/today')} />
        </Card>
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing.xxl, gap: spacing.md },
  gap: { gap: spacing.sm },
  gap4: { gap: 4 },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  flex: { flex: 1 },
  label: { fontWeight: '500' },
  area: { minHeight: 200, padding: spacing.lg, borderRadius: radius.lg, borderWidth: 1, borderColor: semantic.inputBorder, fontSize: 15, lineHeight: 22, color: semantic.textPrimary, textAlignVertical: 'top' },
  footer: { paddingTop: spacing.md, paddingBottom: spacing.xxl },
  scoreRow: { flexDirection: 'row', alignItems: 'flex-end', gap: spacing.sm },
  points: { paddingVertical: 4 },
  point: { flexDirection: 'row', gap: spacing.md, paddingVertical: spacing.md },
  divider: { borderTopWidth: 1, borderTopColor: semantic.border },
  mark: { width: 24, height: 24, borderRadius: 12, alignItems: 'center', justifyContent: 'center' },
  ask: { gap: spacing.md },
});
