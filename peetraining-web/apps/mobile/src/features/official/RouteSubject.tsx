// 1.1 选院校和专业课（PRD v3 增补「引导分流」，设计稿 m1_subject）：选考试年份 → 搜索目标院校 → 按院校列出专业与专业课（最多 4 门），
// 每门标「有官方题库 / 暂无官方题库」；列表里没有时手动添加（代码 + 名称）。「下一步」按匹配结果去 1.1a 或 1.1c；
// 「院校还没定，先随便看看」去 1.10。搜不到的院校可以直接用输入的名称，专业课手动添加（进后台「待入库」），下一步去 1.1c。院校与专业课来自后台维护的院校专业课库（onboarding_route 开关打开时才用这一页）。
import { ApiError } from '@training/api-client';
import { colors, layout, radius, semantic, spacing } from '@training/ui-tokens';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { router } from 'expo-router';
import { useEffect, useMemo, useState } from 'react';
import { Pressable, ScrollView, StyleSheet, TextInput, View } from 'react-native';
import { BottomSheet, Button, ErrorState, Icon, Loading, QuotaSheet, Screen, Tag, Text, toast } from '@/components';
import { useUpgradeActions } from '@/features/membership/purchase';
import { saveDraft, loadDraft } from '@/features/onboarding/draft';
import { Footer, StepHeader } from '@/features/onboarding/ui';
import { ExamYearSelect } from '@/features/profile/ExamYearSelect';
import { track } from '@/lib/analytics';
import { api, unwrap } from '@/lib/api';
import { useCatalogCourses, useCatalogSchools, type CatalogCourse } from './api';

const MAX_COURSES = 4;

interface Manual {
  name: string;
  code: string;
  major: string;
}

/** 代码 + 名称写在一个框里，开头是 3 位数字时拆成代码（与旧 1.1 一致）。 */
export function parseCourse(text: string): { code: string; name: string } {
  const t = text.trim();
  const m = /^(\d{3})\s*(.+)$/.exec(t);
  return m ? { code: m[1] ?? '', name: (m[2] ?? '').trim() } : { code: '', name: t };
}

/** 这一组课怎么分流：有一门有官方题库就先去 1.1a，否则去 1.1c。 */
export function routeOf(courses: Pick<CatalogCourse, 'official_status'>[]): 'official' | 'none' {
  return courses.some((c) => c.official_status === 'available') ? 'official' : 'none';
}

