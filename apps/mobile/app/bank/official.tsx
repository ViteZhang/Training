// 添加官方题库（PRD 5.6、11.15，T30）：已发布的官方题库列表，选专业课添加（代码相同的建议添加到那门课）、移除。
// 添加后官方知识点与题目进到这门课的题库，标「官方」，和自建的一起练；同名知识点保留你自己的表述。受 official_bank 开关控制。
import { radius, semantic, spacing } from '@training/ui-tokens';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { router } from 'expo-router';
import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { BottomSheet, Button, Card, ConfirmDialog, EmptyState, ErrorState, Loading, Screen, Tag, Text, toast } from '@/components';
import { PageHeader } from '@/features/import/ui';
import { api, unwrap } from '@/lib/api';
import type { Schemas } from '@training/api-client';
import { track } from '@/lib/analytics';

type Bank = Schemas['OfficialBank'];

export default function OfficialBanksPage() {
  const qc = useQueryClient();
  const banks = useQuery({ queryKey: ['official-banks'], queryFn: () => unwrap(api.GET('/official-banks')) });
  const subjects = useQuery({ queryKey: ['subjects'], queryFn: () => unwrap(api.GET('/subjects')) });
  const [adding, setAdding] = useState<Bank>();
  const [removing, setRemoving] = useState<Bank>();
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: ['official-banks'] });
    void qc.invalidateQueries({ queryKey: ['bank'] });
    void qc.invalidateQueries({ queryKey: ['today'] });
  };
  const add = useMutation({
    mutationFn: (v: { bank: Bank; subjectId: number }) =>
      unwrap(api.PUT('/official-banks/{bankId}/subscription', { params: { path: { bankId: v.bank.bank_id } }, body: { subject_id: v.subjectId } })),
    onSuccess: () => {
      track('official_bank_add');
      toast('已添加，官方内容标了「官方」');
      setAdding(undefined);
      refresh();
    },
    onError: () => toast('添加失败，请重试'),
  });
  const remove = useMutation({
    mutationFn: (b: Bank) => unwrap(api.DELETE('/official-banks/{bankId}/subscription', { params: { path: { bankId: b.bank_id } } })),
    onSuccess: () => {
      toast('已移除');
      setRemoving(undefined);
      refresh();
    },
    onError: () => toast('移除失败，请重试'),
  });

  if (banks.isPending || subjects.isPending) return <Screen><Loading rows={4} /></Screen>;
  if (banks.isError) return <Screen><ErrorState error={banks.error} onRetry={() => void banks.refetch()} /></Screen>;
  const list = banks.data.items;
  const subs = (subjects.data?.items ?? []).filter((s) => !s.is_essay);
  const subjectName = (id?: number) => {
    const s = subs.find((x) => x.id === id);
    return s ? `${s.code ? `${s.code} ` : ''}${s.name}` : '';
  };

  return (
    <Screen>
      <PageHeader title="添加官方题库" onBack={() => router.back()} />
      <ScrollView contentContainerStyle={styles.body}>
        <Text variant="caption">官方题库由内容团队依据公开真题和已获授权的资料整理。添加后和你自建的题库一起练；你改过的内容不会被官方更新覆盖。</Text>
        {list.length === 0 ? <EmptyState title="暂时还没有官方题库" desc="上线后会在消息中心提醒你" /> : null}
        {list.map((b) => (
          <Card key={b.bank_id} style={styles.card}>
            <View style={styles.row}>
              <Text variant="h3" style={styles.flex}>
                {b.school} {b.subject_code} {b.subject_name}
              </Text>
              <Tag label="官方" tone="brand" />
            </View>
            <Text variant="caption">
              {b.major} · {b.kp_count} 个知识点 · {b.question_count} 道题 · {b.version} 版
            </Text>
            {b.added_subject_id ? (
              <View style={styles.row}>
                <Text variant="body" style={styles.flex} color={semantic.textSecondary}>
                  已添加到 {subjectName(b.added_subject_id)}
                </Text>
                <Button title="移除" kind="text" onPress={() => setRemoving(b)} />
              </View>
            ) : (
              <Button title="添加" onPress={() => setAdding(b)} />
            )}
          </Card>
        ))}
      </ScrollView>
      <BottomSheet visible={!!adding} onClose={() => setAdding(undefined)} title="添加到哪门专业课">
        {subs.length === 0 ? <Text variant="body">先在备考设置里添加专业课</Text> : null}
        {subs.map((s) => (
          <Pressable
            key={s.id}
            accessibilityRole="button"
            disabled={add.isPending}
            onPress={() => adding && add.mutate({ bank: adding, subjectId: s.id })}
            style={styles.option}
          >
            <Text variant="bodyStrong" style={styles.flex}>
              {s.code ? `${s.code} ` : ''}
              {s.name}
            </Text>
            {adding?.suggested_subject_id === s.id ? <Tag label="建议" tone="info" /> : null}
          </Pressable>
        ))}
      </BottomSheet>
      <ConfirmDialog
        visible={!!removing}
        title="移除官方题库？"
        message="没改过的官方内容会连同练习记录一起删除；你改过的保留为自己的内容。"
        confirmText="移除"
        danger
        onConfirm={() => removing && remove.mutate(removing)}
        onCancel={() => setRemoving(undefined)}
      />
    </Screen>
  );
}

const styles = StyleSheet.create({
  body: { padding: spacing.md, gap: spacing.md },
  card: { gap: spacing.sm },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  flex: { flex: 1 },
  option: { flexDirection: 'row', alignItems: 'center', minHeight: 48, paddingHorizontal: spacing.md, borderRadius: radius.md, borderWidth: 1, borderColor: semantic.border, marginBottom: spacing.sm },
});
