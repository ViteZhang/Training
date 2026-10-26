// 0.4 用户协议与隐私政策：两个 Tab；显示版本号、更新日期、生效日期；正文由后台按版本配置。
// 从 6.5 会员中心进入时（kind=membership）只显示会员服务协议。
// 正文支持标题、段落、列表、表格和加粗（见 features/auth/agreementBody）。
import type { Schemas } from '@training/api-client';
import { colors, radius, semantic, spacing } from '@training/ui-tokens';
import { useQuery } from '@tanstack/react-query';
import { useLocalSearchParams } from 'expo-router';
import { useMemo, useState } from 'react';
import { ScrollView, StyleSheet, View } from 'react-native';
import { ErrorState, Loading, NavBar, Screen, Segmented, Text } from '@/components';
import { parseAgreementBody, type Block, type Span } from '@/features/auth/agreementBody';
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

function Spans({ spans }: { spans: Span[] }) {
  return (
    <>
      {spans.map((s, i) =>
        s.bold ? (
          <Text key={i} variant="caption" style={styles.bold}>
            {s.text}
          </Text>
        ) : (
          s.text
        ),
      )}
    </>
  );
}

function BodyBlock({ block }: { block: Block }) {
  switch (block.type) {
    case 'heading':
      return (
        <Text variant="bodyStrong" style={styles.heading}>
          {block.spans.map((s) => s.text).join('')}
        </Text>
      );
    case 'paragraph':
      return (
        <Text variant="caption" style={styles.para}>
          <Spans spans={block.spans} />
        </Text>
      );
    case 'list':
      return (
        <View style={styles.list}>
          {block.items.map((item, i) => (
            <View key={i} style={styles.listItem}>
              <Text variant="caption" style={styles.marker}>
                {item.marker}
              </Text>
              <Text variant="caption" style={styles.listText}>
                <Spans spans={item.spans} />
              </Text>
            </View>
          ))}
        </View>
      );
    case 'table':
      return (
        <View style={styles.table}>
          {block.header.length ? (
            <View style={[styles.row, styles.headRow]}>
              {block.header.map((cell, i) => (
                <Text key={i} variant="small" style={[styles.cell, styles.headCell]}>
                  {cell.map((s) => s.text).join('')}
                </Text>
              ))}
            </View>
          ) : null}
          {block.rows.map((row, r) => (
            <View key={r} style={[styles.row, r > 0 || block.header.length ? styles.rowLine : null]}>
              {row.map((cell, i) => (
                <Text key={i} variant="small" style={styles.cell}>
                  <Spans spans={cell} />
                </Text>
              ))}
            </View>
          ))}
        </View>
      );
  }
}

export default function AgreementPage() {
  const params = useLocalSearchParams<{ kind?: Kind }>();
  const tabs = params.kind === 'membership' ? membershipTabs : defaultTabs;
  const [kind, setKind] = useState<Kind>(params.kind === 'privacy' || params.kind === 'membership' ? params.kind : 'user');
  const q = useQuery({
    queryKey: ['agreement', kind],
    queryFn: () => unwrap(api.GET('/agreements/{kind}', { params: { path: { kind } } })),
  });
  const blocks = useMemo(() => (q.data ? parseAgreementBody(q.data.body) : []), [q.data]);
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
          {blocks.map((b, i) => (
            <BodyBlock key={i} block={b} />
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
  para: { marginTop: 6, lineHeight: 23, color: colors.inkSoft },
  bold: { fontWeight: '700', color: semantic.textPrimary },
  list: { marginTop: 6, gap: 4 },
  listItem: { flexDirection: 'row', gap: 6 },
  marker: { lineHeight: 23, color: colors.inkSoft, minWidth: 16 },
  listText: { flex: 1, lineHeight: 23, color: colors.inkSoft },
  table: { marginTop: spacing.sm, borderWidth: 1, borderColor: semantic.border, borderRadius: radius.sm, overflow: 'hidden' },
  row: { flexDirection: 'row' },
  headRow: { backgroundColor: semantic.fill },
  rowLine: { borderTopWidth: 1, borderTopColor: semantic.border },
  cell: { flex: 1, paddingHorizontal: 6, paddingVertical: 6, lineHeight: 18, color: colors.inkSoft },
  headCell: { fontWeight: '700', color: semantic.textPrimary },
});