export function RouteSubject() {
  const qc = useQueryClient();
  const draft = loadDraft();
  const years = useQuery({ queryKey: ['exam-years'], queryFn: () => unwrap(api.GET('/exam-years')) });
  const subjects = useQuery({ queryKey: ['subjects'], queryFn: () => unwrap(api.GET('/subjects')) });
  const [year, setYear] = useState<number | undefined>(draft.examYear);
  const [query, setQuery] = useState(draft.school ?? '');
  const [term, setTerm] = useState('');
  const [school, setSchool] = useState<string | undefined>(draft.school);
  const [picked, setPicked] = useState<number[]>(draft.picked ?? []);
  const [manual, setManual] = useState<Manual[]>([]);
  const [adding, setAdding] = useState(false);
  const [busy, setBusy] = useState(false);
  const [quota, setQuota] = useState(false);
  const upgrade = useUpgradeActions(() => setQuota(false));

  const selectedYear = years.data?.items.find((y) => y.exam_year === year) ?? years.data?.items[0];
  // 输入停顿 300 毫秒再搜，免得每个字都请求。
  useEffect(() => {
    const t = setTimeout(() => setTerm(query.trim() === school ? '' : query), 300);
    return () => clearTimeout(t);
  }, [query, school]);
  const schools = useCatalogSchools(term);
  const courses = useCatalogCourses(school, selectedYear?.exam_year);
  const list = useMemo(() => courses.data?.items ?? [], [courses.data]);
  const count = picked.length + manual.length;

  const toggle = (c: CatalogCourse) => {
    if (picked.includes(c.id)) setPicked(picked.filter((id) => id !== c.id));
    else if (count >= MAX_COURSES) toast(`最多选 ${MAX_COURSES} 门`);
    else setPicked([...picked, c.id]);
  };
  const chosen = useMemo(() => list.filter((c) => picked.includes(c.id)), [list, picked]);

  /** 默认备考档案：阶段按距初试天数推荐、每天 45 分钟；1.3 移到首次批改之后，那时再改（PRD v3 增补）。 */
  const saveProfile = async (target: string | null) => {
    if (!selectedYear) return;
    await unwrap(
      api.PUT('/profile', { body: { exam_year: selectedYear.exam_year, stage: selectedYear.suggested_stage, daily_minutes: 45, target_school_major: target } }),
    );
  };

  const next = async () => {
    if (!school || count === 0 || !selectedYear) return;
    setBusy(true);
    try {
      const existing = subjects.data?.items ?? [];
      const noOfficial: NonNullable<ReturnType<typeof loadDraft>['noOfficial']> = [];
      for (const c of chosen) {
        let s = existing.find((x) => x.catalog_course_id === c.id);
        if (!s) {
          s = await unwrap(
            api.POST('/subjects', { body: { name: c.subject_name, code: c.subject_code ?? null, full_score: 150, catalog_course_id: c.id } }),
          );
        }
        if (c.official_status !== 'available') noOfficial.push({ subjectId: s.id, name: c.subject_name, code: c.subject_code, major: c.major });
      }
      for (const m of manual) {
        const s = await unwrap(api.POST('/subjects', { body: { name: m.name, code: m.code || null, full_score: 150, school, major: m.major } }));
        noOfficial.push({ subjectId: s.id, name: m.name, code: m.code || undefined, major: m.major });
      }
      const major = chosen[0]?.major ?? manual[0]?.major ?? '';
      const target = major ? `${school} · ${major}` : school;
      await saveProfile(target);
      saveDraft({ examYear: selectedYear.exam_year, school, major, picked, noOfficial, targetSchoolMajor: target });
      await qc.invalidateQueries({ queryKey: ['subjects'] });
      const route = routeOf(chosen);
      track('onboarding_route', { route: route === 'official' ? 'official' : 'none', code: chosen[0]?.subject_code ?? manual[0]?.code });
      setManual([]);
      router.push(route === 'official' ? '/(onboarding)/official' : '/(onboarding)/no-official');
    } catch (e) {
      if (e instanceof ApiError && e.isQuotaExceeded) setQuota(true);
      else toast(e instanceof ApiError ? e.message : '保存失败，请重试');
    } finally {
      setBusy(false);
    }
  };

  const undecided = async () => {
    setBusy(true);
    try {
      await saveProfile(null);
      saveDraft({ examYear: selectedYear?.exam_year, school: undefined, picked: [], noOfficial: [] });
      track('onboarding_route', { route: 'undecided' });
      router.push({ pathname: '/trial/samples', params: { from: 'onboarding' } });
    } catch (e) {
      toast(e instanceof ApiError ? e.message : '保存失败，请重试');
    } finally {
      setBusy(false);
    }
  };

  if (years.isLoading || subjects.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (years.isError) return <Screen><ErrorState error={years.error} onRetry={() => void years.refetch()} /></Screen>;

  const showResults = !!term && query.trim() !== school;
  // 搜索结果里没有同名院校时，可以直接用输入的院校名（PRD v3 增补 1.1：库里没有时手动添加），专业课手动添加后去 1.1c。
  const exact = (schools.data?.items ?? []).some((s) => s.name === query.trim());
  return (
    <Screen>
      <ScrollView keyboardShouldPersistTaps="handled" contentContainerStyle={styles.scroll}>
        <StepHeader step={1} title="你考哪所学校的哪门专业课？" desc="我们会先看看有没有这门课的官方题库。有就能直接刷，没有也能先体验 AI 批改。" />

        <View style={styles.block}>
          <Text variant="caption" color={semantic.textPrimary} style={styles.label}>参加哪一年的考试</Text>
          <ExamYearSelect items={years.data?.items ?? []} value={selectedYear?.exam_year} onChange={setYear} />
        </View>

        <View style={styles.block}>
          <Text variant="caption" color={semantic.textPrimary} style={styles.label}>目标院校</Text>
          <View style={[styles.search, school && query.trim() === school ? styles.searchOn : null]}>
            <Icon name="search" size={18} color={semantic.textSecondary} />
            <TextInput
              accessibilityLabel="搜索院校"
              placeholder="输入院校名称，如 四川大学"
              placeholderTextColor={colors.faint}
              value={query}
              onChangeText={(v) => {
                setQuery(v);
                if (v.trim() !== school) setSchool(undefined);
              }}
              style={styles.input}
              maxLength={64}
              maxFontSizeMultiplier={layout.maxFontScale}
            />
          </View>
          {showResults ? (
            <View style={styles.list}>
              {schools.isLoading ? <Loading rows={2} /> : null}
              {(schools.data?.items ?? []).map((s, i) => (
                <Pressable
                  key={s.name}
                  accessibilityRole="button"
                  onPress={() => {
                    setSchool(s.name);
                    setQuery(s.name);
                    setPicked([]);
                  }}
                  style={[styles.row, i > 0 && styles.divider]}
                >
                  <Text variant="body" style={styles.flex}>{s.name}</Text>
                  <Text variant="small" num>{s.course_count} 门课</Text>
                </Pressable>
              ))}
              {schools.data && !exact ? (
                <Pressable
                  accessibilityRole="button"
                  onPress={() => {
                    const name = query.trim();
                    setSchool(name);
                    setQuery(name);
                    setPicked([]);
                  }}
                  style={[styles.row, schools.data.items.length > 0 && styles.divider, styles.custom]}
                >
                  <View style={styles.flex}>
                    <Text variant="body">就用「{query.trim()}」</Text>
                    <Text variant="small">{schools.data.items.length === 0 ? '库里还没有这所院校，' : ''}下一步手动添加你要考的专业课</Text>
                  </View>
                  <Icon name="chevron" size={16} color={semantic.textSecondary} />
                </Pressable>
              ) : null}
            </View>
          ) : null}
        </View>

        {school ? (
          <View style={styles.block}>
            <View style={styles.labelRow}>
              <Text variant="caption" color={semantic.textPrimary} style={styles.label}>专业与专业课</Text>
              <Text variant="small">最多 {MAX_COURSES} 门</Text>
            </View>
            {courses.isLoading ? <Loading rows={3} /> : null}
            {courses.isError ? <ErrorState error={courses.error} onRetry={() => void courses.refetch()} /> : null}
            <View style={styles.list}>
              {list.map((c) => {
                const on = picked.includes(c.id);
                const has = c.official_status === 'available';
                return (
                  <Pressable
                    key={c.id}
                    accessibilityRole="checkbox"
                    accessibilityState={{ checked: on }}
                    onPress={() => toggle(c)}
                    style={[styles.row, styles.divider, styles.course]}
                  >
                    <View style={[styles.box, on && styles.boxOn]}>{on ? <Icon name="check" size={12} color={colors.white} /> : null}</View>
                    <View style={styles.flex}>
                      <Text variant="body">{c.subject_code ? `${c.subject_code} ` : ''}{c.subject_name}</Text>
                      <Text variant="small">{c.major}</Text>
                    </View>
                    <Tag label={has ? '有官方题库' : '暂无官方题库'} tone={has ? 'mastered' : 'neutral'} style={styles.center} />
                  </Pressable>
                );
              })}
              {manual.map((m, i) => (
                <View key={`${m.code}${m.name}${i}`} style={[styles.row, styles.divider, styles.course]}>
                  <View style={[styles.box, styles.boxOn]}><Icon name="check" size={12} color={colors.white} /></View>
                  <View style={styles.flex}>
                    <Text variant="body">{m.code ? `${m.code} ` : ''}{m.name}</Text>
                    <Text variant="small">{m.major} · 手动添加</Text>
                  </View>
                  <Pressable accessibilityRole="button" accessibilityLabel="移除" onPress={() => setManual(manual.filter((_, j) => j !== i))} style={styles.remove}>
                    <Icon name="close" size={18} color={semantic.textSecondary} />
                  </Pressable>
                </View>
              ))}
              {!courses.isLoading && list.length === 0 && manual.length === 0 ? (
                <View style={[styles.row, styles.divider]}>
                  <Text variant="caption" style={styles.flex}>这所院校的专业课还没录入，手动添加后我们会尽快补上</Text>
                </View>
              ) : null}
              <Pressable accessibilityRole="button" onPress={() => (count >= MAX_COURSES ? toast(`最多选 ${MAX_COURSES} 门`) : setAdding(true))} style={styles.row}>
                <Text variant="body" color={semantic.textSecondary}>列表里没有？手动添加专业课</Text>
              </Pressable>
            </View>
          </View>
        ) : null}
      </ScrollView>
      <Footer>
        <Button title={count === 0 ? '先选院校和专业课' : '下一步'} disabled={!school || count === 0 || !selectedYear} loading={busy} onPress={() => void next()} />
        <Button title="院校还没定，先随便看看" kind="text" disabled={busy} onPress={() => void undecided()} />
      </Footer>

      <ManualSheet
        visible={adding}
        onClose={() => setAdding(false)}
        majors={[...new Set(list.map((c) => c.major))]}
        onAdd={(m) => {
          setManual([...manual, m]);
          setAdding(false);
        }}
      />
      <QuotaSheet
        visible={quota}
        onClose={() => setQuota(false)}
        title="免费版最多添加 3 门专业课"
        desc="开通会员后可以添加第 4 门，资料解析、批改次数也不再受限。"
        upgrade={upgrade}
        freeDesc="免费版最多添加 3 门专业课，可以少选一门。"
        freeOptions={[{ label: '少选一门', onPress: () => setQuota(false) }]}
      />
    </Screen>
  );
}

/** 手动添加：专业 + 「代码 + 名称」（规则同 PRD v3 1.1），库里没有的课进后台「待入库」。 */
function ManualSheet({ visible, onClose, onAdd, majors }: { visible: boolean; onClose: () => void; onAdd: (m: Manual) => void; majors: string[] }) {
  const [major, setMajor] = useState('');
  const [text, setText] = useState('');
  const parsed = parseCourse(text);
  const ok = !!parsed.name && !!major.trim();
  return (
    <BottomSheet visible={visible} onClose={onClose} title="手动添加专业课">
      <View style={styles.sheet}>
        <Text variant="caption" color={semantic.textPrimary} style={styles.label}>专业</Text>
        <TextInput
          accessibilityLabel="专业"
          placeholder={majors[0] ? `如 ${majors[0]}` : '如 哲学'}
          placeholderTextColor={colors.faint}
          value={major}
          onChangeText={setMajor}
          style={styles.field}
          maxLength={64}
          maxFontSizeMultiplier={layout.maxFontScale}
        />
        <Text variant="caption" color={semantic.textPrimary} style={styles.label}>专业课</Text>
        <TextInput
          accessibilityLabel="专业课代码和名称"
          placeholder="代码 + 名称，如 812 西方哲学史"
          placeholderTextColor={colors.faint}
          value={text}
          onChangeText={setText}
          style={styles.field}
          maxLength={64}
          maxFontSizeMultiplier={layout.maxFontScale}
        />
        <Text variant="small">不知道代码？只写名称也可以</Text>
        <Button
          title="添加"
          disabled={!ok}
          onPress={() => {
            onAdd({ name: parsed.name, code: parsed.code, major: major.trim() });
            setText('');
          }}
        />
      </View>
    </BottomSheet>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing.xl, gap: 20 },
  block: { gap: 10 },
  label: { fontWeight: '500' },
  labelRow: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'baseline' },
  search: { flexDirection: 'row', alignItems: 'center', gap: 10, minHeight: 52, paddingHorizontal: 16, borderRadius: 16, borderWidth: 1, borderColor: semantic.inputBorder },
  searchOn: { borderWidth: 1.5, borderColor: semantic.primary },
  input: { flex: 1, minWidth: 0, minHeight: 44, fontSize: 15, padding: 0, color: semantic.textPrimary },
  list: { borderRadius: radius.card, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface, overflow: 'hidden' },
  row: { flexDirection: 'row', alignItems: 'center', gap: 12, minHeight: 52, paddingHorizontal: 16 },
  course: { minHeight: 60 },
  divider: { borderBottomWidth: 1, borderBottomColor: semantic.border },
  box: { width: 20, height: 20, borderRadius: 6, borderWidth: 1.5, borderColor: colors.faint, alignItems: 'center', justifyContent: 'center' },
  boxOn: { borderColor: semantic.primary, backgroundColor: semantic.primary },
  flex: { flex: 1, gap: 2 },
  center: { alignSelf: 'center' },
  custom: { minHeight: 60 },
  remove: { width: 44, height: 44, alignItems: 'center', justifyContent: 'center' },
  sheet: { gap: spacing.md, paddingBottom: spacing.md },
  field: { minHeight: 50, paddingHorizontal: 16, borderRadius: 14, borderWidth: 1, borderColor: semantic.inputBorder, fontSize: 15, color: semantic.textPrimary },
});
