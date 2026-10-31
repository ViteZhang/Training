import type { components } from '@training/api-client';
import { afterEach, expect, it, vi } from 'vitest';
import { uploadTextbookFile } from './textbookUpload';

const target = {
  upload_url: 'https://files.example.test/textbooks/imports/40/upload?signature=private-signature',
  upload_headers: { 'Content-Type': 'text/plain' },
} satisfies Pick<components['schemas']['TextbookImportUpload'], 'upload_url' | 'upload_headers'>;
const file = new File(['教材原文'], '教材.md');
afterEach(() => vi.unstubAllGlobals());

it('直传只使用签名请求头，不附加后台会话', async () => {
  const send = vi.fn().mockResolvedValue(new Response(null, { status: 200 }));
  vi.stubGlobal('fetch', send);
  await uploadTextbookFile(file, target);
  expect(send).toHaveBeenCalledExactlyOnceWith(target.upload_url, { method: 'PUT', headers: target.upload_headers, body: file });
});

it('浏览器拒绝上传时显示域名与跨域检查方向，不泄漏签名', async () => {
  vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')));
  const error = await uploadTextbookFile(file, target).catch((e: Error) => e);
  expect(error).toBeInstanceOf(Error);
  expect((error as Error).message).toContain('files.example.test');
  expect((error as Error).message).toContain(location.origin);
  expect((error as Error).message).not.toContain('private-signature');
});

it('OSS 返回错误时保留 HTTP 状态，不将响应正文或签名显示给用户', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('private response', { status: 403 })));
  await expect(uploadTextbookFile(file, target)).rejects.toThrow('HTTP 403');
});
