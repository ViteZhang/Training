// 2.3 消息中心（T27）：题库整理完成、需核对、复习到期、批改完成、协议更新、公告、客服回复、客服查看了你授权的资料；
// 按「今天 / 更早」分组；点击跳转并标已读；「全部已读」；消息保留 30 天。
import type { Schemas } from '@training/api-client';
import { radius, semantic, spacing } from '@training/ui-tokens';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { router } from 'expo-router';
import { FlatList, Pressable, StyleSheet, View } from 'react-native';
import { Button, EmptyState, ErrorState, Icon, Loading, Screen, Text } from '@/components';
import type { IconName } from '@/components/Icon';
import { entryTones } from '@/features/today/Cards';
import { PageHeader } from '@/features/import/ui';
import { isToday, messageHref, messageKeys, messageTime, useMessages } from '@/features/messages/api';
import { api } from '@/lib/api';

const typeIcon: Record<Schemas['MessageType'], { icon: IconName; tone: { bg: string; fg: string } }> = {
  import_done: { icon: 'paper', tone: entryTones.paper },
  review_needed: { icon: 'edit', tone: entryTones.amber },
  review_due: { icon: 'bell', tone: entryTones.info },
  grading_done: { icon: 'pen', tone: entryTones.wrong },
  paper_graded: { icon: 'pen', tone: entryTones.wrong },
  essay_graded: { icon: 'pen', tone: entryTones.wrong },
  export_ready: { icon: 'paper', tone: entryTones.paper },
  agreement_update: { icon: 'lock', tone: entryTones.neutral },
  announcement: { icon: 'bell', tone: entryTones.neutral },
  support_reply: { icon: 'sparkle', tone: entryTones.recite },
  content_accessed: { icon: 'lock', tone: entryTones.neutral },
  membership: { icon: 'sparkle', tone: entryTones.amber },
  stage_change: { icon: 'today', tone: entryTones.recite },
  official_bank: { icon: 'bank', tone: entryTones.info },
};

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
        right={unread > 0 ? <Button title="全部已读" kind="text" color={semantic.textSecondary} loading={readAll.isPending} onPress={() => readAll.mutate()} /> : undefined}
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
                <View style={[styles.icon, { backgroundColor: (typeIcon[r.m.type] ?? typeIcon.announcement).tone.bg }]}>
                  <Icon name={(typeIcon[r.m.type] ?? typeIcon.announcement).icon} size={20} color={(typeIcon[r.m.type] ?? typeIcon.announcement).tone.fg} />
                </View>
                <View style={[styles.flex, styles.gap4]}>
                  <View style={styles.row}>
                    <View style={[styles.row, styles.flex]}>
                      <Text variant="bodyStrong" style={styles.title} numberOfLines={1}>
                        {r.m.title}
                      </Text>
                      {r.m.read ? null : <View style={styles.dot} />}
                    </View>
                    <Text variant="small">{messageTime(r.m.created_at)}</Text>
                  </View>
                  <Text variant="caption" style={styles.lh}>
                    {r.m.body}
                  </Text>
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
  gap4: { gap: 4 },
  lh: { lineHeight: 20 },
  row: { flexDirection: 'row', alignItems: 'center', gap: 6 },
  title: { fontWeight: '700', flexShrink: 1 },
  header: { marginTop: spacing.md, marginBottom: 10, marginLeft: 4 },
  item: { flexDirection: 'row', gap: 12, padding: 14, marginBottom: 10, borderRadius: radius.xl, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface, minHeight: 44 },
  icon: { width: 38, height: 38, borderRadius: 12, alignItems: 'center', justifyContent: 'center' },
  dot: { width: 7, height: 7, borderRadius: 4, backgroundColor: semantic.danger },
  footer: { textAlign: 'center', marginVertical: spacing.lg },
});
