// 6.5 会员中心（T25）—— 占位，供额度不足弹层跳转。
import { router } from 'expo-router';
import { Button, EmptyState, Screen } from '@/components';

export default function Member() {
  return (
    <Screen>
      <Button title="返回" kind="text" onPress={() => router.back()} style={{ alignSelf: 'flex-start' }} />
      <EmptyState title="会员中心开发中" desc="6.5 会员中心（T25）；内测期可在「我的 → 兑换码」开通" />
    </Screen>
  );
}
