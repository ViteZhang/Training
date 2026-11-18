/** H5 版没有相册写入：调用方在 Web 上先行提示「长按二维码保存」，这里只保证页面能加载。App 打包不会用到这个文件。 */
export async function requestPermissionsAsync(_writeOnly?: boolean, _granularPermissions?: string[]) {
  return { granted: false, canAskAgain: false };
}

export const Asset = {
  async create(_uri: string): Promise<never> {
    throw new Error('网页版不支持保存到相册');
  },
};
