// 1.4 选择导入方式 —— 导入流程（1.4–1.8）在 T12 实现；T07 先提供「先跳过，进入 App」。
import { router } from 'expo-router';
import { StyleSheet } from 'react-native';
import { Button, EmptyState, Screen } from '@/components';
import { setStep } from '@/features/onboarding/api';
import { Footer, StepHeader } from '@/features/onboarding/ui';

export default function ImportStep() {
  return (
    <Screen>
      <StepHeader step={4} title="导入你的第一份资料" desc="AI 会把它整理成能刷、能批改的题库，仅你本人可见。" onBack={() => router.back()} />
      <EmptyState title="导入功能开发中" desc="1.4–1.8 导入流程（T12）" />
      <Footer>
        <Button
          title="先跳过，进入 App"
          kind="text"
          onPress={() => {
            void setStep('done').then(() => router.replace('/(tabs)/today'));
          }}
          style={styles.skip}
        />
      </Footer>
    </Screen>
  );
}

const styles = StyleSheet.create({ skip: { alignSelf: 'center' } });
