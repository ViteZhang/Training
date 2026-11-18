import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { App as AntApp } from 'antd';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { ReactElement } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { Codes } from './Codes';
import { Feedback } from './Feedback';
import { Overview } from './Overview';
import { Parse } from './Parse';
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

const journey = {
  user_id: 7, phone_masked: '139****7777', created_at: '2026-09-26T12:00:00Z', current: 'first_answer', active_days_30: 2, answers_30: 9,
  days: [{ day: '2026-09-27', answers: 5 }],
  milestones: [
    { key: 'registered', label: '注册', reached: true, at: '2026-09-26T12:00:00Z' },
    { key: 'onboarded', label: '完成引导', reached: true },
    { key: 'has_bank', label: '有题库', reached: true, at: '2026-09-26T12:30:00Z' },
    { key: 'first_answer', label: '首次作答', reached: true, at: '2026-09-27T01:00:00Z' },
    { key: 'first_grading', label: '看到批改', reached: false },
    { key: 'return_day', label: '隔天再练', reached: false },
    { key: 'week_later', label: '7 日后仍在练', reached: false },
    { key: 'paid', label: '付费', reached: false },
  ],
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
    g.mockApi['GET /admin/users/{id}/journey'] = () => ({ status: 200, body: journey });
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

  it('使用进度：各级达成时间（北京时间），没有时间记录的标明', async () => {
    wrap(<Users roles={['support']} />);
    fireEvent.click(await screen.findByText('139****7777'));
    expect(await screen.findByText('2026-09-27 09:00')).toBeTruthy();
    expect(screen.getByText('已完成（无时间记录）')).toBeTruthy();
    expect(screen.getAllByText('未达成')).toHaveLength(4);
    expect(screen.getAllByText('首次作答').length).toBeGreaterThan(0);
  });

  it('管理员可查看完整手机号', async () => {
    wrap(<Users roles={['admin']} />);
    fireEvent.click(await screen.findByText('139****7777'));
    fireEvent.click(await screen.findByText('查看（记日志）'));
    expect(await screen.findByText('13900007777')).toBeTruthy();
  });
});

