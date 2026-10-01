import { layout, semantic } from '@training/ui-tokens';
import { ScrollView, StyleSheet, View, type ViewProps } from 'react-native';
import { SafeAreaView, type Edge } from 'react-native-safe-area-context';

export interface ScreenProps extends ViewProps {
  scroll?: boolean;
  edges?: Edge[];
  padded?: boolean;
}

/** 页面容器：纸白底、安全区、默认 20 页边距。 */
export function Screen({ scroll, edges = ['top'], padded = true, style, children, ...rest }: ScreenProps) {
  const inner = [padded && styles.padded, style];
  return (
    <SafeAreaView edges={edges} style={styles.root}>
      {scroll ? (
        <ScrollView contentContainerStyle={inner} {...rest}>
          {children}
        </ScrollView>
      ) : (
        <View style={[styles.fill, ...inner]} {...rest}>
          {children}
        </View>
      )}
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1, backgroundColor: semantic.background },
  fill: { flex: 1 },
  padded: { paddingHorizontal: layout.pagePadding },
});
