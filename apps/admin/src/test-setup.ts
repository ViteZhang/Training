// antd 依赖 matchMedia，jsdom 没有。
Object.defineProperty(window, 'matchMedia', {
  writable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  }),
});

// 接口客户端在模块加载时拿 fetch：这里换成转发到 mockApi 的版本，测试里按「方法 路径」配置响应。
type Handler = (req: Request) => { status: number; body?: unknown } | Promise<{ status: number; body?: unknown }>;
const g = globalThis as unknown as { mockApi: Record<string, Handler>; apiCalls: Request[] };
g.mockApi = {};
g.apiCalls = [];
globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
  const req = input instanceof Request ? input : new Request(new URL(String(input), 'http://localhost'), init);
  g.apiCalls.push(req.clone());
  const path = new URL(req.url).pathname.replace(/^\/api\/v1/, '');
  const key = `${req.method} ${path}`;
  const h = g.mockApi[key] ?? Object.entries(g.mockApi).find(([k]) => new RegExp(`^${k.replace(/\{[^}]+\}/g, '[^/]+')}$`).test(key))?.[1];
  const r = h ? await h(req) : { status: 404, body: { code: 'NOT_FOUND', message: '内容不存在' } };
  return new Response(r.status === 204 ? null : JSON.stringify(r.body ?? {}), { status: r.status, headers: { 'Content-Type': 'application/json' } });
}) as typeof fetch;

// vitest 没开 globals 时 Testing Library 不会自动清理，每个用例后手动卸载。
import { cleanup } from '@testing-library/react';
import { afterEach } from 'vitest';
afterEach(() => cleanup());

// antd 的表格与图片依赖 ResizeObserver，jsdom 没有。
class RO {
  observe() {}
  unobserve() {}
  disconnect() {}
}
(globalThis as unknown as { ResizeObserver: typeof RO }).ResizeObserver ??= RO;
