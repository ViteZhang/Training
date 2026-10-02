// 4.3 答题：选项、判分结果、解析、出处与知识点入口。主观题在 T18 接通打字作答与 AI 批改，这里先提供「看参考答案」自评。
import type { Schemas } from '@training/api-client';
import { radius, semantic, spacing } from '@training/ui-tokens';
import { router } from 'expo-router';
import { Pressable, StyleSheet, TextInput, View } from 'react-native';
import { Button, Tag, Text } from '@/components';
import { qtypeNames } from '@/features/import/api';
import { groupTagNames, type PracticeQuestion } from './api';

export interface LocalResult {
  correct: boolean | null;
  revealed: boolean;
  /** 已提交到服务端（离线时为 false，等联网补交） */
  synced: boolean;
  answer?: string;
  analysis?: string;
  wrongBook?: Schemas['AttemptResult']['wrong_book'];
  selfAssess?: Schemas['SelfAssessLevel'];
}

export function QuestionHeader({ q }: { q: PracticeQuestion }) {
  return (
    <View style={styles.tags}>
      <Tag label={qtypeNames[q.qtype]} tone="brand" />
      {q.plan_group ? <Tag label={groupTagNames[q.plan_group]} tone="info" /> : null}
      {q.origin_tags.includes('ai_generated') ? <Tag label="AI 出题" tone="ai" /> : null}
      {q.source === 'exam' && q.exam_year ? <Tag label={`${q.exam_year} 真题`} /> : null}
    </View>
  );
}

export function Options({ q, selected, result, onToggle }: { q: PracticeQuestion; selected: string[]; result?: LocalResult; onToggle: (key: string) => void }) {
  const answerKeys = result ? (q.answer ?? '').toUpperCase() : '';
  return (
    <View style={styles.options}>
      {(q.options ?? []).map((o) => {
        const picked = selected.includes(o.key);
        const right = !!result && answerKeys.includes(o.key.toUpperCase());
        const wrong = !!result && picked && !right;
        return (
          <Pressable
            key={o.key}
            accessibilityRole={q.qtype === 'multi_choice' ? 'checkbox' : 'radio'}
            accessibilityState={{ checked: picked, disabled: !!result }}
            accessibilityLabel={`${o.key}. ${o.text}`}
            disabled={!!result}
            onPress={() => onToggle(o.key)}
            style={[styles.option, picked && styles.optionOn, right && styles.optionRight, wrong && styles.optionWrong]}
          >
            <Text variant="bodyStrong" style={styles.letter}>
              {o.key}
            </Text>
            <Text variant="body" style={styles.flex}>
              {o.text}
            </Text>
          </Pressable>
        );
      })}
    </View>
  );
}

export function FillBlank({ value, onChange, disabled }: { value: string; onChange: (v: string) => void; disabled: boolean }) {
  return (
    <TextInput
      accessibilityLabel="填空作答"
      value={value}
      onChangeText={onChange}
      editable={!disabled}
      placeholder="多个空用；隔开"
      style={styles.input}
      placeholderTextColor={semantic.textSecondary}
    />
  );
}

export function ResultPanel({ q, result, onReport }: { q: PracticeQuestion; result: LocalResult; onReport: () => void }) {
  const answer = result.answer ?? q.answer;
  const analysis = result.analysis ?? q.analysis;
  const title = result.revealed ? '先看答案，记下来' : result.correct === true ? '回答正确' : result.correct === false ? '回答错误' : '已记录';
  const color = result.revealed ? semantic.info : result.correct ? semantic.mastered : result.correct === false ? semantic.danger : semantic.textPrimary;
  return (
    <View style={styles.result}>
      <Text variant="h3" color={color}>
        {title}
      </Text>
      {answer ? (
        <Text variant="body">
          {q.options?.length ? `正确答案是 ${answer}` : `参考答案：${answer}`}
        </Text>
      ) : null}
      {analysis ? <Text variant="body" color={semantic.textSecondary}>{analysis}</Text> : null}
      {result.wrongBook === 'added' ? <Text variant="caption" color={semantic.danger}>已加入错题本</Text> : null}
      {result.wrongBook === 'removed' ? <Text variant="caption" color={semantic.mastered}>两次答对，已从错题本移出</Text> : null}
      {!result.synced ? <Text variant="caption" color={semantic.info}>离线作答，联网后自动提交并复核</Text> : null}
      {q.source_ref ? (
        <Text variant="caption">
          {q.origin_tags.includes('ai_generated') && q.knowledge_points[0] ? `AI 按你的知识点「${q.knowledge_points[0].name}」出题 · ` : ''}
          依据 {q.source_ref.file_name}
          {q.source_ref.page ? ` 第 ${q.source_ref.page} 页` : ''}
        </Text>
      ) : null}
      <View style={styles.links}>
        {q.knowledge_points.slice(0, 2).map((k) => (
          <Button key={k.id} title={`知识点：${k.name} ›`} kind="text" onPress={() => router.push({ pathname: '/bank/kp/[id]', params: { id: String(k.id) } })} />
        ))}
        <Button title="题目有问题" kind="text" onPress={onReport} />
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  tags: { flexDirection: 'row', flexWrap: 'wrap', gap: spacing.xs },
  options: { gap: spacing.sm },
  option: { flexDirection: 'row', alignItems: 'center', gap: spacing.md, minHeight: 52, padding: spacing.md, borderRadius: radius.md, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface },
  optionOn: { borderColor: semantic.primary, backgroundColor: semantic.primarySoft },
  optionRight: { borderColor: semantic.mastered, backgroundColor: semantic.masteredSoft },
  optionWrong: { borderColor: semantic.danger, backgroundColor: semantic.dangerSoft },
  letter: { width: 24 },
  flex: { flex: 1 },
  input: { minHeight: 52, borderWidth: 1, borderColor: semantic.border, borderRadius: radius.md, paddingHorizontal: spacing.md, fontSize: 16, color: semantic.textPrimary, backgroundColor: semantic.surface },
  result: { gap: spacing.sm, padding: spacing.lg, borderRadius: radius.lg, backgroundColor: semantic.surface, borderWidth: 1, borderColor: semantic.border },
  links: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center' },
});
