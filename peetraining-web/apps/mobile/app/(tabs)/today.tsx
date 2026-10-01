// 2.1 今日首页（T16） —— T02 只放占位，页面在对应任务卡里实现。
import { EmptyState, Screen, Text } from '@/components';

export default function TodayTab() {
  return (
    <Screen>
      <Text variant="h1" style={{ marginTop: 16 }}>
        今日
      </Text>
      <EmptyState title="页面开发中" desc="2.1 今日首页（T16）" />
    </Screen>
  );
}
