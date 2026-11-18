import { Platform } from 'react-native';

/** 是否运行在浏览器里（H5 版）。App 上恒为 false，相关分支不会执行。 */
export const isWeb = Platform.OS === 'web';
