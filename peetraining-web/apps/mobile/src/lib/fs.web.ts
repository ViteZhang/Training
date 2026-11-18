/**
 * H5 版的本地文件：浏览器没有文件系统，选中的文件、照片是 blob: 地址。
 * 这里只实现业务代码用到的那部分 expo-file-system 接口：
 *   - 「复制到 App 目录」只在内存里记一条别名，指回原来的 blob 地址（刷新页面后 blob 失效，未传完的需要重选）
 *   - 读取用 fetch，上传用 XMLHttpRequest（要进度）
 * App 打包不会用到这个文件（见 fs.ts）。
 */
const aliases = new Map<string, string>();
const sizes = new Map<string, number>();

const resolve = (uri: string): string => {
  let cur = uri;
  for (let i = 0; i < 8 && aliases.has(cur); i++) cur = aliases.get(cur)!;
  return cur;
};
const join = (base: string, name: string) => (base.endsWith('/') ? base : `${base}/`) + name;
const uriOf = (part: string | { uri: string }) => (typeof part === 'string' ? part : part.uri);

/** 选文件后登记大小（浏览器里没法同步读文件大小）。 */
export function rememberSize(uri: string, size: number) {
  sizes.set(uri, size);
}

export class Directory {
  readonly uri: string;
  constructor(base: string | { uri: string }, ...names: string[]) {
    this.uri = names.reduce(join, uriOf(base));
    if (!this.uri.endsWith('/')) this.uri += '/';
  }
  get exists() {
    return true;
  }
  get size() {
    return 0;
  }
  create(_options?: unknown) {}
  list(): (File | Directory)[] {
    return [];
  }
  delete() {}
}

export class File {
  readonly uri: string;
  constructor(base: string | { uri: string }, ...names: string[]) {
    this.uri = names.reduce(join, uriOf(base));
  }
  private get source() {
    return resolve(this.uri);
  }
  get exists() {
    return /^(blob:|data:|https?:)/.test(this.source);
  }
  get size() {
    return sizes.get(this.source) ?? sizes.get(this.uri) ?? 0;
  }
  private async blob(): Promise<Blob> {
    if (!this.exists) throw new Error('文件已失效，请重新选择');
    const blob = await (await fetch(this.source)).blob();
    sizes.set(this.source, blob.size);
    return blob;
  }
  async arrayBuffer(): Promise<ArrayBuffer> {
    return (await this.blob()).arrayBuffer();
  }
  async bytes(): Promise<Uint8Array> {
    return new Uint8Array(await this.arrayBuffer());
  }
  copy(target: File) {
    aliases.set(target.uri, this.source);
  }
  move(target: File) {
    this.copy(target);
  }
  delete() {
    aliases.delete(this.uri);
  }
  async upload(
    url: string,
    options: { httpMethod?: string; headers?: Record<string, string>; onProgress?: (p: { bytesSent: number; totalBytes: number }) => void } = {},
  ): Promise<{ status: number; body: string; headers: Record<string, string> }> {
    const blob = await this.blob();
    return new Promise((done, fail) => {
      const xhr = new XMLHttpRequest();
      xhr.open(options.httpMethod ?? 'POST', url);
      for (const [k, v] of Object.entries(options.headers ?? {})) xhr.setRequestHeader(k, v);
      xhr.upload.onprogress = (e) => options.onProgress?.({ bytesSent: e.loaded, totalBytes: e.lengthComputable ? e.total : blob.size });
      xhr.onload = () => done({ status: xhr.status, body: xhr.responseText, headers: {} });
      xhr.onerror = () => fail(new Error('上传没成功，请检查网络后重试'));
      xhr.ontimeout = () => fail(new Error('上传超时，请重试'));
      xhr.send(blob);
    });
  }
  /** H5 不落盘：返回一个指向下载地址的 File，调用方在 Web 上直接打开地址。 */
  static async downloadFileAsync(url: string, target: File | Directory): Promise<File> {
    aliases.set(target.uri, url);
    return new File(target.uri);
  }
}

export const Paths = {
  cache: new Directory('webfs://cache'),
  document: new Directory('webfs://document'),
};
