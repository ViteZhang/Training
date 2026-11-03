// 1.5 隐私承诺卡（PRD v3 增补「内容来源与隐私承诺」，设计稿 m1_upload）：上传页、官方题库页、隐私政策展示同样的四条承诺。
import { colors, radius, spacing } from '@training/ui-tokens';
import { StyleSheet, View } from 'react-native';
import { Icon, Text } from '@/components';
import { appConfig } from '@/lib/config';

export const privacyPromises = ['仅你本人可见，后台看不到内容', '不用于训练 AI 模型', '官方题库不使用任何用户上传的资料', '随时可以删除或导出'];

export function PrivacyCard() {
  return (
    <View style={styles.card} accessibilityRole="summary">
      <View style={styles.row}>
        <Icon name="lock" size={18} color={colors.green} />
        <Text variant="bodyStrong" color={colors.greenDeep}>你的资料只属于你</Text>
      </View>
      {privacyPromises.map((p) => (
        <View key={p} style={styles.row}>
          <Icon name="check" size={16} color={colors.green} />
          <Text variant="caption" color={colors.ink}>{p}</Text>
        </View>
      ))}
    </View>
  );
}

/** 「文件在微信里？」默认展开的三步说明（1.5）。 */
export function WeChatSteps() {
  return (
    <View style={styles.wechat}>
      <Text variant="bodyStrong">文件在微信里？</Text>
      <Text variant="caption" color={colors.ink}>1. 在微信里打开这个文件</Text>
      <Text variant="caption" color={colors.ink}>2. 点右上角「···」，选「用其他应用打开」</Text>
      <Text variant="caption" color={colors.ink}>3. 在列表里选「{appConfig.appName}」，文件会直接导进来</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  card: { gap: 10, padding: spacing.lg, borderRadius: radius.xl + 2, backgroundColor: colors.greenSoft },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  wechat: { gap: 6, paddingHorizontal: spacing.lg, paddingVertical: 14, borderRadius: radius.xl, backgroundColor: colors.fill },
});
