// 0.5 权限说明 / 0.5b 权限被拒：相机、相册、麦克风、通知只在首次使用对应功能时弹出，先说明用途再调系统弹窗；
// 被拒时给替代路径（如相机被拒 → 从相册导入）和「去设置」。
import { semantic } from '@training/ui-tokens';
import { useCallback, useState } from 'react';
import { Linking, StyleSheet, View } from 'react-native';
import { BottomSheet, Button, Icon, Text } from '@/components';
import type { IconName } from '@/components/Icon';
import { storage } from '@/lib/storage';

export type PermissionKind = 'camera' | 'photos' | 'microphone' | 'notifications';

const copy: Record<PermissionKind, { title: string; desc: string; deniedTitle: string; deniedDesc: string }> = {
  camera: {
    title: '允许使用相机',
    desc: '用于拍照导入题目和讲义，以及拍摄手写答案。照片只用于识别文字，仅你本人可见。',
    deniedTitle: '相机权限未开启',
    deniedDesc: '拍照导入需要相机权限，请在系统设置中打开。也可以改从相册或文件导入。',
  },
  photos: {
    title: '允许访问相册',
    desc: '用于从相册导入题目、讲义的照片和截图。只读取你选中的图片，仅你本人可见。',
    deniedTitle: '相册权限未开启',
    deniedDesc: '从相册导入需要相册权限，请在系统设置中打开。也可以拍照或从文件导入。',
  },
  microphone: {
    title: '允许使用麦克风',
    desc: '用于语音作答和口述背诵。录音只用于转成文字，仅你本人可见。',
    deniedTitle: '麦克风权限未开启',
    deniedDesc: '语音作答需要麦克风权限，请在系统设置中打开。也可以打字或拍手写稿作答。',
  },
  notifications: {
    title: '允许发送通知',
    desc: '用于每日训练提醒、复习到期提醒，以及资料解析和批改完成的通知。',
    deniedTitle: '通知权限未开启',
    deniedDesc: '不开通知也能正常使用，完成的结果可以在消息中心查看。需要提醒时请在系统设置中打开。',
  },
};

export interface PermissionResult {
  granted: boolean;
  /** false 表示系统不会再弹窗，只能去设置 */
  canAskAgain: boolean;
}

const kindIcon: Record<PermissionKind, IconName> = { camera: 'camera', photos: 'image', microphone: 'mic', notifications: 'bell' };

/** 弹窗顶部的图标块：说明时为浅蓝，被拒时为浅红。 */
function IconBadge({ kind, denied }: { kind: PermissionKind; denied?: boolean }) {
  return (
    <View style={[styles.badge, { backgroundColor: denied ? semantic.dangerSoft : semantic.infoSoft }]}>
      <Icon name={kindIcon[kind]} size={22} color={denied ? semantic.danger : semantic.info} />
    </View>
  );
}

type Stage = 'idle' | 'explain' | 'denied';

/**
 * 用法：
 *   const camera = usePermissionPrompt('camera', requestCameraPermission, { label: '从相册导入', onPress: pickFromAlbum });
 *   if (await camera.ensure()) openCamera();
 *   ...
 *   {camera.element}
 */
export function usePermissionPrompt(
  kind: PermissionKind,
  request: () => Promise<PermissionResult>,
  alternative?: { label: string; onPress: () => void },
) {
  const [stage, setStage] = useState<Stage>('idle');
  const [resolver, setResolver] = useState<((ok: boolean) => void) | null>(null);
  const explainedKey = `permission_explained_${kind}`;

  const finish = useCallback(
    (ok: boolean) => {
      setStage('idle');
      resolver?.(ok);
      setResolver(null);
    },
    [resolver],
  );

  const askSystem = useCallback(async () => {
    storage.set(explainedKey, true);
    const res = await request();
    if (res.granted) return true;
    setStage('denied');
    return false;
  }, [explainedKey, request]);

  /** 有权限返回 true；第一次先弹说明，用户点「去允许」后再调系统弹窗。 */
  const ensure = useCallback(async (): Promise<boolean> => {
    if (storage.getBoolean(explainedKey)) return askSystem();
    return new Promise<boolean>((resolve) => {
      setResolver(() => resolve);
      setStage('explain');
    });
  }, [askSystem, explainedKey]);

  const c = copy[kind];
  const element =
    stage === 'explain' ? (
      <BottomSheet visible centered icon={<IconBadge kind={kind} />} onClose={() => finish(false)} title={c.title}>
        <Text variant="caption" style={styles.desc}>
          {c.desc}
        </Text>
        <View style={styles.actions}>
          <Button title="暂不" kind="secondary" onPress={() => finish(false)} style={styles.flex} />
          <Button
            title="去允许"
            onPress={() => {
              const r = resolver;
              setResolver(null);
              setStage('idle');
              void askSystem().then((ok) => r?.(ok));
            }}
            style={styles.flex}
          />
        </View>
      </BottomSheet>
    ) : stage === 'denied' ? (
      <BottomSheet visible centered icon={<IconBadge kind={kind} denied />} onClose={() => setStage('idle')} title={c.deniedTitle}>
        <Text variant="caption" style={styles.desc}>
          {c.deniedDesc}
        </Text>
        <View style={styles.actions}>
          {alternative ? (
            <Button
              title={alternative.label}
              kind="secondary"
              style={styles.flex}
              onPress={() => {
                setStage('idle');
                alternative.onPress();
              }}
            />
          ) : null}
          <Button title="去设置" style={styles.flex} onPress={() => void Linking.openSettings()} />
        </View>
        <Button title="取消" kind="text" onPress={() => setStage('idle')} />
      </BottomSheet>
    ) : null;

  return { ensure, element };
}

const styles = StyleSheet.create({
  actions: { flexDirection: 'row', gap: 10, marginTop: 20 },
  flex: { flex: 1, minHeight: 46 },
  desc: { textAlign: 'center', lineHeight: 21 },
  badge: { width: 56, height: 56, borderRadius: 18, alignItems: 'center', justifyContent: 'center' },
});
