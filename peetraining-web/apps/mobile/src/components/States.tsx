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
  /** 标题，如「AI 正在批改」；不传只显示步骤 */
  title?: string;
  /** 标题下的一行说明（会和预计时长拼在一起） */
  desc?: string;
  /** 可离开的任务（解析、整卷批改、作文批改）显示「先离开，好了通知我」 */
  onLeave?: () => void;
}

/** AI 生成中（设计稿 4.6）：三个圆点 + 标题 + 分步打勾；超过 15 秒换安抚文案。 */
export function AIGenerating({ steps, current, eta, title, desc, onLeave }: AIGeneratingProps) {
  const [slow, setSlow] = useState(false);
  useEffect(() => {
    const t = setTimeout(() => setSlow(true), timing.aiSlowMs);
    return () => clearTimeout(t);
  }, []);
  const sub = slow ? '内容比较多，还在认真处理，请再等一会儿' : [desc, eta ? `${eta.startsWith('约') || eta.startsWith('大约') || eta.startsWith('每') ? '' : '预计'}${eta}` : ''].filter(Boolean).join(' · ');
  return (
    <View style={styles.ai} accessibilityLiveRegion="polite">
      <View style={styles.dots} accessibilityElementsHidden>
        <View style={[styles.aiDot, { backgroundColor: semantic.textPrimary }]} />
        <View style={[styles.aiDot, { backgroundColor: '#8C877D' }]} />
        <View style={[styles.aiDot, { backgroundColor: '#CFCAC0' }]} />
      </View>
      {title ? <Text variant="h2">{title}</Text> : null}
      {sub ? <Text variant="caption">{sub}</Text> : null}
      <View style={styles.steps}>
        {steps.map((st, i) => (
          <View key={st} style={styles.step}>
            <View style={[styles.dot, i < current && styles.dotDone, i === current && styles.dotActive]}>
              {i < current ? <Icon name="check" size={12} color="#FFFFFF" /> : null}
            </View>
            <Text variant="body" color={i > current ? semantic.textSecondary : undefined}>
              {st}
            </Text>
          </View>
        ))}
      </View>
      {onLeave ? <Button title="先离开，好了通知我" kind="text" style={styles.leave} onPress={onLeave} /> : null}
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

/** 额度不足：底部弹层（设计稿 4.9）：锁形图标、标题、说明，「开通会员」+ 不付费也能继续的出路。 */
export function QuotaSheet({ visible, onClose, title, desc, freeOptions, onUpgrade }: QuotaSheetProps) {
  return (
    <BottomSheet visible={visible} onClose={onClose}>
      <View style={styles.lock}>
        <Icon name="lock" size={22} color={semantic.danger} />
      </View>
      <Text variant="h2" style={styles.sheetTitle}>
        {title}
      </Text>
      {desc ? (
        <Text variant="caption" style={styles.sheetDesc}>
          {desc}
        </Text>
      ) : null}
      <View style={styles.sheetActions}>
        {onUpgrade ? <Button title="开通会员" onPress={onUpgrade} block /> : null}
        <View style={styles.freeRow}>
          {freeOptions.map((o) => (
            <Button key={o.label} title={o.label} kind={onUpgrade ? 'text' : 'secondary'} color={onUpgrade ? semantic.textPrimary : undefined} onPress={o.onPress} style={styles.free} />
          ))}
        </View>
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
  ai: { gap: 8, paddingVertical: spacing.xxl, paddingHorizontal: 18 },
  dots: { flexDirection: 'row', gap: 6, marginBottom: 8 },
  aiDot: { width: 8, height: 8, borderRadius: 4 },
  steps: { gap: 14, marginTop: spacing.lg },
  step: { flexDirection: 'row', alignItems: 'center', gap: 12 },
  dot: { width: 24, height: 24, borderRadius: 12, borderWidth: 1.5, borderStyle: 'dashed', borderColor: '#BDB8AD', alignItems: 'center', justifyContent: 'center' },
  dotActive: { borderColor: semantic.textPrimary },
  dotDone: { backgroundColor: semantic.textPrimary, borderColor: semantic.textPrimary, borderStyle: 'solid' },
  leave: { alignSelf: 'flex-start', paddingHorizontal: 0, marginTop: spacing.md },
  lock: { width: 52, height: 52, borderRadius: 16, alignItems: 'center', justifyContent: 'center', backgroundColor: semantic.dangerSoft, marginTop: 4, marginBottom: 14 },
  sheetTitle: { marginBottom: 6 },
  sheetDesc: { marginBottom: spacing.lg, lineHeight: 21 },
  sheetActions: { gap: 6 },
  freeRow: { flexDirection: 'row', gap: 8 },
  free: { flex: 1 },
});
