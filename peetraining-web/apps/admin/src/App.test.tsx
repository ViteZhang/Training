import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { App } from './App';
import { AppLayout } from './components/AppLayout';
import { maskPhone } from './lib/mask';
import { visibleMenu } from './lib/menu';
import { clearSession, setSession } from './lib/session';

type Handler = (req: Request) => { status: number; body?: unknown } | Promise<{ status: number; body?: unknown }>;
const g = globalThis as unknown as { mockApi: Record<string, Handler>; apiCalls: Request[] };

const me = (over: object = {}) => ({ id: 1, username: 'kefu', display_name: '客服小王', roles: ['support'], must_change_password: false, expires_at: '2030-01-01T00:00:00Z', ...over });

function renderApp(path = '/') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <App />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  g.apiCalls.length = 0;
  g.mockApi = { 'GET /admin/me': () => ({ status: 200, body: me() }) };
});
afterEach(() => clearSession());

describe('后台登录', () => {
  it('账号密码 → 短信验证码 → 按角色进入首页', async () => {
    g.mockApi['POST /admin/auth/login'] = () => ({ status: 200, body: { challenge_id: 'c1', phone_masked: '137****0001' } });
    g.mockApi['POST /admin/auth/verify'] = async (req) => {
      const b = (await req.json()) as { challenge_id: string; code: string };
      return b.code === '123456' ? { status: 200, body: { token: 'tok', admin: me() } } : { status: 400, body: { code: 'BAD_REQUEST', message: '验证码错误' } };
    };
    g.mockApi['GET /admin/users'] = () => ({ status: 200, body: { items: [] } });
    renderApp();
    fireEvent.change(screen.getByLabelText('账号'), { target: { value: 'kefu' } });
    fireEvent.change(screen.getByLabelText('密码'), { target: { value: 'pw' } });
    fireEvent.click(screen.getByRole('button', { name: '下一步' }));
    expect(await screen.findByText('验证码已发送到 137****0001')).toBeTruthy();
    fireEvent.change(screen.getByLabelText('短信验证码'), { target: { value: '000000' } });
    fireEvent.click(screen.getByRole('button', { name: '登 录' }));
    expect(await screen.findByText('验证码错误')).toBeTruthy();
    fireEvent.change(screen.getByLabelText('短信验证码'), { target: { value: '123456' } });
    fireEvent.click(screen.getByRole('button', { name: '登 录' }));
    // 客服没有 7.1：落到第一个可见页面 7.2 用户。
    expect(await screen.findByRole('heading', { name: '7.2 用户' })).toBeTruthy();
    expect(screen.queryByText('7.1 概览')).toBeNull();
    expect(g.apiCalls.some((r) => r.url.endsWith('/admin/users') && r.headers.get('Authorization') === 'Bearer tok')).toBe(true);
  });

  it('首次登录必须修改初始密码', async () => {
    setSession('tok');
    g.mockApi['GET /admin/me'] = () => ({ status: 200, body: me({ must_change_password: true }) });
    renderApp();
    expect(await screen.findByText('修改初始密码')).toBeTruthy();
  });

  it('会话过期回到登录页', async () => {
    setSession('expired');
    g.mockApi['GET /admin/me'] = () => ({ status: 401, body: { code: 'UNAUTHORIZED', message: '请先登录' } });
    renderApp();
    expect(await screen.findByLabelText('账号')).toBeTruthy();
  });
});

describe('后台壳', () => {
  it('菜单按角色显示五组', () => {
    expect(visibleMenu(['admin']).map((gr) => gr.title)).toEqual(['运营', '质量', '配置', '官方题库', '系统']);
    expect(visibleMenu(['content_editor']).flatMap((gr) => gr.pages.map((p) => p.code))).toEqual(['7.12']);
    expect(visibleMenu(['analyst']).flatMap((gr) => gr.pages.map((p) => p.code))).toEqual(['7.1', '7.10']);
  });

  it('布局显示账号名与退出', () => {
    let out = false;
    render(
      <MemoryRouter>
        <AppLayout account={{ name: '客服小王', roles: ['support'] }} now={new Date(2026, 9, 1, 9, 5)} onLogout={() => (out = true)}>
          <div>内容</div>
        </AppLayout>
      </MemoryRouter>,
    );
    expect(screen.getByText('客服小王')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /退\s*出/ }));
    expect(out).toBe(true);
  });

  it('手机号脱敏', () => {
    expect(maskPhone('13812345678')).toBe('138****5678');
    expect(maskPhone('abc')).toBe('abc');
  });
});

void waitFor;
