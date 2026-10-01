// 0.4 用户协议与隐私政策：两个 Tab；显示版本号、更新日期、生效日期；正文由后台按版本配置。
import type { Schemas } from '@training/api-client';
import { radius, semantic, spacing } from '@training/ui-tokens';
import { useQuery } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { Button, ErrorState, Loading, Screen, Text } from '@/components';
import { api, unwrap } from '@/lib/api';

type Kind = Schemas['AgreementKind'];
const tabs: { kind: Kind; label: string }[] = [
  { kind: 'user', label: '用户协议' },
  { kind: 'privacy', label: '隐私政策' },
];

function day(iso?: string) {
  return iso ? iso.slice(0, 10) : '';
}

export default function AgreementPage() {
  const params = useLocalSearchParams<{ kind?: Kind }>();
  const [kind, setKind] = useState<Kind>(params.kind === 'privacy' ? 'privacy' : 'user');
  const q = useQuery({
    queryKey: ['agreement', kind],
    queryFn: () => unwrap(api.GET('/agreements/{kind}', { params: { path: { kind } } })),
  });
  return (
    <Screen>
      <Button title="返回" kind="text" onPress={() => router.back()} style={styles.back} />
      <Text variant="h2">协议与政策</Text>
      <View style={styles.tabs}>
        {tabs.map((t) => (
          <Pressable
            key={t.kind}
            accessibilityRole="tab"
            accessibilityState={{ selected: t.kind === kind }}
            onPress={() => setKind(t.kind)}
            style={[styles.tab, t.kind === kind && styles.tabOn]}
          >
            <Text variant="bodyStrong" color={t.kind === kind ? '#FFFFFF' : semantic.textSecondary}>
              {t.label}
            </Text>
          </Pressable>
        ))}
      </View>
      {q.isLoading ? <Loading rows={8} /> : null}
      {q.isError ? <ErrorState error={q.error} onRetry={() => void q.refetch()} /> : null}
      {q.data ? (
        <ScrollView contentContainerStyle={styles.body}>
          <Text variant="caption">
            版本 {q.data.version} · 更新日期 {day(q.data.updated_at)} · 生效日期 {day(q.data.effective_at)}
          </Text>
          {q.data.body.split(/\n{2,}/).map((p, i) => (
            <Text key={i} variant={p.startsWith('#') ? 'h3' : 'body'} style={styles.para}>
              {p.replace(/^#+\s*/, '')}
            </Text>
          ))}
        </ScrollView>
      ) : null}
    </Screen>
  );
}

const styles = StyleSheet.create({
  back: { alignSelf: 'flex-start', marginTop: spacing.sm, paddingHorizontal: 0 },
  tabs: { flexDirection: 'row', gap: spacing.sm, marginVertical: spacing.lg },
  tab: { paddingHorizontal: spacing.lg, minHeight: 36, justifyContent: 'center', borderRadius: radius.pill, backgroundColor: '#F2EFE8' },
  tabOn: { backgroundColor: semantic.primary },
  body: { paddingBottom: spacing.xxxl },
  para: { marginTop: spacing.md },
});
