// 2.3 消息中心（T27）：题库整理完成、需核对、复习到期、批改完成、协议更新、公告、客服回复、客服查看了你授权的资料；
// 按「今天 / 更早」分组；点击跳转并标已读；「全部已读」；消息保留 30 天。
import type { Schemas } from '@training/api-client';
import { radius, semantic, spacing } from '@training/ui-tokens';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { router } from 'expo-router';
import { FlatList, Pressable, StyleSheet, View } from 'react-native';
import { Button, EmptyState, ErrorState, Loading, Screen, Text } from '@/components';
import { PageHeader } from '@/features/import/ui';
import { isToday, messageHref, messageKeys, messageTime, useMessages } from '@/features/messages/api';
import { api } from '@/lib/api';

type Row = { kind: 'header'; title: string } | { kind: 'item'; m: Schemas['Message'] };

export default function MessagesPage() {
  const qc = useQueryClient();
  const q = useMessages();
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: messageKeys.list });
    void qc.invalidateQueries({ queryKey: messageKeys.unread });
  };
  const readAll = useMutation({ mutationFn: () => api.POST('/messages/read-all'), onSuccess: refresh });
  const open = (m: Schemas['Message']) => {
    if (!m.read) void api.POST('/messages/{messageId}/read', { params: { path: { messageId: m.id } } }).then(refresh);
    const href = messageHref(m);
    if (href) router.push(href);
  };

  if (q.isPending) return <Screen><Loading rows={6} /></Screen>;
  if (q.isError || !q.data) return <Screen><ErrorState error={q.error} onRetry={() => void q.refetch()} /></Screen>;
  const items = q.data.pages.flatMap((p) => p.items);
  const unread = q.data.pages[0]?.unread ?? 0;
  const rows: Row[] = [];
  const today = items.filter((m) => isToday(m.created_at));
  const earlier = items.filter((m) => !isToday(m.created_at));
  if (today.length) rows.push({ kind: 'header', title: '今天' }, ...today.map((m) => ({ kind: 'item' as const, m })));
  if (earlier.length) rows.push({ kind: 'header', title: '更早' }, ...earlier.map((m) => ({ kind: 'item' as const, m })));

  return (
    <Screen>
      <PageHeader
        title="消息"
        onBack={() => router.back()}
        right={unread > 0 ? <Button title="全部已读" kind="text" loading={readAll.isPending} onPress={() => readAll.mutate()} /> : undefined}
      />
      {items.length === 0 ? (
        <EmptyState title="还没有消息" desc="题库整理好、批改完成、复习到期时会在这里提醒你" />
      ) : (
        <FlatList
          data={rows}
          keyExtractor={(r, i) => (r.kind === 'item' ? `m${r.m.id}` : `h${i}`)}
          onEndReached={() => q.hasNextPage && void q.fetchNextPage()}
          refreshing={q.isRefetching}
          onRefresh={() => void q.refetch()}
          renderItem={({ item: r }) =>
            r.kind === 'header' ? (
              <Text variant="caption" style={styles.header}>{r.title}</Text>
            ) : (
              <Pressable accessibilityRole="button" accessibilityLabel={`${r.m.read ? '' : '未读 '}${r.m.title}`} onPress={() => open(r.m)} style={styles.item}>
                <View style={[styles.dot, r.m.read && styles.dotRead]} />
                <View style={styles.flex}>
                  <View style={styles.row}>
                    <Text variant="bodyStrong" style={styles.flex}>{r.m.title}</Text>
                    <Text variant="caption">{messageTime(r.m.created_at)}</Text>
                  </View>
                  <Text variant="caption">{r.m.body}</Text>
                </View>
              </Pressable>
            )
          }
          ListFooterComponent={<Text variant="caption" style={styles.footer}>消息保留 30 天</Text>}
        />
      )}
    </Screen>
  );
}

const styles = StyleSheet.create({
  flex: { flex: 1 },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  header: { marginTop: spacing.md, marginBottom: spacing.xs },
  item: { flexDirection: 'row', gap: spacing.sm, padding: spacing.md, marginBottom: spacing.sm, borderRadius: radius.md, backgroundColor: semantic.surface, minHeight: 44 },
  dot: { width: 8, height: 8, borderRadius: 4, marginTop: 8, backgroundColor: semantic.danger },
  dotRead: { backgroundColor: 'transparent' },
  footer: { textAlign: 'center', marginVertical: spacing.lg },
});
