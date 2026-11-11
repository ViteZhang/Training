// 6.9 目标院校与专业：院校从考试科目库里搜索选择（和 1.1 同一个来源），库里没有的可以直接用输入的名称；专业选填。
// 存成一句话「院校 · 专业」（profile.target_school_major），用来匹配官方题库、上线时提醒。
import { layout, radius, semantic, spacing } from '@training/ui-tokens';
import { useEffect, useState } from 'react';
import { Pressable, StyleSheet, TextInput, View } from 'react-native';
import { BottomSheet, Button, Loading, Text } from '@/components';
import { useCatalogSchools } from '@/features/official/api';
import { splitTarget } from './target';

export function SchoolSheet({ value, onClose, onSave }: { value?: string | null; onClose: () => void; onSave: (v: string | null) => void }) {
  const init = splitTarget(value);
  const [query, setQuery] = useState(init.school);
  const [school, setSchool] = useState(init.school);
  const [major, setMajor] = useState(init.major);
  const [term, setTerm] = useState('');
  useEffect(() => {
    const t = setTimeout(() => setTerm(query.trim() === school ? '' : query), 300);
    return () => clearTimeout(t);
  }, [query, school]);
  const schools = useCatalogSchools(term);
  const items = schools.data?.items ?? [];
  const typed = query.trim();
  const searching = !!term && typed !== school;
  const exact = items.some((s) => s.name === typed);
  const pick = (name: string) => {
    setSchool(name);
    setQuery(name);
  };
  return (
    <BottomSheet visible onClose={onClose} title="目标院校与专业">
      <View style={styles.gap}>
        <TextInput accessibilityLabel="目标院校" value={query} onChangeText={setQuery} placeholder="搜索院校，如：海南大学" style={styles.input} maxLength={32} maxFontSizeMultiplier={layout.maxFontScale} />
        {searching ? (
          <View style={styles.list}>
            {schools.isLoading ? <Loading rows={2} /> : null}
            {items.slice(0, 6).map((s, i) => (
              <Pressable key={s.name} accessibilityRole="button" onPress={() => pick(s.name)} style={[styles.row, i > 0 && styles.divider]}>
                <Text variant="body" style={styles.flex}>{s.name}</Text>
                <Text variant="small" num>{s.course_count} 门课</Text>
              </Pressable>
            ))}
            {schools.data && !exact && typed ? (
              <Pressable accessibilityRole="button" onPress={() => pick(typed)} style={[styles.row, items.length > 0 && styles.divider]}>
                <View style={styles.flex}>
                  <Text variant="body">就用「{typed}」</Text>
                  <Text variant="small">库里还没有这所院校，官方题库上线后会提醒你</Text>
                </View>
              </Pressable>
            ) : null}
          </View>
        ) : null}
        <TextInput accessibilityLabel="报考专业" value={major} onChangeText={setMajor} placeholder="专业（选填），如：中国语言文学" style={styles.input} maxLength={30} maxFontSizeMultiplier={layout.maxFontScale} />
        <Button
          title="保存"
          disabled={searching}
          onPress={() => {
            const m = major.trim();
            onSave(school ? (m ? `${school} · ${m}` : school) : null);
          }}
        />
        {school || value ? <Button title="清除目标院校" kind="text" onPress={() => onSave(null)} /> : null}
      </View>
    </BottomSheet>
  );
}

const styles = StyleSheet.create({
  gap: { gap: spacing.md },
  flex: { flex: 1 },
  input: { minHeight: 48, borderWidth: 1, borderColor: semantic.inputBorder, borderRadius: radius.md, paddingHorizontal: spacing.md, fontSize: 16, color: semantic.textPrimary },
  list: { borderWidth: 1, borderColor: semantic.border, borderRadius: radius.md, paddingHorizontal: spacing.md },
  row: { flexDirection: 'row', alignItems: 'center', gap: 8, minHeight: 48, paddingVertical: 8 },
  divider: { borderTopWidth: 1, borderTopColor: semantic.border },
});
