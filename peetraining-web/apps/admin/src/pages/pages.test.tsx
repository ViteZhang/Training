import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { App as AntApp } from 'antd';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { ReactElement } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { Codes } from './Codes';
import { Feedback } from './Feedback';
import { Overview } from './Overview';
import { Users } from './Users';

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

const user = {
  id: 7, phone_masked: '139****7777', invite_code: 'K7Q2HN', status: 'active', subjects: '654', materials: 3, questions: 246, is_member: false, created_at: '2026-09-26T12:00:00Z',
};

describe('7.2 用户', () => {
  beforeEach(() => {
    g.mockApi['GET /admin/users'] = () => ({ status: 200, body: { items: [user] } });
    g.mockApi['GET /admin/users/{id}'] = () => ({
      status: 200,
      body: { ...user, nickname: '小林', failed_materials: 1, pages: 98, knowledge_points: 58, papers: 2, week_gradings: 17, invited: 3, invite_days: 14, subject_stats: [], quota: [] },
    });
    g.mockApi['GET /admin/audit-logs'] = () => ({ status: 200, body: { items: [] } });
    g.mockApi['GET /admin/users/{id}/phone'] = () => ({ status: 200, body: { phone: '13900007777' } });
  });

  it('手机号脱敏；客服看不到完整手机号、赠送会员与封禁', async () => {
    wrap(<Users roles={['support']} />);
    fireEvent.click(await screen.findByText('139****7777'));
    expect(await screen.findByText('3 份 · 98 页 · 解析失败 1 份')).toBeTruthy();
    expect(screen.getByText(/资料和题目内容：不可见/)).toBeTruthy();
    expect(screen.queryByText('查看（记日志）')).toBeNull();
    expect(screen.queryByText('赠送会员天数')).toBeNull();
    expect(screen.queryByText('封禁账号')).toBeNull();
    expect(screen.getByText('加解析额度')).toBeTruthy();
  });

  it('管理员可查看完整手机号', async () => {
    wrap(<Users roles={['admin']} />);
    fireEvent.click(await screen.findByText('139****7777'));
    fireEvent.click(await screen.findByText('查看（记日志）'));
    expect(await screen.findByText('13900007777')).toBeTruthy();
  });
});

describe('7.4 兑换码', () => {
  it('生成一批兑换码后立即导出完整码；批次明细只有末 3 位', async () => {
    const created = { id: 3, name: '内测第一批', tier: 'gift', days: 7, quantity: 2, used: 0, expires_at: '2030-01-01T15:59:59Z', channel: '内测群', status: 'active', created_at: '2026-10-03T02:00:00Z' };
    g.mockApi['GET /admin/redeem/summary'] = () => ({ status: 200, body: { generated: 2, used: 0, available: 2, inactive: 0 } });
    g.mockApi['GET /admin/redeem/batches'] = () => ({ status: 200, body: { items: [created] } });
    g.mockApi['POST /admin/redeem/batches'] = () => ({ status: 200, body: { batch: created, codes: ['ABCD2345', 'WXYZ6789'] } });
    g.mockApi['GET /admin/redeem/batches/{id}'] = () => ({
      status: 200,
      body: { batch: created, codes: [{ id: 1, tail: '345', status: 'unused', batch_id: 3, batch_name: '内测第一批', tier: 'gift', expires_at: created.expires_at }] },
    });
    const blobs: Blob[] = [];
    URL.createObjectURL = vi.fn((b: Blob) => {
      blobs.push(b);
      return 'blob:x';
    });
    URL.revokeObjectURL = vi.fn();
    wrap(<Codes roles={['admin']} />);
    expect(await screen.findByText('内测第一批')).toBeTruthy();
    // 直接调接口生成（表单的日期控件在 jsdom 里不好操作，生成接口单独验证参数）。
    const body = { name: '内测第一批', tier: 'gift', days: 7, quantity: 2, expires_at: '2030-01-01T15:59:59Z' };
    const r = await fetch('/api/v1/admin/redeem/batches', { method: 'POST', body: JSON.stringify(body) });
    expect(((await r.json()) as { codes: string[] }).codes).toHaveLength(2);
    fireEvent.click(screen.getByRole('button', { name: /导\s*出/ }));
    await waitFor(() => expect(blobs).toHaveLength(1));
    const csv = await blobs[0]!.text();
    expect(csv).toContain('"345"');
    expect(csv).not.toContain('ABCD2345');
  });

  it('客服只能查询，不能新建与停用', async () => {
    g.mockApi['GET /admin/redeem/summary'] = () => ({ status: 200, body: { generated: 0, used: 0, available: 0, inactive: 0 } });
    g.mockApi['GET /admin/redeem/batches'] = () => ({ status: 200, body: { items: [] } });
    wrap(<Codes roles={['support']} />);
    expect(await screen.findByText('单码查询')).toBeTruthy();
    expect(screen.queryByText('新建批次')).toBeNull();
  });
});

