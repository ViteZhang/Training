// 6.9 备考设置：专业课与目标分（可改、可增删，删除时提示会一起删除它的题库）；目标院校选填；初试日期只读；
// 每日学习时长；备考阶段（显示系统建议和理由）；作文每周目标篇数。改阶段或时长从明天起生效，改目标分立即生效。
import type { Schemas } from '@training/api-client';
import { ApiError } from '@training/api-client';
import { layout, radius, semantic, spacing } from '@training/ui-tokens';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { router } from 'expo-router';
import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, TextInput, View } from 'react-native';
import { BottomSheet, Button, Card, ConfirmDialog, ErrorState, Loading, QuotaSheet, Screen, Tag, Text, toast } from '@/components';
import { stageInfo, stages } from '@/features/onboarding/api';
import { Chips, OptionCard, Stepper } from '@/features/onboarding/ui';
import { api, unwrap } from '@/lib/api';

type Profile = Schemas['StudyProfile'];

export default function PrepSettings() {
  const qc = useQueryClient();
  const profile = useQuery({ queryKey: ['profile'], queryFn: () => unwrap(api.GET('/profile')) });
  const subjects = useQuery({ queryKey: ['subjects'], queryFn: () => unwrap(api.GET('/subjects')) });
  const [editing, setEditing] = useState<Schemas['Subject'] | null>(null);
  const [adding, setAdding] = useState(false);
  const [deleting, setDeleting] = useState<Schemas['Subject'] | null>(null);
  const [schoolEdit, setSchoolEdit] = useState<string | null>(null);
  const [quota, setQuota] = useState(false);

  const save = useMutation({
    mutationFn: (patch: Partial<Schemas['StudyProfileInput']>) => {
      const p = profile.data as Profile;
      const body: Schemas['StudyProfileInput'] = {
        exam_year: p.exam_year,
        stage: p.pending_stage ?? p.stage,
        daily_minutes: (p.pending_daily_minutes ?? p.daily_minutes) as 30 | 45 | 60 | 90,
        ...patch,
      };
      return unwrap(api.PUT('/profile', { body }));
    },
    onSuccess: (p) => qc.setQueryData(['profile'], p),
    onError: () => toast('保存失败，请重试'),
  });
  const refreshSubjects = () => qc.invalidateQueries({ queryKey: ['subjects'] });

  if (profile.isLoading || subjects.isLoading) return <Screen><Loading rows={8} /></Screen>;
  if (profile.isError || subjects.isError || !profile.data) {
    return <Screen><ErrorState error={profile.error ?? subjects.error} onRetry={() => void Promise.all([profile.refetch(), subjects.refetch()])} /></Screen>;
  }
  const p = profile.data;
  const effectiveStage = p.pending_stage ?? p.stage;
  const effectiveMinutes = (p.pending_daily_minutes ?? p.daily_minutes) as 30 | 45 | 60 | 90;

  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        <Button title="返回" kind="text" onPress={() => router.back()} style={styles.back} />
        <Text variant="h2">备考设置</Text>

        <Text variant="caption" style={styles.section}>
          专业课与目标
        </Text>
        <Card style={styles.gap}>
          {subjects.data?.items.map((s) => (
            <Pressable key={s.id} accessibilityRole="button" onPress={() => setEditing(s)} style={styles.row}>
              <View style={styles.flex}>
                <Text variant="caption">{s.code ?? ''}</Text>
                <Text variant="bodyStrong">{s.name}</Text>
              </View>
              <Text variant="body" color={s.target_score == null ? semantic.textSecondary : undefined}>
                {s.target_score == null ? '未设目标' : `目标 ${s.target_score} / ${s.full_score}`}
              </Text>
            </Pressable>
          ))}
          <Button
            title="添加专业课"
            kind="secondary"
            disabled={(subjects.data?.items.length ?? 0) >= 4}
            onPress={() => (subjects.data?.can_add ? setAdding(true) : setQuota(true))}
          />
          <Text variant="caption">删除专业课会一起删除它的题库</Text>
        </Card>

        <Text variant="caption" style={styles.section}>
          目标院校（选填）
        </Text>
        <Pressable accessibilityRole="button" onPress={() => setSchoolEdit(p.target_school_major ?? '')}>
          <Card style={styles.row}>
            <View style={styles.flex}>
              <Text variant="bodyStrong">院校与专业</Text>
              <Text variant="caption">填了以后，有官方题库上线时会提醒你</Text>
            </View>
            <Text variant="body" color={semantic.textSecondary}>
              {p.target_school_major ?? '未填写'}
            </Text>
          </Card>
        </Pressable>

        <Text variant="caption" style={styles.section}>
          时间
        </Text>
        <Card style={styles.gap}>
          <View style={styles.row}>
            <Text variant="bodyStrong" style={styles.flex}>
              初试日期
            </Text>
            <Text variant="body">{p.subject_exam_date}（距今 {p.days_to_exam} 天）</Text>
          </View>
          <Text variant="bodyStrong">每日学习时长</Text>
          <Chips options={[30, 45, 60, 90] as const} value={effectiveMinutes} onChange={(m) => save.mutate({ daily_minutes: m })} format={(n) => `${n} 分钟`} />
        </Card>

        <Text variant="caption" style={styles.section}>
          备考阶段
        </Text>
        <View style={styles.gap}>
          {stages.map((k) => (
            <OptionCard key={k} label={stageInfo[k].name} selected={k === effectiveStage} onPress={() => save.mutate({ stage: k })}>
              <View style={styles.row}>
                <Text variant="bodyStrong">{stageInfo[k].name}</Text>
                {k === p.suggested_stage ? <Tag label="系统建议" tone="progress" /> : null}
              </View>
              <Text variant="caption">{stageInfo[k].desc}</Text>
            </OptionCard>
          ))}
          {p.suggested_reason ? <Text variant="caption">系统建议：{p.suggested_reason}</Text> : null}
        </View>

        <Text variant="caption" style={styles.section}>
          作文
        </Text>
        <Card style={styles.row}>
          <Text variant="bodyStrong" style={styles.flex}>
            每周目标篇数
          </Text>
          <Stepper value={p.essay_weekly_goal} min={0} max={14} step={1} onChange={(v) => save.mutate({ essay_weekly_goal: v })} suffix="篇" />
        </Card>

        <Text variant="caption" style={styles.note}>
          {p.pending_effective_on ? `已修改，${p.pending_effective_on} 起生效。` : ''}改阶段或时长从明天起生效；改目标分立即更新首页的差距。
        </Text>
      </ScrollView>

      {editing ? (
        <SubjectSheet
          subject={editing}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null);
            void refreshSubjects();
          }}
          onDelete={() => {
            setDeleting(editing);
            setEditing(null);
          }}
        />
      ) : null}
      {adding ? (
        <AddSubjectSheet
          onClose={() => setAdding(false)}
          onQuota={() => {
            setAdding(false);
            setQuota(true);
          }}
          onSaved={() => {
            setAdding(false);
            void refreshSubjects();
          }}
        />
      ) : null}
      <ConfirmDialog
        visible={!!deleting}
        title={`删除「${deleting?.name ?? ''}」？`}
        message="会一起删除它的题库、资料和学习记录，无法找回。"
        confirmText="删除"
        danger
        onCancel={() => setDeleting(null)}
        onConfirm={() => {
          const s = deleting;
          setDeleting(null);
          if (s)
            void unwrap(api.DELETE('/subjects/{subjectId}', { params: { path: { subjectId: s.id } } }))
              .then(() => {
                toast('已删除');
                void refreshSubjects();
              })
              .catch(() => toast('删除失败，请重试'));
        }}
      />
      {schoolEdit !== null ? (
        <BottomSheet visible onClose={() => setSchoolEdit(null)} title="目标院校与专业">
          <TextInput accessibilityLabel="目标院校专业" value={schoolEdit} onChangeText={setSchoolEdit} placeholder="如：海南大学 · 中国语言文学" style={styles.input} maxLength={64} maxFontSizeMultiplier={layout.maxFontScale} />
          <Button
            title="保存"
            onPress={() => {
              save.mutate({ target_school_major: schoolEdit.trim() || null });
              setSchoolEdit(null);
            }}
            style={styles.sheetButton}
          />
        </BottomSheet>
      ) : null}
      <QuotaSheet
        visible={quota}
        onClose={() => setQuota(false)}
        title="免费版最多添加 3 门专业课"
        desc="开通会员后可以添加第 4 门。"
        onUpgrade={() => {
          setQuota(false);
          router.push('/member');
        }}
        freeOptions={[{ label: '先不加了', onPress: () => setQuota(false) }]}
      />
    </Screen>
  );
}

