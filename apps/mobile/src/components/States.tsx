import { radius, semantic, spacing, timing } from '@training/ui-tokens';
import { ApiError } from '@training/api-client';
import { useEffect, useState, type ReactNode } from 'react';
import { StyleSheet, View } from 'react-native';
import { useDelayed } from '../lib/useDelayed';
import { BottomSheet } from './BottomSheet';
import { Button } from './Button';
import { Icon, type IconName } from './Icon';
import { Text } from './Text';

/**
 * 每个页面都要处理的五种状态（PRD 14、dev-spec 第十一节）：
 * 加载中（超过 300 毫秒才出骨架屏）、空、网络错误、AI 生成中、额度不足。
 */

/** 骨架屏：若干灰色块，模拟卡片与文字行。 */
export function Skeleton({ rows = 3 }: { rows?: number }) {
  return (
    <View accessibilityLabel="加载中" style={styles.skeleton}>
      <View style={[styles.bone, styles.boneTitle]} />
      {Array.from({ length: rows }, (_, i) => (
        <View key={i} style={[styles.bone, { width: `${90 - i * 12}%` }]} />
      ))}
    </View>
  );
}

/** 加载中：超过 300 毫秒才显示骨架屏，避免闪烁。 */
export function Loading({ rows }: { rows?: number }) {
  const show = useDelayed(true);
  return show ? <Skeleton rows={rows} /> : null;
}

function StateBlock({ icon, title, desc, action }: { icon: IconName; title: string; desc?: string; action?: ReactNode }) {
  return (
    <View style={styles.block}>
      <Icon name={icon} size={40} color={semantic.textSecondary} />
      <Text variant="bodyStrong" style={styles.blockTitle}>
        {title}
      </Text>
      {desc ? (
        <Text variant="caption" style={styles.center}>
          {desc}
        </Text>
      ) : null}
      {action ? <View style={styles.action}>{action}</View> : null}
    </View>
  );
}

/** 空状态：图标 + 一句说明（为什么空、怎么有）+ 一个行动按钮。 */
export function EmptyState({ title, desc, actionText, onAction }: { title: string; desc?: string; actionText?: string; onAction?: () => void }) {
  return (
    <StateBlock
      icon="empty"
      title={title}
      desc={desc}
      action={actionText && onAction ? <Button title={actionText} onPress={onAction} /> : undefined}
    />
  );
}

/** 网络错误：图标 + 说明 + 重试。作答内容先存本地，恢复后自动提交（由调用方处理）。 */
export function ErrorState({ error, onRetry }: { error?: unknown; onRetry?: () => void }) {
  const network = !(error instanceof ApiError) || error.isNetwork;
  return (
    <StateBlock
      icon="offline"
      title={network ? '网络开小差了' : '出了点问题'}
      desc={network ? '检查网络后重试，已作答的内容不会丢' : error instanceof ApiError ? error.message : undefined}
      action={onRetry ? <Button title="重试" kind="secondary" onPress={onRetry} /> : undefined}
    />
  );
}

export interface AIGeneratingProps {
  /** 分步文案，如 ['提取要点', '比对 6 个采分点', '生成建议'] */
  steps: string[];
  /** 当前进行到第几步（0 起） */
  current: number;
  /** 预计时长，如「约 5 秒」 */
  eta?: string;
  /** 可离开的任务（解析、整卷批改、作文批改）显示「先离开，好了通知我」 */
  onLeave?: () => void;
}

/** AI 生成中：分步打勾 + 预计时长；超过 15 秒换安抚文案。 */
export function AIGenerating({ steps, current, eta, onLeave }: AIGeneratingProps) {
  const [slow, setSlow] = useState(false);
  useEffect(() => {
    const t = setTimeout(() => setSlow(true), timing.aiSlowMs);
    return () => clearTimeout(t);
  }, []);
  return (
    <View style={styles.ai} accessibilityLiveRegion="polite">
      <Icon name="sparkle" size={32} color={semantic.primary} />
      {steps.map((s, i) => (
        <View key={s} style={styles.step}>
          <View style={[styles.dot, i < current && styles.dotDone, i === current && styles.dotActive]}>
            {i < current ? <Icon name="check" size={12} color="#FFFFFF" /> : null}
          </View>
          <Text variant={i === current ? 'bodyStrong' : 'body'} color={i > current ? semantic.textSecondary : undefined}>
            {s}
          </Text>
        </View>
      ))}
      <Text variant="caption" style={styles.center}>
        {slow ? '内容比较多，还在认真处理，请再等一会儿' : eta ? `预计${eta}` : ''}
      </Text>
      {onLeave ? <Button title="先离开，好了通知我" kind="text" onPress={onLeave} /> : null}
    </View>
  );
}

/** AI 失败：「生成失败，未扣除次数」+ 重试（自动重试 1 次后才到这里）。 */
export function AIFailed({ onRetry }: { onRetry: () => void }) {
  return <StateBlock icon="sparkle" title="生成失败，未扣除次数" desc="可能是网络或服务繁忙，稍后再试" action={<Button title="重试" onPress={onRetry} />} />;
}

export interface QuotaSheetProps {
  visible: boolean;
  onClose: () => void;
  title: string;
  desc?: string;
  /** 永远给一条不付费也能继续的路：对照参考答案、明天再用 */
  freeOptions: { label: string; onPress: () => void }[];
  onUpgrade?: () => void;
}

/** 额度不足：底部弹层（如 4.9），给开通会员和免费出路。 */
export function QuotaSheet({ visible, onClose, title, desc, freeOptions, onUpgrade }: QuotaSheetProps) {
  return (
    <BottomSheet visible={visible} onClose={onClose} title={title}>
      {desc ? (
        <Text variant="body" color={semantic.textSecondary} style={styles.sheetDesc}>
          {desc}
        </Text>
      ) : null}
      <View style={styles.sheetActions}>
        {onUpgrade ? <Button title="开通会员" onPress={onUpgrade} block /> : null}
        {freeOptions.map((o) => (
          <Button key={o.label} title={o.label} kind="secondary" onPress={o.onPress} block />
        ))}
      </View>
    </BottomSheet>
  );
}

const styles = StyleSheet.create({
  skeleton: { gap: spacing.md, paddingVertical: spacing.lg },
  bone: { height: 14, borderRadius: radius.sm, backgroundColor: '#EEEAE1' },
  boneTitle: { height: 22, width: '50%' },
  block: { alignItems: 'center', paddingVertical: spacing.xxxl, paddingHorizontal: spacing.xl, gap: spacing.sm },
  blockTitle: { marginTop: spacing.sm },
  center: { textAlign: 'center' },
  action: { marginTop: spacing.md },
  ai: { alignItems: 'center', gap: spacing.md, paddingVertical: spacing.xxl },
  step: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, alignSelf: 'stretch', paddingHorizontal: spacing.xxl },
  dot: { width: 18, height: 18, borderRadius: 9, borderWidth: 1.5, borderColor: semantic.border, alignItems: 'center', justifyContent: 'center' },
  dotActive: { borderColor: semantic.primary },
  dotDone: { backgroundColor: semantic.primary, borderColor: semantic.primary },
  sheetDesc: { marginBottom: spacing.lg },
  sheetActions: { gap: spacing.md },
});