describe('7.4 兑换码', () => {
  const created = { id: 3, name: '内测第一批', tier: 'gift', days: 7, quantity: 4, used: 1, expires_at: '2030-01-01T15:59:59Z', channel: '内测群', status: 'active', created_at: '2026-10-03T02:00:00Z' };
  const base = { batch_id: 3, batch_name: '内测第一批', tier: 'gift', days: 7, expires_at: created.expires_at };
  const codes = [
    { ...base, id: 1, tail: '345', code: 'ABCD2345', status: 'unused' },
    { ...base, id: 2, tail: '789', code: 'WXYZ6789', status: 'unused', note: '小红书 @阿青', noted_by_name: '客服小王', noted_at: '2026-11-10T02:00:00Z' },
    { ...base, id: 3, tail: 'K7Q', code: 'HJKMNK7Q', status: 'used', used_by: 42, used_at: '2026-11-11T02:00:00Z', note: '考研群 王同学' },
    { ...base, id: 4, tail: 'PQR', status: 'unused' },
  ];
  beforeEach(() => {
    g.mockApi['GET /admin/redeem/summary'] = () => ({ status: 200, body: { generated: 4, used: 1, available: 3, inactive: 0 } });
    g.mockApi['GET /admin/redeem/batches'] = () => ({ status: 200, body: { items: [created] } });
    g.mockApi['GET /admin/redeem/batches/{id}'] = () => ({ status: 200, body: { batch: created, codes } });
    g.mockApi['PUT /admin/redeem/codes/{id}/note'] = () => ({ status: 204 });
    g.mockApi['POST /admin/redeem/batches/{id}/plain-codes'] = () => ({ status: 200, body: { filled: 1, already: 0, unmatched: 1 } });
  });

  it('导出带明文、发放状态与备注；没补录的旧码只有末 3 位', async () => {
    const blobs: Blob[] = [];
    URL.createObjectURL = vi.fn((b: Blob) => {
      blobs.push(b);
      return 'blob:x';
    });
    URL.revokeObjectURL = vi.fn();
    wrap(<Codes roles={['admin']} />);
    expect(await screen.findByText('内测第一批')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /导\s*出/ }));
    await waitFor(() => expect(blobs).toHaveLength(1));
    const csv = await blobs[0]!.text();
    expect(csv).toContain('"ABCD2345","未发放"');
    expect(csv).toContain('"WXYZ6789","已发放未激活","小红书 @阿青","客服小王"');
    expect(csv).toContain('"HJKMNK7Q","已激活"');
    expect(csv).toContain('"*****PQR"');
  });

  it('明细：按发放状态筛选；复制一个未发放的码后要求填备注', async () => {
    const writeText = vi.fn(() => Promise.resolve());
    Object.assign(navigator, { clipboard: { writeText } });
    wrap(<Codes roles={['support']} />);
    fireEvent.click(await screen.findByRole('button', { name: /明\s*细/ }));
    expect(await screen.findByText('ABCD2345')).toBeTruthy();
    expect(screen.getByText('*****PQR')).toBeTruthy();
    expect(screen.getByText(/有 1 个旧码库里没有明文/)).toBeTruthy();
    fireEvent.click(screen.getByText('已发放未激活 1'));
    expect(await screen.findByText('小红书 @阿青')).toBeTruthy();
    expect(screen.queryByText('ABCD2345')).toBeNull();
    fireEvent.click(screen.getByText('复制一个未发放的码'));
    expect(await screen.findByText('已复制兑换码。发给谁了？记一下，避免重复发放。')).toBeTruthy();
    expect(writeText).toHaveBeenCalledWith('ABCD2345');
    fireEvent.change(screen.getByPlaceholderText('渠道 + 对方昵称或手机号后 4 位'), { target: { value: ' 小红书 @小林 ' } });
    fireEvent.click(screen.getByRole('button', { name: /保\s*存/ }));
    await waitFor(() => expect(g.apiCalls.some((r) => r.method === 'PUT' && r.url.endsWith('/admin/redeem/codes/1/note'))).toBe(true));
    const put = g.apiCalls.find((r) => r.method === 'PUT')!;
    expect(await put.clone().json()).toEqual({ note: '小红书 @小林' });
    expect(screen.queryByText('作废')).toBeNull();
  });

  it('补录明文：从粘贴的 CSV 里取出兑换码', async () => {
    wrap(<Codes roles={['admin']} />);
    fireEvent.click(await screen.findByRole('button', { name: /明\s*细/ }));
    fireEvent.click(await screen.findByText('补录明文（1 个缺）'));
    fireEvent.change(screen.getByPlaceholderText(/ABCD2345/), { target: { value: '兑换码,档位\nmnpq2pqr,7 天\nZZZZ2222,7 天\nMNPQ2PQR,7 天' } });
    fireEvent.click(screen.getByRole('button', { name: '补录 2 个码' }));
    await waitFor(() => expect(g.apiCalls.some((r) => r.url.endsWith('/plain-codes'))).toBe(true));
    const call = g.apiCalls.find((r) => r.url.endsWith('/plain-codes'))!;
    expect(await call.clone().json()).toEqual({ codes: ['MNPQ2PQR', 'ZZZZ2222'] });
  });

  it('客服能查找（按码或备注），不能新建与停用', async () => {
    g.mockApi['GET /admin/redeem/codes/search'] = () => ({ status: 200, body: { items: [codes[1]] } });
    wrap(<Codes roles={['support']} />);
    expect(await screen.findByText('查找兑换码')).toBeTruthy();
    expect(screen.queryByText('新建批次')).toBeNull();
    fireEvent.change(screen.getByPlaceholderText(/输入兑换码，或备注里的名字/), { target: { value: '阿青' } });
    fireEvent.click(screen.getByRole('button', { name: /查\s*找/ }));
    expect(await screen.findByText('小红书 @阿青')).toBeTruthy();
    expect(g.apiCalls.find((r) => r.url.includes('/redeem/codes/search'))?.url).toContain('q=%E9%98%BF%E9%9D%92');
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

describe('7.5 资料解析监控', () => {
  it('缺答案统计：占比、匹配补上比例、AI 参考答案确认率与修改率', async () => {
    g.mockApi['GET /admin/parse/stats'] = () => ({
      status: 200,
      body: { success_rate: 0.96, pages: 100, files: 10, avg_seconds: 20, p95_seconds: 60, formats: [],
        answers: { imported_questions: 200, missing_original: 50, matched: 20, ai_answers: 10, ai_confirmed: 6, ai_edited: 2 } },
    });
    g.mockApi['GET /admin/parse/jobs'] = () => ({ status: 200, body: { items: [] } });
    wrap(<Parse />);
    expect(await screen.findByText('导入题目缺答案')).toBeTruthy();
    expect(screen.getByText('缺答案占比')).toBeTruthy();
    expect(screen.getByText('50 / 200 题')).toBeTruthy();
    expect(screen.getByText('AI 参考答案确认率')).toBeTruthy();
    expect(screen.getByText('共 10 份')).toBeTruthy();
  });
});