function SubjectSheet({ subject, onClose, onSaved, onDelete }: { subject: Schemas['Subject']; onClose: () => void; onSaved: () => void; onDelete: () => void }) {
  const [full, setFull] = useState<100 | 150 | 300>(subject.full_score as 100 | 150 | 300);
  const [target, setTarget] = useState<number | null>(subject.target_score ?? null);
  const [busy, setBusy] = useState(false);
  const save = async () => {
    setBusy(true);
    try {
      await unwrap(api.PATCH('/subjects/{subjectId}', { params: { path: { subjectId: subject.id } }, body: { full_score: full, target_score: target } }));
      onSaved();
    } catch (e) {
      toast(e instanceof ApiError ? e.message : '保存失败，请重试');
    } finally {
      setBusy(false);
    }
  };
  return (
    <BottomSheet visible onClose={onClose} title={subject.name}>
      <Text variant="caption">满分</Text>
      <Chips options={[100, 150, 300] as const} value={full} onChange={(f) => { setFull(f); if (target !== null) setTarget(Math.min(target, f)); }} />
      <View style={styles.sheetRow}>
        {target !== null ? (
          <>
            <Stepper value={target} min={0} max={full} onChange={setTarget} suffix={`/ ${full}`} />
            <Button title="不设目标" kind="text" onPress={() => setTarget(null)} />
          </>
        ) : (
          <Button title="设个目标分" kind="secondary" onPress={() => setTarget(Math.round((full * 0.7) / 5) * 5)} />
        )}
      </View>
      <Button title="保存" loading={busy} onPress={() => void save()} style={styles.sheetButton} />
      <Button title="删除这门专业课" kind="text" onPress={onDelete} />
    </BottomSheet>
  );
}

