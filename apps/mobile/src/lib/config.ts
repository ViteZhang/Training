import Constants from 'expo-constants';

type Extra = { apiBaseUrl?: string; variant?: string };
const extra = (Constants.expoConfig?.extra ?? {}) as Extra;

/** 运行时配置，来自 app.config.ts 的 extra。 */
export const appConfig = {
  appName: Constants.expoConfig?.name ?? '考研Training',
  apiBaseUrl: extra.apiBaseUrl ?? 'https://training.dreamelab.cn/api/v1',
  variant: extra.variant ?? 'development',
  version: Constants.expoConfig?.version ?? '0.0.0',
};
