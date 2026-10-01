// 4.1 训练首页（T17） —— T02 只放占位，页面在对应任务卡里实现。
import { EmptyState, Screen, Text } from '@/components';

export default function TrainTab() {
  return (
    <Screen>
      <Text variant="h1" style={{ marginTop: 16 }}>
        训练
      </Text>
      <EmptyState title="页面开发中" desc="4.1 训练首页（T17）" />
    </Screen>
  );
}
