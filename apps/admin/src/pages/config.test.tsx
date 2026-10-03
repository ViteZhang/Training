import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { App as AntApp } from 'antd';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { ReactElement } from 'react';
import { beforeEach, describe, expect, it } from 'vitest';
import { flatten, setPath } from '../components/ParamEditor';
import { ParamsPanel } from './Config';
import { Roles } from './Roles';

type Handler = (req: Request) => { status: number; body?: unknown } | Promise<{ status: number; body?: unknown }>;
const g = globalThis as unknown as { mockApi: Record<string, Handler>; apiCalls: Request[] };

function wrap(ui: ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <AntApp>{ui}</AntApp>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  g.apiCalls.length = 0;
  g.mockApi = {};
});

describe('规则参数编辑器', () => {
  it('展开与按路径修改', () => {
    const v = { free: { grading_daily: 3, parse_pages_total: 100 }, member: { grading_daily: null } };
    expect(flatten(v).map((l) => l.path)).toEqual(['free.grading_daily', 'free.parse_pages_total', 'member.grading_daily']);
    expect(setPath(v, 'free.grading_daily', 5)).toEqual({ free: { grading_daily: 5, parse_pages_total: 100 }, member: { grading_daily: null } });
    expect(v.free.grading_daily).toBe(3);
  });

  it('改免费批改次数并带版本号保存', async () => {
    let saved: unknown;
    g.mockApi['GET /admin/config/params'] = () => ({
      status: 200,
      body: { items: [{ key: 'quota', value: { free: { grading_daily: 3 }, member: { grading_daily: null } }, description: 'PRD 13.1', version: 4, updated_at: '2026-10-03T02:00:00Z' }] },
    });
    g.mockApi['PUT /admin/config/params/{key}'] = async (req) => {
      saved = await req.json();
      return { status: 200, body: { key: 'quota', value: {}, description: '', version: 5, updated_at: '2026-10-03T02:00:00Z' } };
    };
    wrap(<ParamsPanel keys={['quota']} />);
    const input = await screen.findByLabelText('free.grading_daily');
    fireEvent.change(input, { target: { value: '5' } });
    fireEvent.click(screen.getByRole('button', { name: /保\s*存/ }));
    await waitFor(() => expect(saved).toEqual({ value: { free: { grading_daily: 5 }, member: { grading_daily: null } }, version: 4 }));
  });
});

describe('7.15 后台账号与权限', () => {
  it('成员列表显示角色与状态', async () => {
    g.mockApi['GET /admin/accounts'] = () => ({
      status: 200,
      body: {
        items: [
          { id: 1, username: 'root', display_name: '林', phone_masked: '137****0001', roles: ['admin'], status: 'active', must_change_password: false, created_at: '2026-10-01T00:00:00Z' },
          { id: 2, username: 'kefu', display_name: '小王', phone_masked: '137****0002', roles: ['support'], status: 'active', must_change_password: true, created_at: '2026-10-01T00:00:00Z' },
        ],
      },
    });
    g.mockApi['GET /admin/audit-logs'] = () => ({ status: 200, body: { items: [{ id: 1, admin_name: '林', action: 'POST /admin/accounts', created_at: '2026-10-03T02:00:00Z' }] } });
    wrap(<Roles />);
    expect(await screen.findByText('kefu')).toBeTruthy();
    expect(screen.getByText('待改初始密码')).toBeTruthy();
    expect(await screen.findByText('POST /admin/accounts')).toBeTruthy();
  });
});