function AddSubjectSheet({ onClose, onSaved, onQuota }: { onClose: () => void; onSaved: () => void; onQuota: () => void }) {
  const [name, setName] = useState('');
  const [code, setCode] = useState('');
  const [busy, setBusy] = useState(false);
  const add = async () => {
    setBusy(true);
    try {
      await unwrap(api.POST('/subjects', { body: { name: name.trim(), code: code.trim() || null, full_score: 150 } }));
      onSaved();
    } catch (e) {
      if (e instanceof ApiError && e.isQuotaExceeded) onQuota();
      else toast(e instanceof ApiError ? e.message : '添加失败，请重试');
    } finally {
      setBusy(false);
    }
  };
  return (
    <BottomSheet visible onClose={onClose} title="添加专业课">
      <TextInput accessibilityLabel="专业课名称" placeholder="专业课名称" value={name} onChangeText={setName} style={styles.input} maxLength={64} maxFontSizeMultiplier={layout.maxFontScale} />
      <TextInput accessibilityLabel="专业课代码" placeholder="代码（选填）" value={code} onChangeText={setCode} style={[styles.input, styles.inputGap]} maxLength={16} maxFontSizeMultiplier={layout.maxFontScale} />
      <Button title="添加" disabled={!name.trim()} loading={busy} onPress={() => void add()} style={styles.sheetButton} />
    </BottomSheet>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing.xxxl },
  back: { alignSelf: 'flex-start', marginTop: spacing.sm, paddingHorizontal: 0 },
  section: { marginTop: spacing.xl, marginBottom: spacing.sm },
  gap: { gap: spacing.sm },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, minHeight: layout.minTouch },
  flex: { flex: 1 },
  note: { marginTop: spacing.xl },
  input: { minHeight: 48, borderWidth: 1, borderColor: semantic.border, borderRadius: radius.md, paddingHorizontal: spacing.md, fontSize: 16, color: semantic.textPrimary },
  inputGap: { marginTop: spacing.sm },
  sheetRow: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', marginTop: spacing.md, flexWrap: 'wrap' },
  sheetButton: { marginTop: spacing.lg },
});
