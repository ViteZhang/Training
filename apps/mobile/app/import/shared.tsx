// 1.5 外部文件待导入：先保存，登录和选择专业课后再交给原导入流程。
import { useQuery } from '@tanstack/react-query';
import { semantic, spacing } from '@training/ui-tokens';
import { router } from 'expo-router';
import { useState } from 'react';
import { ScrollView, StyleSheet } from 'react-native';
import { Button, Card, EmptyState, ErrorState, Loading, Screen, Text, toast } from '@/components';
import { checkLimits } from '@/features/import/files';
import { FileRow, PageHeader } from '@/features/import/ui';
import { removeSharedFiles, useSharedInbox } from '@/features/import/sharedInbox';
import { useImportFlow } from '@/features/import/store';
import { WeChatImportGuide } from '@/features/import/WeChatImportGuide';
import { Chips, Footer } from '@/features/onboarding/ui';
import { api, unwrap } from '@/lib/api';
import { useSession } from '@/lib/session';

export default function SharedImport() {
  const files = useSharedInbox();
  const session = useSession((state) => state.session);
  const subjects = useQuery({ queryKey: ['subjects'], enabled: !!session, queryFn: () => unwrap(api.GET('/subjects')) });
  const [subject, setSubject] = useState<number>();
  const [mode, setMode] = useState<'question' | 'reference' | 'answers'>('question');
  const selected = subjects.data?.items.find((item) => item.id === subject)?.id ?? subjects.data?.items[0]?.id;
  const start = () => {
    if (!selected || !files.length) return;
    if (mode === 'answers') {
      router.push({ pathname: '/import/answers', params: { subject: String(selected), shared: '1' } });
      return;
    }
    const flow = useImportFlow.getState();
    if (flow.files.length && flow.subjectId !== selected) { toast('还有一批资料尚未处理，请先完成原导入或移除已选文件'); return; }
    if (!flow.files.length) flow.start(mode, selected, false);
    else if (flow.mode !== mode) { toast('待导入文件与当前导入类型不同，请先完成原导入'); return; }
    const existing = new Set(useImportFlow.getState().files.map((file) => file.sharedKey));
    const adding = files.filter((file) => !existing.has(file.key));
    const error = checkLimits(useImportFlow.getState().files, adding);
    if (error) { toast(error); return; }
    flow.add(adding);
    router.push('/import/files');
  };
  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.content}>
        <PageHeader title="收到的文件" desc="文件已保存在本机，选择专业课和用途后再上传。" onBack={() => router.back()} />
        {!files.length ? <EmptyState title="暂无待导入文件" desc="可以从微信分享文件，或先保存到手机再导入。" /> : (
          <Card>{files.map((file) => <FileRow key={file.key} file={file} onRemove={() => removeSharedFiles([file.key])} />)}</Card>
        )}
        <WeChatImportGuide pickFile={() => router.push('/import')} />
        {!session ? <>
          <Text variant="body">请先登录，文件会保留，登录后可继续导入。</Text>
          <Button title="登录后继续导入" onPress={() => router.push('/(auth)/login')} />
        </> : subjects.isLoading ? <Loading rows={3} /> : subjects.isError ? <ErrorState error={subjects.error} onRetry={() => void subjects.refetch()} /> : !subjects.data?.items.length ? <>
          <Text variant="body">请先添加一门专业课，收到的文件会继续保留。</Text>
          <Button title="添加专业课" onPress={() => router.push('/(onboarding)/subject')} />
        </> : <>
          <Text variant="bodyStrong">放进哪门专业课</Text>
          <Chips options={subjects.data.items.map((item) => item.id)} value={selected!} onChange={setSubject} format={(id) => subjects.data.items.find((item) => item.id === id)?.name ?? ''} />
          <Text variant="bodyStrong">这份文件用于</Text>
          <Chips options={['question', 'reference', 'answers'] as const} value={mode} onChange={setMode} format={(value) => ({ question: '题目', reference: '资料', answers: '补充答案' })[value]} />
          <Text variant="caption">继续后可以核对文件、移除不需要的项，再确认开始解析。</Text>
        </>}
      </ScrollView>
      {session && selected ? <Footer><Button title="继续导入" disabled={!files.length} onPress={start} /></Footer> : null}
    </Screen>
  );
}
const styles = StyleSheet.create({ content: { gap: spacing.md, paddingBottom: spacing.xl, backgroundColor: semantic.background } });
