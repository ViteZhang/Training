// 基础组件演示页（T02 验收）：按钮、卡片、标签、进度条、底部弹层、确认弹窗、Toast，以及五种状态。仅开发版可进入。
import { semantic, spacing } from '@training/ui-tokens';
import { ApiError } from '@training/api-client';
import { router } from 'expo-router';
import { useState } from 'react';
import { StyleSheet, View } from 'react-native';
import {
  AIFailed,
  AIGenerating,
  BottomSheet,
  Button,
  Card,
  ConfirmDialog,
  EmptyState,
  ErrorState,
  Logo,
  ProgressBar,
  QuotaSheet,
  Screen,
  Skeleton,
  Tag,
  Text,
  toast,
} from '@/components';

export default function ComponentsDemo() {
  const [sheet, setSheet] = useState(false);
  const [dialog, setDialog] = useState(false);
  const [quota, setQuota] = useState(false);
  return (
    <Screen scroll>
      <View style={styles.section}>
        <Button title="返回" kind="text" onPress={() => router.back()} style={{ alignSelf: 'flex-start' }} />
        <Text variant="h1">基础组件</Text>
        <Text variant="caption">颜色、字号来自 ui-tokens（VI v1.0）</Text>
      </View>

      <Card style={styles.section}>
        <Text variant="h2">文字层级</Text>
        <Text variant="h1">专业课预估分</Text>
        <Text variant="h2">失分诊断</Text>
        <Text variant="body">采分点逐条批改，并引用你的原话。</Text>
        <Text variant="caption">根据你导入的 2 套真题卷实测估算</Text>
        <Text variant="score">128 / 150</Text>
      </Card>

      <Card style={[styles.section, { backgroundColor: semantic.primary }]}>
        <View style={styles.row}>
          <Logo size={48} />
          <Text variant="h2" color="#FFFFFF">
            预估分 118–126
          </Text>
        </View>
        <ProgressBar value={0.7} target={0.85} onBrand height={8} />
      </Card>

      <Card style={styles.section}>
        <Text variant="h2">按钮与标签</Text>
        <Button title="开始今日训练" onPress={() => toast('已开始')} />
        <Button title="次要按钮" kind="secondary" onPress={() => setSheet(true)} />
        <Button title="文字按钮" kind="text" onPress={() => setDialog(true)} />
        <Button title="提交中" loading />
        <View style={styles.row}>
          <Tag label="AI 生成" tone="ai" />
          <Tag label="已掌握" tone="mastered" />
          <Tag label="到期复习" tone="info" />
          <Tag label="薄弱" tone="danger" />
          <Tag label="真题 2023" />
        </View>
        <ProgressBar value={0.42} />
      </Card>

      <Card style={styles.section}>
        <Text variant="h2">五种状态</Text>
        <Text variant="bodyStrong">加载中（骨架屏）</Text>
        <Skeleton />
        <Text variant="bodyStrong">空状态</Text>
        <EmptyState title="还没有导入资料" desc="先导入一份真题或讲义，AI 帮你建好题库" actionText="导入第一份资料" onAction={() => toast('去导入')} />
        <Text variant="bodyStrong">网络错误</Text>
        <ErrorState error={new ApiError(0, undefined)} onRetry={() => toast('重试')} />
        <Text variant="bodyStrong">AI 生成中</Text>
        <AIGenerating steps={['提取要点', '比对 6 个采分点', '生成建议']} current={1} eta="约 5 秒" />
        <AIFailed onRetry={() => toast('重试')} />
        <Text variant="bodyStrong">额度不足</Text>
        <Button title="打开额度不足弹层" kind="secondary" onPress={() => setQuota(true)} />
      </Card>

      <BottomSheet visible={sheet} onClose={() => setSheet(false)} title="请阅读并同意以下条款">
        <Text variant="body">你上传的资料和题目仅本人可见。</Text>
        <Button title="同意并获取验证码" onPress={() => setSheet(false)} style={{ marginTop: spacing.lg }} />
      </BottomSheet>
      <ConfirmDialog
        visible={dialog}
        title="删除这份资料？"
        message="从它识别出的题连同作答记录和错题会一起删除。"
        confirmText="删除"
        danger
        onConfirm={() => setDialog(false)}
        onCancel={() => setDialog(false)}
      />
      <QuotaSheet
        visible={quota}
        onClose={() => setQuota(false)}
        title="今天的批改次数用完了"
        desc="答案已保存。免费版每天 3 次，会员不限次。"
        onUpgrade={() => setQuota(false)}
        freeOptions={[
          { label: '先对照参考答案', onPress: () => setQuota(false) },
          { label: '明天再批改', onPress: () => setQuota(false) },
        ]}
      />
    </Screen>
  );
}

const styles = StyleSheet.create({
  section: { marginBottom: spacing.lg, gap: spacing.md },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, flexWrap: 'wrap' },
});
