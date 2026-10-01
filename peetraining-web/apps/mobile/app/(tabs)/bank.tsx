// 3.1 题库（T13） —— T02 只放占位，页面在对应任务卡里实现。
import { EmptyState, Screen, Text } from '@/components';

export default function BankTab() {
  return (
    <Screen>
      <Text variant="h1" style={{ marginTop: 16 }}>
        题库
      </Text>
      <EmptyState title="页面开发中" desc="3.1 题库（T13）" />
    </Screen>
  );
}
