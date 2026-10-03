// 1.5 选择文件：文件（Word、PDF、Excel）、相册、拍照、粘贴文字；可多选、显示上传进度、可移除；
// 必须勾选使用权才能开始；解析额度不足在开始前提示（PRD 1.5、11.12）。
import { ApiError } from '@training/api-client';
import { radius, semantic, spacing } from '@training/ui-tokens';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import * as DocumentPicker from 'expo-document-picker';
import * as ImagePicker from 'expo-image-picker';
import { router } from 'expo-router';
import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { Button, Card, EmptyState, Icon, QuotaSheet, Screen, Text, toast } from '@/components';
import { importKeys } from '@/features/import/api';
import { checkLimits, formatOf, limits, uploadAll, UnsupportedFile, type Picked } from '@/features/import/files';
import { useImportFlow } from '@/features/import/store';
import { Checkbox, FileRow, PageHeader } from '@/features/import/ui';
import { setStep } from '@/features/onboarding/api';
import { Footer } from '@/features/onboarding/ui';
import { usePermissionPrompt } from '@/features/permissions/PermissionPrompt';
import { api, unwrap } from '@/lib/api';

let seq = 0;
const nextKey = () => `f${Date.now()}-${seq++}`;

export default function FilesScreen() {
  const qc = useQueryClient();
  const { mode, subjectId, files, add, update, remove, onboarding } = useImportFlow();
  const subjects = useQuery({ queryKey: ['subjects'], queryFn: () => unwrap(api.GET('/subjects')) });
  const subject = subjects.data?.items.find((s) => s.id === subjectId);
  const [agreed, setAgreed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [quota, setQuota] = useState<{ need?: number; remaining?: number } | null>(null);

  const addPicked = (items: Omit<Picked, 'key' | 'status' | 'progress'>[]) => {
    const err = checkLimits(files, items);
    if (err) {
      toast(err);
      return;
    }
    add(items.map((i) => ({ ...i, key: nextKey(), status: 'pending', progress: 0 })));
  };

  const pickDocs = async () => {
    const res = await DocumentPicker.getDocumentAsync({
      multiple: true,
      copyToCacheDirectory: true,
      type: [
        'application/pdf',
        'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
        'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
        'application/msword',
        'application/vnd.ms-excel',
      ],
    });
    if (res.canceled) return;
    try {
      addPicked(res.assets.map((a) => ({ name: a.name, uri: a.uri, size: a.size ?? 0, mimeType: a.mimeType, format: formatOf(a.name, a.mimeType) })));
    } catch (e) {
      toast(e instanceof UnsupportedFile ? e.message : '选文件出了点问题，请重试');
    }
  };

  const fromAssets = (assets: ImagePicker.ImagePickerAsset[], prefix: string) =>
    assets.map((a, i) => ({
      name: a.fileName ?? `${prefix} ${files.filter((f) => f.format === 'image').length + i + 1}.jpg`,
      uri: a.uri,
      size: a.fileSize ?? 0,
      mimeType: a.mimeType ?? 'image/jpeg',
      format: 'image' as const,
    }));

  const pickAlbum = async () => {
    const res = await ImagePicker.launchImageLibraryAsync({ mediaTypes: ['images'], allowsMultipleSelection: true, selectionLimit: limits.maxImages, quality: 0.85 });
    if (!res.canceled) addPicked(fromAssets(res.assets, '图片'));
  };
  const album = usePermissionPrompt('photos', async () => {
    const r = await ImagePicker.requestMediaLibraryPermissionsAsync();
    return { granted: r.granted, canAskAgain: r.canAskAgain };
  });
  const camera = usePermissionPrompt(
    'camera',
    async () => {
      const r = await ImagePicker.requestCameraPermissionsAsync();
      return { granted: r.granted, canAskAgain: r.canAskAgain };
    },
    { label: '从相册选', onPress: () => void pickAlbum() },
  );

  const takePhoto = async () => {
    if (!(await camera.ensure())) return;
    const res = await ImagePicker.launchCameraAsync({ mediaTypes: ['images'], quality: 0.85 });
    if (!res.canceled) addPicked(fromAssets(res.assets, '拍照'));
  };

  const start = async () => {
    if (!subjectId) return;
    setBusy(true);
    try {
      const ids = await uploadAll(subjectId, mode, files, update);
      const job = await unwrap(api.POST('/import-jobs', { body: { subject_id: subjectId, mode, material_ids: ids } }));
      await qc.invalidateQueries({ queryKey: importKeys.jobs(true) });
      if (onboarding) void setStep('1.6');
      router.replace({ pathname: '/import/job/[id]', params: { id: String(job.id) } });
    } catch (e) {
      if (e instanceof ApiError && e.isQuotaExceeded) {
        setQuota({ need: e.detail?.need as number | undefined, remaining: e.detail?.remaining as number | undefined });
      } else {
        toast(e instanceof ApiError ? e.message : '上传没成功，请检查网络后重试');
      }
    } finally {
      setBusy(false);
    }
  };

  const sources = [
    { key: 'file', title: '文件', desc: 'Word · PDF · Excel', icon: 'import' as const, onPress: () => void pickDocs() },
    {
      key: 'album',
      title: '相册',
      desc: '截图、照片',
      icon: 'empty' as const,
      onPress: () =>
        void album.ensure().then(async (ok) => {
          if (ok) await pickAlbum();
        }),
    },
    { key: 'camera', title: '拍照', desc: '纸质习题册', icon: 'sparkle' as const, onPress: () => void takePhoto() },
    { key: 'paste', title: '粘贴文字', desc: '从网页、文档复制', icon: 'bank' as const, onPress: () => router.push('/import/paste') },
  ];

  if (!subjectId) {
    return (
      <Screen>
        <EmptyState title="请先选择导入方式" actionText="去选择" onAction={() => router.replace('/import')} />
      </Screen>
    );
  }

  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        <PageHeader
          title={mode === 'reference' ? '选择资料文件' : '选择题目文件'}
          desc={`导入到 ${subject ? `${subject.code ? `${subject.code} ` : ''}${subject.name}` : '专业课'} · 可以多选，之后还能追加`}
          onBack={() => router.back()}
        />
        <View style={styles.grid}>
          {sources.map((s) => (
            <Pressable key={s.key} accessibilityRole="button" accessibilityLabel={s.title} onPress={s.onPress} style={styles.source} disabled={busy}>
              <Icon name={s.icon} color={semantic.primary} />
              <Text variant="bodyStrong">{s.title}</Text>
              <Text variant="caption">{s.desc}</Text>
            </Pressable>
          ))}
        </View>

        {files.length > 0 ? (
          <Card style={styles.list}>
            <Text variant="bodyStrong">已选 {files.length} 项</Text>
            {files.map((f) => (
              <FileRow key={f.key} file={f} onRemove={busy ? undefined : () => remove(f.key)} />
            ))}
          </Card>
        ) : null}
        <Text variant="caption" style={styles.hint}>
          每次最多 {limits.maxFiles} 个文件，每个不超过 200 页、{limits.maxMB} MB；图片每次最多 {limits.maxImages} 张
        </Text>
        <Card style={styles.tip}>
          <Text variant="bodyStrong">答案在单独的文件里？</Text>
          <Text variant="caption">一起选上就行，AI 会按题号把答案配到题目上</Text>
        </Card>
      </ScrollView>
      <Footer>
        <Checkbox checked={agreed} onChange={setAgreed} label="我确认对这些资料有合法的使用权，仅用于本人学习" />
        <Button title="开始解析" disabled={!agreed || files.length === 0} loading={busy} onPress={() => void start()} />
      </Footer>

      {album.element}
      {camera.element}
      <QuotaSheet
        visible={!!quota}
        onClose={() => setQuota(null)}
        title="资料解析额度不够了"
        desc={
          quota?.need !== undefined && quota.remaining !== undefined
            ? `这次大约需要 ${quota.need} 页，还剩 ${quota.remaining} 页。开通会员每月 1000 页。`
            : '开通会员每月可解析 1000 页。'
        }
        onUpgrade={() => {
          setQuota(null);
          router.push('/member');
        }}
        freeOptions={[{ label: '少选几个文件', onPress: () => setQuota(null) }]}
      />
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing.xl },
  grid: { flexDirection: 'row', flexWrap: 'wrap', gap: spacing.sm },
  source: { width: '48%', flexGrow: 1, minHeight: 96, padding: spacing.md, gap: spacing.xs, borderRadius: radius.lg, backgroundColor: semantic.surface, borderWidth: 1, borderColor: semantic.border },
  list: { marginTop: spacing.lg, gap: spacing.xs },
  hint: { marginTop: spacing.sm },
  tip: { marginTop: spacing.lg, gap: spacing.xs, backgroundColor: semantic.infoSoft },
});
