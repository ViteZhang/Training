import { layout, semantic } from '@training/ui-tokens';
import { useEffect, useState } from 'react';
import { Keyboard, KeyboardAvoidingView, Platform, ScrollView, StyleSheet, View, type ViewProps } from 'react-native';
import { SafeAreaView, type Edge } from 'react-native-safe-area-context';

export interface ScreenProps extends ViewProps {
  scroll?: boolean;
  /**
   * 让开哪些系统区域，默认上下都让开：安卓（Expo SDK 57 强制 edge-to-edge）页面会画到虚拟导航键后面，
   * 不让开底部，吸底按钮会被返回键、主页键挡住。底部有 Tab 栏的四个 Tab 页传 ['top']（Tab 栏自己让开了）。
   */
  edges?: Edge[];
  padded?: boolean;
}

/**
 * 页面容器：白底、安全区、默认 20 页边距。
 * 键盘弹起时整页让出键盘的高度（behavior="padding"）：edge-to-edge 下安卓不再自动缩小窗口，
 * 不处理的话输入框和吸底按钮都会被键盘挡住；页面里的 ScrollView 随之变矮，安卓会把正在输入的框滚进可见区域。
 */
export function Screen({ scroll, edges = ['top', 'bottom'], padded = true, style, children, ...rest }: ScreenProps) {
  const inner = [padded && styles.padded, style];
  // 键盘弹起时让出的高度已含底部系统区域，再留底部安全区会在键盘上方多出一条空白
  const keyboard = useKeyboardShown();
  const safeEdges = keyboard ? edges.filter((e) => e !== 'bottom') : edges;
  return (
    <KeyboardAvoidingView behavior="padding" style={styles.root}>
      <SafeAreaView edges={safeEdges} style={styles.fill}>
        {scroll ? (
          <ScrollView contentContainerStyle={inner} keyboardShouldPersistTaps="handled" {...rest}>
            {children}
          </ScrollView>
        ) : (
          <View style={[styles.fill, ...inner]} {...rest}>
            {children}
          </View>
        )}
      </SafeAreaView>
    </KeyboardAvoidingView>
  );
}

function useKeyboardShown() {
  const [shown, setShown] = useState(false);
  useEffect(() => {
    const ios = Platform.OS === 'ios';
    const show = Keyboard.addListener(ios ? 'keyboardWillShow' : 'keyboardDidShow', () => setShown(true));
    const hide = Keyboard.addListener(ios ? 'keyboardWillHide' : 'keyboardDidHide', () => setShown(false));
    return () => {
      show.remove();
      hide.remove();
    };
  }, []);
  return shown;
}

const styles = StyleSheet.create({
  root: { flex: 1, backgroundColor: semantic.background },
  fill: { flex: 1 },
  padded: { paddingHorizontal: layout.pagePadding },
});
