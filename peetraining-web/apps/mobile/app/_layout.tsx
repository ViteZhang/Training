import { QueryClientProvider } from '@tanstack/react-query';
import { semantic } from '@training/ui-tokens';
import { useFonts } from 'expo-font';
import { Stack } from 'expo-router';
import * as SplashScreen from 'expo-splash-screen';
import { StatusBar } from 'expo-status-bar';
import { useEffect } from 'react';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import { ToastHost } from '@/components';
import { queryClient } from '@/lib/queryClient';

void SplashScreen.preventAutoHideAsync();

/** 数字用 DM Sans（与设计稿一致），构建时打包，不在运行时加载 Google Fonts（ADR 0010）。中文用系统字体。 */
const fonts = {
  'DMSans-Regular': require('../assets/fonts/DMSans-Regular.ttf'),
  'DMSans-Medium': require('../assets/fonts/DMSans-Medium.ttf'),
  'DMSans-SemiBold': require('../assets/fonts/DMSans-SemiBold.ttf'),
  'DMSans-Bold': require('../assets/fonts/DMSans-Bold.ttf'),
};

export default function RootLayout() {
  const [loaded, error] = useFonts(fonts);
  useEffect(() => {
    if (loaded || error) void SplashScreen.hideAsync();
  }, [loaded, error]);
  if (!loaded && !error) return null;

  return (
    <SafeAreaProvider>
      <QueryClientProvider client={queryClient}>
        <StatusBar style="dark" />
        <Stack screenOptions={{ headerShown: false, contentStyle: { backgroundColor: semantic.background } }} />
        <ToastHost />
      </QueryClientProvider>
    </SafeAreaProvider>
  );
}
