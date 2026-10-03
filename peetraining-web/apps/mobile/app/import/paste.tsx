// 1.5b 粘贴文字导入：格式示例、字数与上限；单次最多 2 万字（PRD 11.12）。创建资料后回到 1.5 一起开始解析。
import { ApiError } from '@training/api-client';
import { layout, radius, semantic, spacing } from '@training/ui-tokens';
import { router } from 'expo-router';
import { useState } from 'react';
import { StyleSheet, TextInput, View } from 'react-native';
import { Button, Screen, Text, toast } from '@/components';
import { limits } from '@/features/import/files';
import { useImportFlow } from '@/features/import/store';
import { Checkbox, PageHeader } from '@/features/import/ui';
import { Footer } from '@/features/onboarding/ui';
import { api, unwrap } from '@/lib/api';

const example = `一、名词解释（每题 5 分）
1. 典型
答案：典型是文学形象的高级形态之一，指……

2. 陌生化
答案：陌生化是俄国形式主义提出的概念……

二、简答题（每题 15 分）
1. 简述文学作品的层次结构。`;

export default function PasteScreen() {
  const { subjectId, mode, add } = useImportFlow();
  const [text, setText] = useState('');
  const [agreed, setAgreed] = useState(false);
  const [busy, setBusy] = useState(false);
  const count = [...text.trim()].length;
  const over = count > limits.maxPasteChars;

  const submit = async () => {
    if (!subjectId) return;
    setBusy(true);
    try {
      const m = await unwrap(api.POST('/materials/paste', { body: { subject_id: subjectId, category: mode, text: text.trim(), right_confirmed: true } }));
      add([{ key: `paste-${m.id}`, name: m.file_name, format: 'text', size: count, materialId: m.id, status: 'uploaded', progress: 1 }]);
      router.back();
    } catch (e) {
      toast(e instanceof ApiError ? e.message : '没保存成功，请重试');
    } finally {
      setBusy(false);
    }
  };

  return (
    <Screen>
      <PageHeader title="粘贴文字" onBack={() => router.back()} />
      <TextInput
        accessibilityLabel="要导入的文字"
        multiline
        placeholder={example}
        placeholderTextColor={semantic.textSecondary}
        value={text}
        onChangeText={setText}
        style={styles.input}
        textAlignVertical="top"
        maxFontSizeMultiplier={layout.maxFontScale}
      />
      <View style={styles.meta}>
        <Text variant="caption">每道题之间空一行识别更准；答案写在题目下方，以「答案：」开头</Text>
        <Text variant="caption" color={over ? semantic.danger : semantic.textSecondary}>
          {count} / {limits.maxPasteChars} 字
        </Text>
      </View>
      <Footer>
        <Checkbox checked={agreed} onChange={setAgreed} label="我确认对这些资料有合法的使用权，仅用于本人学习" />
        <Button title="识别这段文字" disabled={!agreed || count === 0 || over} loading={busy} onPress={() => void submit()} />
      </Footer>
    </Screen>
  );
}

const styles = StyleSheet.create({
  input: { flex: 1, minHeight: 240, borderWidth: 1, borderColor: semantic.border, borderRadius: radius.md, padding: spacing.md, fontSize: 16, lineHeight: 24, color: semantic.textPrimary, backgroundColor: semantic.surface },
  meta: { marginTop: spacing.sm, gap: spacing.xs },
});
