// 6.1 我的（T24）—— T02 只放占位，并提供基础组件演示页入口（仅开发版显示）。
import { router } from 'expo-router';
import { Button, EmptyState, Screen, Text } from '@/components';
import { appConfig } from '@/lib/config';

export default function MeTab() {
  return (
    <Screen>
      <Text variant="h1" style={{ marginTop: 16 }}>
        我的
      </Text>
      <EmptyState title="页面开发中" desc="6.1 我的（T24）" />
      {appConfig.variant !== 'production' ? <Button title="基础组件演示" kind="secondary" onPress={() => router.push('/dev/components')} /> : null}
    </Screen>
  );
}
