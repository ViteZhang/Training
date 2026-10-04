import type { Schemas } from '@training/api-client';
import { ApiError } from '@training/api-client';
import * as Crypto from 'expo-crypto';
import { File } from 'expo-file-system';
import { api, unwrap } from '@/lib/api';

export type Format = Schemas['MaterialFormat'];

/** 1.5 已选的一项：文件、照片或粘贴的文字。 */
export interface Picked {
  key: string;
  name: string;
  format: Format;
  /** 本地文件地址；粘贴的文字为空 */
  uri?: string;
  size: number;
  mimeType?: string;
  /** 粘贴的文字创建时就有资料 ID */
  materialId?: number;
  status: 'pending' | 'hashing' | 'uploading' | 'uploaded' | 'failed';
  progress: number;
  error?: string;
  duplicate?: boolean;
}

/** 单次上限（PRD 11.12；服务端也会校验，参数以服务端为准）。 */
export const limits = { maxFiles: 10, maxImages: 30, maxMB: 50, maxPasteChars: 20000 };

export class UnsupportedFile extends Error {}

/** 按扩展名判断格式；旧版 .doc / .xls 给出另存提示（PRD 11.12）。 */
export function formatOf(name: string, mimeType?: string): Format {
  const ext = name.toLowerCase().split('.').pop() ?? '';
  if (ext === 'docx') return 'docx';
  if (ext === 'xlsx') return 'xlsx';
  if (ext === 'pdf') return 'pdf';
  if (['jpg', 'jpeg', 'png', 'heic', 'heif', 'webp'].includes(ext) || mimeType?.startsWith('image/')) return 'image';
  if (ext === 'doc') throw new UnsupportedFile('旧版 Word（.doc）暂不支持，请另存为 .docx 后再选');
  if (ext === 'xls') throw new UnsupportedFile('旧版 Excel（.xls）暂不支持，请另存为 .xlsx 后再选');
  throw new UnsupportedFile(`「${name}」的格式暂不支持，可以选 Word、PDF、Excel 或图片`);
}

/** 检查加入后是否超过单次上限，超过返回提示。 */
export function checkLimits(current: Picked[], adding: Pick<Picked, 'format' | 'size' | 'name'>[]): string | undefined {
  const all = [...current, ...adding];
  const images = all.filter((f) => f.format === 'image').length;
  const files = all.filter((f) => f.format !== 'image' && f.format !== 'text').length;
  if (files > limits.maxFiles) return `每次最多 ${limits.maxFiles} 个文件`;
  if (images > limits.maxImages) return `图片每次最多 ${limits.maxImages} 张`;
  const big = adding.find((f) => f.size > limits.maxMB * 1024 * 1024);
  if (big) return `「${big.name}」超过 ${limits.maxMB} MB，请压缩或拆分后再选`;
  return undefined;
}

function hex(buf: ArrayBuffer) {
  return Array.from(new Uint8Array(buf), (b) => b.toString(16).padStart(2, '0')).join('');
}

/** 文件内容的 SHA-256，用来识别重复上传（同一文件不重复扣额度）。 */
export async function sha256OfFile(uri: string) {
  const buf = await new File(uri).arrayBuffer();
  return hex(await Crypto.digest(Crypto.CryptoDigestAlgorithm.SHA256, new Uint8Array(buf)));
}

export type Update = (key: string, patch: Partial<Picked>) => void;

/**
 * 上传已选的文件（1.5「开始解析」）：算哈希 → 申请直传地址 → PUT 到 OSS（带进度）→ 回调确认。
 * 已有资料 ID 的（粘贴的文字、上次传过的）跳过。返回全部资料 ID；有文件失败时抛出第一个错误。
 */
export async function uploadAll(subjectId: number, category: Schemas['MaterialCategory'] | undefined, files: Picked[], update: Update): Promise<number[]> {
  const todo = files.filter((f) => !f.materialId && f.uri);
  const hashes: string[] = [];
  for (const f of todo) {
    update(f.key, { status: 'hashing', error: undefined });
    hashes.push(await sha256OfFile(f.uri!));
  }
  if (todo.length > 0) {
    const res = await unwrap(
      api.POST('/materials/upload-requests', {
        body: {
          subject_id: subjectId,
          category,
          right_confirmed: true,
          files: todo.map((f, i) => ({ file_name: f.name, format: f.format, size_bytes: f.size, sha256: hashes[i]!, content_type: f.mimeType })),
        },
      }),
    );
    await Promise.all(
      res.items.map(async (t) => {
        const f = todo[t.index]!;
        if (t.duplicate || !t.upload_url) {
          update(f.key, { materialId: t.material_id, status: 'uploaded', progress: 1, duplicate: !!t.duplicate });
          f.materialId = t.material_id;
          return;
        }
        update(f.key, { status: 'uploading', progress: 0 });
        try {
          const r = await new File(f.uri!).upload(t.upload_url, {
            httpMethod: 'PUT',
            headers: t.upload_headers,
            onProgress: ({ bytesSent, totalBytes }) => update(f.key, { progress: totalBytes ? bytesSent / totalBytes : 0 }),
          });
          if (r.status < 200 || r.status >= 300) throw new Error(`上传失败（${r.status}）`);
          await unwrap(api.POST('/materials/{materialId}/uploaded', { params: { path: { materialId: t.material_id } } }));
          update(f.key, { materialId: t.material_id, status: 'uploaded', progress: 1 });
          f.materialId = t.material_id;
        } catch (e) {
          update(f.key, { status: 'failed', error: e instanceof ApiError ? e.message : '上传没成功，请检查网络后重试' });
          throw e;
        }
      }),
    );
  }
  return files.map((f) => f.materialId).filter((id): id is number => !!id);
}
