import type { components } from '@training/api-client';

/** OSS 直传失败只显示存储域名，不泄漏预签名参数或后台会话。 */
export async function uploadTextbookFile(file: File, target: Pick<components['schemas']['TextbookImportUpload'], 'upload_url' | 'upload_headers'>) {
  let response: Response;
  try {
    response = await fetch(target.upload_url, { method: 'PUT', headers: target.upload_headers, body: file });
  } catch {
    const host = new URL(target.upload_url).host;
    throw new Error(`原件尚未上传：无法连接文件存储（${host}）。请检查网络，以及 OSS 是否允许当前后台来源 ${location.origin} 的 PUT 跨域请求；原任务可重新上传继续。`);
  }
  if (!response.ok) throw new Error(`原件上传失败（HTTP ${response.status}），请检查存储权限及签名配置，可重新选择同一份原件继续上传`);
}
