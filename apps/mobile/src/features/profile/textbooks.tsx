// 参考教材（PRD v3 1.3、6.9，选填）：每门专业课可填多本（最多 5 本），只填书名，不需要上传教材。
// 资料里缺答案时，AI 会尽量按这本教材的表述给参考答案。1.3 用 TextbookList，6.9 用 TextbookSheet。
import type { Schemas } from '@training/api-client';
import { layout, radius, semantic, spacing } from '@training/ui-tokens';
import { useState } from 'react';
import { Pressable, StyleSheet, TextInput, View } from 'react-native';
import { BottomSheet, Button, Icon, Text } from '@/components';
import { Chips } from '@/features/onboarding/ui';
import { api, unwrap } from '@/lib/api';

export const MAX_TEXTBOOKS = 5;
export const textbookHint = '资料里缺答案时，AI 会尽量按这本教材的表述给参考答案';

export type TextbookMap = Record<number, string[]>;

export function textbooksOf(subjects: Schemas['Subject'][]): TextbookMap {
  return Object.fromEntries(subjects.map((s) => [s.id, s.textbooks ?? []]));
}

/** 只保存改过的专业课。 */
export async function saveTextbooks(subjects: Schemas['Subject'][], next: TextbookMap) {
  for (const s of subjects) {
    const before = (s.textbooks ?? []).join('\n');
    const after = (next[s.id] ?? []).join('\n');
    if (before === after) continue;
    await unwrap(api.PATCH('/subjects/{subjectId}', { params: { path: { subjectId: s.id } }, body: { textbooks: next[s.id] ?? [] } }));
  }
}

/** 6.9 的一行摘要：「654 · 童庆炳《文学理论教程》等 2 本」。 */
export function textbookSummary(s: Schemas['Subject']) {
  const books = s.textbooks ?? [];
  const label = s.code || s.name;
  if (books.length === 0) return `${label} · 未填写`;
  return books.length === 1 ? `${label} · ${books[0]}` : `${label} · ${books[0]}等 ${books.length} 本`;
}

/** 1.3：教材列表（书名 + 专业课代码，可删）+「添加教材」。 */
export function TextbookList({ subjects, value, onChange }: { subjects: Schemas['Subject'][]; value: TextbookMap; onChange: (v: TextbookMap) => void }) {
  const [adding, setAdding] = useState(false);
  const rows = subjects.flatMap((s) => (value[s.id] ?? []).map((b) => ({ subject: s, book: b })));
  const full = subjects.every((s) => (value[s.id] ?? []).length >= MAX_TEXTBOOKS);
  return (
    <View style={styles.gap}>
      {rows.map(({ subject, book }) => (
        <View key={`${subject.id}-${book}`} style={styles.row}>
          <Text variant="body" style={styles.flex}>
            {book}
          </Text>
          {subjects.length > 1 || subject.code ? (
            <Text variant="small" num>
              {subject.code || subject.name}
            </Text>
          ) : null}
          <Pressable
            accessibilityRole="button"
            accessibilityLabel={`删除 ${book}`}
            hitSlop={10}
            onPress={() => onChange({ ...value, [subject.id]: (value[subject.id] ?? []).filter((b) => b !== book) })}
          >
            <Icon name="close" size={16} color={semantic.textSecondary} />
          </Pressable>
        </View>
      ))}
      {!full ? (
        <Pressable accessibilityRole="button" onPress={() => setAdding(true)} style={styles.add}>
          <Text variant="caption">+ 添加教材</Text>
        </Pressable>
      ) : null}
      <Text variant="small" style={styles.hint}>
        {textbookHint}
      </Text>
      {adding ? (
        <AddTextbookSheet
          subjects={subjects.filter((s) => (value[s.id] ?? []).length < MAX_TEXTBOOKS)}
          onClose={() => setAdding(false)}
          onAdd={(subjectId, title) => {
            const cur = value[subjectId] ?? [];
            if (!cur.includes(title)) onChange({ ...value, [subjectId]: [...cur, title] });
            setAdding(false);
          }}
        />
      ) : null}
    </View>
  );
}

