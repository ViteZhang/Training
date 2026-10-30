/** 外部分享先经过启动页的登录/协议检查，文件由根布局持久接收。 */
export function redirectSystemPath({ path, initial }: { path: string; initial: boolean }) {
  try {
    const url = new URL(path, 'peetraining://');
    if (url.hostname === 'expo-sharing' || /^(file|content):$/.test(url.protocol)) return initial ? '/' : '/import/shared';
  } catch { return '/'; }
  return path;
}