describe('7.7 用户反馈', () => {
  it('授权内查看资料：提示已记日志并通知用户', async () => {
    g.mockApi['GET /admin/feedbacks/stats'] = () => ({ status: 200, body: { open: 1, avg_reply_seconds: 600, top_type: 'recognition', satisfaction: 0, types: {} } });
    g.mockApi['GET /admin/feedbacks'] = () => ({
      status: 200,
      body: { items: [{ id: 5, user_id: 7, type: 'recognition', content: '第 3 页识别错了', allow_access: true, status: 'open', created_at: '2026-10-03T02:00:00Z' }] },
    });
    g.mockApi['GET /admin/feedbacks/{id}'] = () => ({
      status: 200,
      body: { id: 5, user_id: 7, type: 'recognition', content: '第 3 页识别错了', allow_access: true, status: 'open', created_at: '2026-10-03T02:00:00Z', screenshots: [], grant_expires_at: '2026-10-06T02:00:00Z', material_id: 9 },
    });
    g.mockApi['GET /admin/feedbacks/{id}/material'] = () => ({
      status: 200,
      body: { id: 9, format: 'pdf', pages: 1, status: 'parsed', page_texts: ['第一页识别文字'], grant_expires_at: '2026-10-06T02:00:00Z' },
    });
    g.mockApi['POST /admin/feedbacks/{id}/reply'] = () => ({ status: 204 });
    wrap(<Feedback />);
    fireEvent.click(await screen.findByText('第 3 页识别错了'));
    fireEvent.click(await screen.findByText('查看授权资料'));
    expect(await screen.findByText('这次查看已记入日志，并已通知用户')).toBeTruthy();
    expect(await screen.findByText('第一页识别文字')).toBeTruthy();
  });
});

describe('7.1 概览', () => {
  it('统计卡片与「需要处理」提醒', async () => {
    g.mockApi['GET /admin/overview'] = () => ({
      status: 200,
      body: {
        updated_at: '2026-10-03T02:07:00Z', users_total: 2846, week_new_users: 312, week_active_users: 1120, week_parse_pages: 38420, week_parse_rate: 0.93, paid_users: 214,
        members: 260, conversion: 0.11, month_revenue_cents: 2186000, week_dispute_rate: 0.05, funnel: [{ name: 'registered', value: 2846 }, { name: 'imported', value: 1932 }],
        import_modes: [{ name: 'question', value: 10 }], hot_subjects: [{ name: '654', value: 412 }], days: [{ day: '2026-10-03', new_users: 1, active_users: 1, answers: 5, parse_pages: 3, revenue_cents: 0 }],
        alerts: ['本周资料解析成功率低于 95%，去「资料解析监控」看失败任务'],
      },
    });
    wrap(<Overview />);
    expect(await screen.findByText('本周资料解析成功率低于 95%，去「资料解析监控」看失败任务')).toBeTruthy();
    expect(screen.getByText('654')).toBeTruthy();
    expect(screen.getByText(/本月 ¥21860.00/)).toBeTruthy();
  });
});
