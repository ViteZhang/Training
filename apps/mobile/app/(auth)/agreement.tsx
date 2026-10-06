// 0.4 用户协议与隐私政策：两个 Tab；显示版本号、更新日期、生效日期；正文由后台按版本配置。
// 从 6.5 会员中心进入时（kind=membership）只显示会员服务协议。
import type { Schemas } from '@training/api-client';
import { spacing } from '@training/ui-tokens';
import { useQuery } from '@tanstack/react-query';
import { useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { ScrollView, StyleSheet, View } from 'react-native';
import { ErrorState, Loading, NavBar, Screen, Segmented, Text } from '@/components';
import { api, unwrap } from '@/lib/api';

type Kind = Schemas['AgreementKind'];
const defaultTabs: { kind: Kind; label: string }[] = [
  { kind: 'user', label: '用户协议' },
  { kind: 'privacy', label: '隐私政策' },
];
const membershipTabs: { kind: Kind; label: string }[] = [{ kind: 'membership', label: '会员服务协议' }];

function day(iso?: string) {
  return iso ? iso.slice(0, 10) : '';
}

export default function AgreementPage() {
  const params = useLocalSearchParams<{ kind?: Kind }>();
  const tabs = params.kind === 'membership' ? membershipTabs : defaultTabs;
  const [kind, setKind] = useState<Kind>(params.kind === 'privacy' || params.kind === 'membership' ? params.kind : 'user');
  const q = useQuery({
    queryKey: ['agreement', kind],
    queryFn: () => unwrap(api.GET('/agreements/{kind}', { params: { path: { kind } } })),
  });
  return (
    <Screen>
      <NavBar title="协议与政策" />
      <View style={styles.tabs}>
        <Segmented options={tabs.map((t) => ({ key: t.kind, label: t.label }))} value={kind} onChange={setKind} />
      </View>
      {q.isLoading ? <Loading rows={8} /> : null}
      {q.isError ? <ErrorState error={q.error} onRetry={() => void q.refetch()} /> : null}
      {q.data ? (
        <ScrollView contentContainerStyle={styles.body}>
          <Text variant="small">
            版本 {q.data.version} · 更新日期 {day(q.data.updated_at)} · 生效日期 {day(q.data.effective_at)}
          </Text>
          {q.data.body.split(/\n{2,}/).map((p, i) => (
            <Text key={i} variant={p.startsWith('#') ? 'bodyStrong' : 'caption'} style={p.startsWith('#') ? styles.heading : styles.para}>
              {p.replace(/^#+\s*/, '')}
            </Text>
          ))}
        </ScrollView>
      ) : null}
    </Screen>
  );
}

const styles = StyleSheet.create({
  tabs: { marginTop: spacing.sm, marginBottom: spacing.md },
  body: { paddingBottom: spacing.xxxl },
  heading: { marginTop: 18, fontWeight: '700' },
  para: { marginTop: 6, lineHeight: 23, color: '#4A4740' },
});
