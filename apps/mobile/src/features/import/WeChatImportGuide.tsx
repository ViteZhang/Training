import { semantic, spacing } from '@training/ui-tokens';
import { useState } from 'react';
import { Linking, Platform, StyleSheet, View } from 'react-native';
import { BottomSheet, Button, Text, toast } from '@/components';
import { appConfig } from '@/lib/config';

/** 微信不开放 App 读取聊天记录；由用户主动分享文件给本 App。 */
export function WeChatImportGuide({ pickFile }: { pickFile: () => void }) {
  const [visible, setVisible] = useState(false);
  const openWeChat = async () => {
    try { await Linking.openURL('weixin://'); }
    catch { toast('暂时无法打开微信，请手动打开微信并按步骤分享文件'); }
  };
  return (
    <>
      <Button title="从微信导入" kind="secondary" onPress={() => setVisible(true)} />
      <BottomSheet visible={visible} onClose={() => setVisible(false)} title="从微信聊天导入文件">
        <View style={styles.gap}>
          <Text variant="body">1. 打开微信聊天，点开要导入的文件，等待下载完成。</Text>
          <Text variant="body">2. 点右上角「…」，选择「其他应用打开」或「分享」。</Text>
          <Text variant="body">3. 选择「{appConfig.appName}」，再选择专业课和导入类型。</Text>
          {Platform.OS === 'ios' ? <Text variant="caption">iPhone 分享后点「保存待导入」，再打开本 App。文件会保留，登录后也能继续导入。</Text> : <Text variant="caption">接收后会先保存文件，选择专业课并确认后才上传。</Text>}
          <Text variant="caption">没找到本 App？请在分享列表中查看「更多」，并确认安装了支持文件分享的新版 App。也可以先将文件保存到手机，再从下面选择。</Text>
          <Text variant="small">支持 PDF、Word（.docx）、Excel（.xlsx）和图片；旧版 .doc／.xls 请先另存为新格式。</Text>
          <Button title="打开微信，按步骤分享" onPress={() => void openWeChat()} />
          <Button title="已保存到手机，选择文件" kind="secondary" onPress={() => { setVisible(false); pickFile(); }} />
          <Button title="知道了" kind="text" onPress={() => setVisible(false)} />
        </View>
      </BottomSheet>
    </>
  );
}
const styles = StyleSheet.create({ gap: { gap: spacing.md, paddingTop: spacing.sm, paddingBottom: spacing.sm, borderColor: semantic.border } });
