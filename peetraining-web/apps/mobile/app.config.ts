import type { ConfigContext, ExpoConfig } from 'expo/config';

/**
 * App 配置。显示名、包名、接口地址都从环境变量读取，不写死（CLAUDE.md）。
 *   APP_NAME            显示名，默认「考研Training」
 *   APP_ID              iOS Bundle ID 与安卓包名，默认 cn.dreamelab.training（D4）
 *   API_BASE_URL        接口地址，默认生产 https://training.dreamelab.cn/api/v1；本地开发指向 mock 或本机后端
 *   APP_VARIANT         development / preview / production（EAS 三个配置，T05）
 */
const variant = process.env.APP_VARIANT ?? 'development';
const appId = process.env.APP_ID ?? 'cn.dreamelab.training';
const baseName = process.env.APP_NAME ?? '考研Training';

export default ({ config }: ConfigContext): ExpoConfig => ({
  ...config,
  name: variant === 'production' ? baseName : `${baseName}${variant === 'preview' ? ' 内测' : ' 开发'}`,
  slug: 'peetraining',
  scheme: 'peetraining',
  version: '0.1.0',
  orientation: 'portrait',
  icon: './assets/icon.png',
  userInterfaceStyle: 'light',
  backgroundColor: '#FAF8F3',
  ios: {
    bundleIdentifier: variant === 'production' ? appId : `${appId}.${variant}`,
    supportsTablet: false,
    infoPlist: {
      NSCameraUsageDescription: '用于拍照导入题目和拍手写稿识别',
      NSPhotoLibraryUsageDescription: '用于从相册导入题目和资料',
      NSMicrophoneUsageDescription: '用于语音作答和口述背诵',
    },
  },
  android: {
    package: variant === 'production' ? appId : `${appId}.${variant}`,
    adaptiveIcon: {
      backgroundColor: '#231F55',
      foregroundImage: './assets/android-icon-foreground.png',
      backgroundImage: './assets/android-icon-background.png',
      monochromeImage: './assets/android-icon-monochrome.png',
    },
    predictiveBackGestureEnabled: false,
  },
  web: { favicon: './assets/favicon.png' },
  plugins: [
    'expo-router',
    [
      'expo-splash-screen',
      { image: './assets/splash-icon.png', imageWidth: 120, backgroundColor: '#231F55' },
    ],
    'expo-font',
    [
      'expo-image-picker',
      {
        photosPermission: '用于从相册导入题目和资料',
        cameraPermission: '用于拍照导入题目和拍手写稿识别',
        microphonePermission: false,
      },
    ],
    'expo-document-picker',
    ['expo-notifications', { color: '#231F55' }],
    // 口述背诵（4.16，功能开关 oral_recite）与语音作答（voice_answer）录音
    ['expo-audio', { microphonePermission: '用于语音作答和口述背诵' }],
  ],
  experiments: { typedRoutes: true },
  extra: {
    apiBaseUrl: process.env.API_BASE_URL ?? 'https://training.dreamelab.cn/api/v1',
    variant,
  },
});
