import { layout, radius, semantic, spacing } from '@training/ui-tokens';
import type { ReactNode } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { Button, Icon, ProgressBar, Tag, Text } from '@/components';
import type { Picked } from './files';

/** 导入流程里非引导页面的顶部：返回 + 标题 + 说明。 */
export function PageHeader({ title, desc, onBack, right }: { title: string; desc?: string; onBack?: () => void; right?: ReactNode }) {
  return (
    <View style={styles.header}>
      <View style={styles.headerRow}>
        {onBack ? <Button title="返回" kind="text" onPress={onBack} /> : <View />}
        {right}
      </View>
      <Text variant="h1" style={styles.title}>
        {title}
      </Text>
      {desc ? (
        <Text variant="body" color={semantic.textSecondary}>
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
      <View style={[styles.box, checked && styles.boxOn]}>{checked ? <Icon name="check" size={14} color={semantic.textOnBrand} /> : null}</View>
      <Text variant="caption" style={styles.flex} maxFontSizeMultiplier={layout.maxFontScale}>
        {label}
      </Text>
    </Pressable>
  );
}

const formatLabel: Record<Picked['format'], string> = { pdf: 'PDF', docx: 'DOC', xlsx: 'XLS', image: 'IMG', text: 'TXT' };

/** 1.5 已选文件的一行：格式、名称、上传进度或状态、移除。 */
export function FileRow({ file, onRemove }: { file: Picked; onRemove?: () => void }) {
  const mb = file.size / 1024 / 1024;
  return (
    <View style={styles.file} accessibilityLabel={file.name}>
      <View style={styles.badge}>
        <Text variant="small" color={semantic.primary}>
          {formatLabel[file.format]}
        </Text>
      </View>
      <View style={styles.flex}>
        <Text variant="bodyStrong" numberOfLines={1}>
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
          <Text variant="caption">
            {file.format === 'text' ? '粘贴的文字' : mb >= 0.1 ? `${mb.toFixed(1)} MB` : '小于 0.1 MB'}
            {file.status === 'uploaded' ? (file.duplicate ? ' · 之前传过，不重复扣额度' : ' · 已上传') : ''}
          </Text>
        )}
      </View>
      {file.duplicate ? <Tag label="已有" tone="info" /> : null}
      {onRemove && file.status !== 'uploading' ? <Button title="移除" kind="text" onPress={onRemove} /> : null}
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
  header: { paddingTop: spacing.sm, paddingBottom: spacing.lg, gap: spacing.xs },
  headerRow: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', minHeight: 44 },
  title: { marginTop: spacing.sm },
  check: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, minHeight: 44 },
  box: { width: 22, height: 22, borderRadius: radius.sm, borderWidth: 1.5, borderColor: semantic.border, alignItems: 'center', justifyContent: 'center' },
  boxOn: { backgroundColor: semantic.primary, borderColor: semantic.primary },
  flex: { flex: 1 },
  file: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, paddingVertical: spacing.sm, borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: semantic.border },
  badge: { width: 44, height: 44, borderRadius: radius.md, backgroundColor: semantic.primarySoft, alignItems: 'center', justifyContent: 'center' },
  progress: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  segments: { flexDirection: 'row', gap: spacing.xs, flexWrap: 'wrap' },
  segment: { paddingHorizontal: spacing.md, minHeight: 36, justifyContent: 'center', borderRadius: radius.lg, backgroundColor: semantic.surface, borderWidth: 1, borderColor: semantic.border },
  segmentOn: { backgroundColor: semantic.primary, borderColor: semantic.primary },
});
