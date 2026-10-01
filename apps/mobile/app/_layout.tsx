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

/** 标题用思源宋体子集、数字用 Sora，构建时打包，不在运行时加载 Google Fonts（ADR 0010）。 */
const fonts = {
  'NotoSerifSC-Heavy': require('../assets/fonts/NotoSerifSC-Heavy.ttf'),
  'NotoSerifSC-Bold': require('../assets/fonts/NotoSerifSC-Bold.ttf'),
  'Sora-SemiBold': require('../assets/fonts/Sora-SemiBold.ttf'),
  'Sora-Bold': require('../assets/fonts/Sora-Bold.ttf'),
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
