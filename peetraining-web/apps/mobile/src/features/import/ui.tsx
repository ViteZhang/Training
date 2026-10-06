import { layout, radius, semantic, spacing } from '@training/ui-tokens';
import type { ReactNode } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { Icon, NavBar, ProgressBar, Tag, Text } from '@/components';
import type { Picked } from './files';

/**
 * 二级页顶部。默认是设计稿的顶部栏：返回 + 居中标题 + 右侧操作；
 * large（有 desc 时默认）用于导入等流程页：返回一行，下面 26 号大标题和说明。
 */
export function PageHeader({ title, desc, onBack, right, large }: { title: string; desc?: string; onBack?: () => void; right?: ReactNode; large?: boolean }) {
  if (!(large ?? !!desc)) return <NavBar title={title} onBack={onBack} back={!!onBack} right={right} />;
  return (
    <View style={styles.header}>
      <NavBar onBack={onBack} back={!!onBack} right={right} />
      <Text variant="h1" style={styles.title}>
        {title}
      </Text>
      {desc ? (
        <Text variant="caption" style={styles.desc}>
          {desc}
        </Text>
      ) : null}
    </View>
  );
}

/** 勾选框（使用权确认等）。 */
export function Checkbox({ checked, onChange, label }: { checked: boolean; onChange: (v: boolean) => void; label: string }) {
  return (
    <Pressable accessibilityRole="checkbox" accessibilityState={{ checked }} accessibilityLabel={label} onPress={() => onChange(!checked)} style={styles.check}>
      <View style={[styles.box, checked && styles.boxOn]}>{checked ? <Icon name="check" size={12} color={semantic.textOnBrand} /> : null}</View>
      <Text variant="caption" style={styles.flex} maxFontSizeMultiplier={layout.maxFontScale}>
        {label}
      </Text>
    </Pressable>
  );
}

const badgeTone: Record<Picked['format'], { bg: string; fg: string }> = {
  pdf: { bg: semantic.dangerSoft, fg: semantic.danger },
  docx: { bg: semantic.infoSoft, fg: '#1D4C77' },
  xlsx: { bg: semantic.masteredSoft, fg: '#1F6B4A' },
  image: { bg: semantic.fill, fg: semantic.textPrimary },
  text: { bg: semantic.fill, fg: semantic.textPrimary },
};

const formatLabel: Record<Picked['format'], string> = { pdf: 'PDF', docx: 'DOC', xlsx: 'XLS', image: 'IMG', text: 'TXT' };

/** 1.5 已选文件的一行：格式、名称、上传进度或状态、移除。 */
export function FileRow({ file, onRemove }: { file: Picked; onRemove?: () => void }) {
  const mb = file.size / 1024 / 1024;
  return (
    <View style={styles.file} accessibilityLabel={file.name}>
      <View style={[styles.badge, { backgroundColor: badgeTone[file.format].bg }]}>
        <Text variant="small" color={badgeTone[file.format].fg} style={styles.badgeText}>
          {formatLabel[file.format]}
        </Text>
      </View>
      <View style={styles.flex}>
        <Text variant="body" numberOfLines={1}>
          {file.name}
        </Text>
        {file.status === 'uploading' || file.status === 'hashing' ? (
          <View style={styles.progress}>
            <ProgressBar value={file.progress} />
            <Text variant="caption">{file.status === 'hashing' ? '准备中' : `${Math.round(file.progress * 100)}%`}</Text>
          </View>
        ) : file.status === 'failed' ? (
          <Text variant="caption" color={semantic.danger}>
            {file.error ?? '上传失败'}
          </Text>
        ) : (
          <Text variant="small">
            {file.format === 'text' ? '粘贴的文字' : mb >= 0.1 ? `${mb.toFixed(1)} MB` : '小于 0.1 MB'}
            {file.status === 'uploaded' ? (file.duplicate ? ' · 之前传过，不重复扣额度' : ' · 已上传') : ''}
          </Text>
        )}
      </View>
      {file.duplicate ? <Tag label="已有" tone="info" /> : null}
      {onRemove && file.status !== 'uploading' ? (
        <Pressable accessibilityRole="button" accessibilityLabel={`移除 ${file.name}`} onPress={onRemove} style={styles.remove}>
          <Icon name="close" size={18} color={semantic.textSecondary} />
        </Pressable>
      ) : null}
    </View>
  );
}

/** 分段筛选（1.7 全部 / 需核对 / 主观题 / 客观题）。 */
export function Segments<T extends string>({ options, value, onChange }: { options: { key: T; label: string; count?: number }[]; value: T; onChange: (v: T) => void }) {
  return (
    <View style={styles.segments} accessibilityRole="tablist">
      {options.map((o) => (
        <Pressable
          key={o.key}
          accessibilityRole="tab"
          accessibilityState={{ selected: o.key === value }}
          onPress={() => onChange(o.key)}
          style={[styles.segment, o.key === value && styles.segmentOn]}
        >
          <Text variant="caption" color={o.key === value ? semantic.textOnBrand : semantic.textPrimary}>
            {o.label}
            {o.count !== undefined ? ` ${o.count}` : ''}
          </Text>
        </Pressable>
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  header: { paddingBottom: spacing.lg, gap: 6 },
  title: { marginTop: spacing.lg },
  desc: { fontSize: 14, lineHeight: 21 },
  check: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, minHeight: 44 },
  box: { width: 20, height: 20, borderRadius: 10, borderWidth: 1.5, borderColor: '#BDB8AD', alignItems: 'center', justifyContent: 'center' },
  boxOn: { backgroundColor: semantic.textPrimary, borderColor: semantic.textPrimary },
  flex: { flex: 1 },
  file: { flexDirection: 'row', alignItems: 'center', gap: 12, paddingVertical: 14, borderBottomWidth: 1, borderBottomColor: semantic.border },
  badge: { width: 34, height: 34, borderRadius: 8, alignItems: 'center', justifyContent: 'center' },
  badgeText: { fontSize: 10, fontWeight: '700' },
  remove: { width: 40, height: 44, alignItems: 'center', justifyContent: 'center', marginRight: -10 },
  progress: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  segments: { flexDirection: 'row', gap: 6, flexWrap: 'wrap' },
  segment: { paddingHorizontal: 14, minHeight: 34, justifyContent: 'center', borderRadius: radius.pill, backgroundColor: semantic.fill },
  segmentOn: { backgroundColor: semantic.primary, borderColor: semantic.primary },
});
