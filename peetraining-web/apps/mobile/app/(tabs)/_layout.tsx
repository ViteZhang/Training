// 底部导航 4 个 Tab：今日、题库、训练、我的（PRD 4）。样式按设计稿 m2_home 的主导航，颜色换成 VI。
import { colors, layout, semantic } from '@training/ui-tokens';
import { Tabs } from 'expo-router';
import { Icon, type IconName } from '@/components/Icon';

const tabs: { name: string; title: string; icon: IconName }[] = [
  { name: 'today', title: '今日', icon: 'today' },
  { name: 'bank', title: '题库', icon: 'bank' },
  { name: 'train', title: '训练', icon: 'train' },
  { name: 'me', title: '我的', icon: 'me' },
];

export default function TabsLayout() {
  return (
    <Tabs
      screenOptions={{
        headerShown: false,
        tabBarActiveTintColor: colors.indigo,
        tabBarInactiveTintColor: '#8C877D',
        tabBarStyle: { backgroundColor: semantic.surface, borderTopColor: semantic.border, minHeight: 56 },
        tabBarLabelStyle: { fontSize: 11, fontWeight: '600' },
        tabBarItemStyle: { minHeight: layout.minTouch },
        sceneStyle: { backgroundColor: semantic.background },
      }}
    >
      {tabs.map((t) => (
        <Tabs.Screen
          key={t.name}
          name={t.name}
          options={{ title: t.title, tabBarIcon: ({ color }) => <Icon name={t.icon} color={color} /> }}
        />
      ))}
    </Tabs>
  );
}
