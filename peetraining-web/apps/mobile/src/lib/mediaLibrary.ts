/** 相册写入（6.5a 保存客服二维码）。H5 版改用同目录的 mediaLibrary.web.ts：浏览器不能写相册，expo-media-library 在浏览器里一加载就报错。 */
export { Asset, requestPermissionsAsync } from 'expo-media-library';
