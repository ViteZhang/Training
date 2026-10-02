// 模块 1 导入流程（T12）：1.4 选择方式 → 1.5 选文件 → 1.6 解析 → 1.7 确认 → 1.8 完成。
import { Stack } from 'expo-router';

export default function ImportLayout() {
  return <Stack screenOptions={{ headerShown: false }} />;
}