function AddTextbookSheet({ subjects, onClose, onAdd }: { subjects: Schemas['Subject'][]; onClose: () => void; onAdd: (subjectId: number, title: string) => void }) {
  const [subjectId, setSubjectId] = useState<number>(subjects[0]?.id ?? 0);
  const [title, setTitle] = useState('');
  return (
    <BottomSheet visible onClose={onClose} title="添加参考教材">
      {subjects.length > 1 ? (
        <View style={styles.sheetGap}>
          <Text variant="caption">用于哪门专业课</Text>
          <Chips options={subjects.map((s) => s.id)} value={subjectId} onChange={setSubjectId} format={(id) => { const s = subjects.find((x) => x.id === id); return s?.code ? `${s.code} ${s.name}` : s?.name ?? ''; }} />
        </View>
      ) : null}
      <TextInput
        accessibilityLabel="教材名称"
        value={title}
        onChangeText={setTitle}
        placeholder="如：童庆炳《文学理论教程》"
        placeholderTextColor={semantic.textTertiary}
        maxLength={64}
        maxFontSizeMultiplier={layout.maxFontScale}
        style={styles.input}
        autoFocus
      />
      <Text variant="small" style={styles.hint}>
        写上作者和书名即可，不需要上传教材
      </Text>
      <Button title="添加" disabled={!title.trim() || !subjectId} onPress={() => subjectId && onAdd(subjectId, title.trim())} style={styles.sheetButton} />
    </BottomSheet>
  );
}

/** 6.9：编辑一门专业课的参考教材，保存即生效。 */
export function TextbookSheet({ subject, onClose, onSaved }: { subject: Schemas['Subject']; onClose: () => void; onSaved: () => void }) {
  const [books, setBooks] = useState<string[]>(subject.textbooks ?? []);
  const [title, setTitle] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const add = () => {
    const t = title.trim();
    if (t && !books.includes(t) && books.length < MAX_TEXTBOOKS) setBooks([...books, t]);
    setTitle('');
  };
  const save = async () => {
    setBusy(true);
    try {
      const pending = title.trim() && !books.includes(title.trim()) ? [...books, title.trim()].slice(0, MAX_TEXTBOOKS) : books;
      await unwrap(api.PATCH('/subjects/{subjectId}', { params: { path: { subjectId: subject.id } }, body: { textbooks: pending } }));
      onSaved();
    } catch (e) {
      setError(e instanceof Error ? e.message : '保存失败，请重试');
    } finally {
      setBusy(false);
    }
  };
  return (
    <BottomSheet visible onClose={onClose} title={`参考教材 · ${subject.code || subject.name}`}>
      <Text variant="small" style={styles.hint}>
        {textbookHint}
      </Text>
      <View style={[styles.gap, styles.sheetList]}>
        {books.map((b) => (
          <View key={b} style={styles.row}>
            <Text variant="body" style={styles.flex}>
              {b}
            </Text>
            <Pressable accessibilityRole="button" accessibilityLabel={`删除 ${b}`} hitSlop={10} onPress={() => setBooks(books.filter((x) => x !== b))}>
              <Icon name="close" size={16} color={semantic.textSecondary} />
            </Pressable>
          </View>
        ))}
        {books.length < MAX_TEXTBOOKS ? (
          <View style={styles.addRow}>
            <TextInput
              accessibilityLabel="教材名称"
              value={title}
              onChangeText={setTitle}
              onSubmitEditing={add}
              placeholder="如：童庆炳《文学理论教程》"
              placeholderTextColor={semantic.textTertiary}
              maxLength={64}
              maxFontSizeMultiplier={layout.maxFontScale}
              style={[styles.input, styles.flex]}
            />
            <Button title="添加" kind="secondary" size="sm" disabled={!title.trim()} onPress={add} />
          </View>
        ) : null}
      </View>
      {error ? (
        <Text variant="caption" color={semantic.danger}>
          {error}
        </Text>
      ) : null}
      <Button title="保存" loading={busy} onPress={() => void save()} style={styles.sheetButton} />
    </BottomSheet>
  );
}

const styles = StyleSheet.create({
  gap: { gap: 8 },
  sheetGap: { gap: 8, marginBottom: 12 },
  sheetList: { marginVertical: 12 },
  flex: { flex: 1 },
  row: { flexDirection: 'row', alignItems: 'center', gap: 10, minHeight: 48, paddingHorizontal: 14, borderRadius: 14, borderWidth: 1, borderColor: semantic.inputBorder },
  add: { minHeight: 44, alignItems: 'center', justifyContent: 'center', borderRadius: 14, borderWidth: 1, borderStyle: 'dashed', borderColor: semantic.textTertiary },
  addRow: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  hint: { lineHeight: 19 },
  input: { minHeight: 48, borderWidth: 1, borderColor: semantic.inputBorder, borderRadius: radius.md, paddingHorizontal: spacing.md, fontSize: 16, color: semantic.textPrimary },
  sheetButton: { marginTop: spacing.md },
});
