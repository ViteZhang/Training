// 6.13 意见反馈：类型（功能建议、识别不准、批改不准、出现问题、侵权投诉）；详细描述；截图最多 3 张；
// 可勾选「允许客服查看相关资料排查问题（72 小时内有效，每次查看都会通知你）」；回复在消息中心；历史记录。
import type { Schemas } from '@training/api-client';
import { radius, semantic, spacing } from '@training/ui-tokens';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { File } from 'expo-file-system';
import * as ImagePicker from 'expo-image-picker';
import { router } from 'expo-router';
import { useState } from 'react';
import { Image, Pressable, ScrollView, StyleSheet, TextInput, View } from 'react-native';
import { Button, Card, EmptyState, Screen, Tag, Text, toast } from '@/components';
import { Checkbox, PageHeader } from '@/features/import/ui';
import { feedbackTypes, mineKeys, ymd } from '@/features/mine/api';
import { api, unwrap } from '@/lib/api';

type Shot = { uri: string; mimeType: 'image/jpeg' | 'image/png' | 'image/heic' | 'image/webp'; size: number };
const maxShots = 3;

/** 截图直传 OSS（复用手写稿的直传接口），返回对象键。 */
async function upload(shots: Shot[]): Promise<string[]> {
  if (shots.length === 0) return [];
  const req = await unwrap(api.POST('/handwriting/upload-requests', { body: { files: shots.map((s) => ({ content_type: s.mimeType, size: s.size })) } }));
  await Promise.all(
    req.items.map(async (t, i) => {
      const r = await new File(shots[i]!.uri).upload(t.upload_url, { httpMethod: 'PUT', headers: t.upload_headers });
      if (r.status < 200 || r.status >= 300) throw new Error(`第 ${i + 1} 张截图上传失败`);
    }),
  );
  return req.items.map((t) => t.object_key);
}

function History() {
  const list = useQuery({ queryKey: mineKeys.feedbacks, queryFn: () => unwrap(api.GET('/feedbacks')) });
  const items = list.data?.items ?? [];
  if (items.length === 0) return <EmptyState title="还没有反馈" desc="提交的反馈和回复会显示在这里" />;
  return (
    <View style={styles.gap}>
      {items.map((f: Schemas['Feedback']) => (
        <Card key={f.id} style={styles.gap}>
          <View style={styles.row}>
            <Tag label={feedbackTypes.find((t) => t.key === f.ftype)?.label ?? f.ftype} />
            <Text variant="caption" style={styles.flex}>
              {ymd(f.created_at)}
            </Text>
            <Text variant="caption" color={f.status === 'replied' ? semantic.mastered : undefined}>
              {f.status === 'replied' ? '已回复' : f.status === 'closed' ? '已关闭' : '处理中'}
            </Text>
          </View>
          <Text variant="body">{f.content}</Text>
          {f.reply ? <Text variant="caption">回复：{f.reply}</Text> : null}
        </Card>
      ))}
    </View>
  );
}

export default function FeedbackPage() {
  const qc = useQueryClient();
  const [showHistory, setShowHistory] = useState(false);
  const [type, setType] = useState<Schemas['FeedbackType']>();
  const [content, setContent] = useState('');
  const [shots, setShots] = useState<Shot[]>([]);
  const [allow, setAllow] = useState(false);
  const submit = useMutation({
    mutationFn: async () => {
      const keys = await upload(shots);
      return unwrap(api.POST('/feedbacks', { body: { ftype: type!, content: content.trim(), screenshot_keys: keys, allow_access: allow } }));
    },
    onSuccess: () => {
      toast('已提交，我们会通过消息中心回复你');
      void qc.invalidateQueries({ queryKey: mineKeys.feedbacks });
      setType(undefined);
      setContent('');
      setShots([]);
      setAllow(false);
      setShowHistory(true);
    },
    onError: (e) => toast(e instanceof Error ? e.message : '提交失败，请重试'),
  });
  const pick = async () => {
    const res = await ImagePicker.launchImageLibraryAsync({ mediaTypes: ['images'], quality: 0.8, allowsMultipleSelection: true, selectionLimit: maxShots - shots.length });
    if (res.canceled) return;
    const next = res.assets.map((a) => ({
      uri: a.uri,
      mimeType: (['image/png', 'image/heic', 'image/webp'].includes(a.mimeType ?? '') ? a.mimeType : 'image/jpeg') as Shot['mimeType'],
      size: a.fileSize ?? new File(a.uri).size,
    }));
    setShots((s) => [...s, ...next].slice(0, maxShots));
  };
  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll} keyboardShouldPersistTaps="handled">
        <PageHeader title="意见反馈" onBack={() => router.back()} right={<Button title={showHistory ? '写反馈' : '历史'} kind="text" onPress={() => setShowHistory((v) => !v)} />} />
        {showHistory ? (
          <History />
        ) : (
          <>
            <Text variant="bodyStrong">反馈类型</Text>
            <View style={styles.types}>
              {feedbackTypes.map((t) => (
                <Pressable
                  key={t.key}
                  accessibilityRole="radio"
                  accessibilityState={{ selected: type === t.key }}
                  onPress={() => setType(t.key)}
                  style={[styles.type, type === t.key && styles.typeOn]}
                >
                  <Text variant="caption" color={type === t.key ? semantic.textOnBrand : undefined}>
                    {t.label}
                  </Text>
                </Pressable>
              ))}
            </View>
            <Text variant="bodyStrong">详细描述</Text>
            <TextInput
              accessibilityLabel="详细描述"
              multiline
              value={content}
              onChangeText={setContent}
              placeholder={type === 'infringement' ? '请写明被侵权的内容、你的权利证明与联系方式' : '遇到了什么问题，在哪个页面'}
              placeholderTextColor={semantic.textSecondary}
              style={styles.input}
              textAlignVertical="top"
            />
            <Text variant="bodyStrong">截图</Text>
            <View style={styles.row}>
              {shots.map((s, i) => (
                <Pressable key={s.uri} accessibilityLabel={`删除第 ${i + 1} 张截图`} onPress={() => setShots((all) => all.filter((x) => x !== s))}>
                  <Image source={{ uri: s.uri }} style={styles.thumb} />
                </Pressable>
              ))}
              {shots.length < maxShots ? <Button title="+ 添加" kind="secondary" onPress={() => void pick()} /> : null}
            </View>
            <Text variant="small">选填，最多 3 张；点截图可删除</Text>
            <Checkbox checked={allow} onChange={setAllow} label="允许客服查看相关资料排查问题（72 小时内有效，每次查看都会通知你）" />
            <Text variant="caption">我们会通过消息中心回复你</Text>
          </>
        )}
      </ScrollView>
      {!showHistory ? (
        <Button title="提交" disabled={!type || content.trim().length < 5} loading={submit.isPending} onPress={() => submit.mutate()} />
      ) : null}
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { gap: spacing.md, paddingBottom: spacing.xl },
  gap: { gap: spacing.sm },
  flex: { flex: 1 },
  row: { flexDirection: 'row', alignItems: 'center', flexWrap: 'wrap', gap: spacing.sm },
  types: { flexDirection: 'row', flexWrap: 'wrap', gap: spacing.sm },
  type: { minHeight: 40, justifyContent: 'center', paddingHorizontal: spacing.md, borderRadius: radius.lg, borderWidth: 1, borderColor: semantic.border },
  typeOn: { backgroundColor: semantic.primary, borderColor: semantic.primary },
  input: { minHeight: 140, padding: spacing.md, borderRadius: radius.md, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface, fontSize: 16, lineHeight: 24 },
  thumb: { width: 64, height: 64, borderRadius: radius.md },
});
