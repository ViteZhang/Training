import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { App as AntApp } from 'antd';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { ReactElement } from 'react';
import { beforeEach, describe, expect, it } from 'vitest';
import { flatten, setPath } from '../components/ParamEditor';
import { ManualIssue } from './Codes';
import { ExamDatesPanel, ParamsPanel, PurchaseEntryPanel } from './Config';
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

describe('7.8 初试日期', () => {
  it('点「修改」带出原日期，保存覆盖同一年', async () => {
    let savedUrl = '';
    let saved: unknown;
    g.mockApi['GET /admin/config/exam-dates'] = () => ({
      status: 200,
      body: { items: [{ year: 2027, label: '2027 研考', first_exam_start: '2026-12-19', first_exam_end: '2026-12-21', subject_exam_date: '2026-12-20' }] },
    });
    g.mockApi['PUT /admin/config/exam-dates/{year}'] = async (req) => {
      savedUrl = req.url;
      saved = await req.json();
      return { status: 204 };
    };
    wrap(<ExamDatesPanel />);
    fireEvent.click(await screen.findByRole('button', { name: /修\s*改/ }));
    expect(screen.getByText('修改 2027 年')).toBeTruthy();
    expect((screen.getByLabelText('年份') as HTMLInputElement).disabled).toBe(true);
    fireEvent.change(screen.getByLabelText('名称'), { target: { value: '2027 年研考' } });
    fireEvent.click(screen.getByRole('button', { name: /保\s*存/ }));
    await waitFor(() =>
      expect(saved).toEqual({ label: '2027 年研考', first_exam_start: '2026-12-19', first_exam_end: '2026-12-21', subject_exam_date: '2026-12-20' }),
    );
    expect(savedUrl).toMatch(/\/admin\/config\/exam-dates\/2027$/);
    await waitFor(() => expect(screen.getByText('新增某年')).toBeTruthy());
  });
});

describe('7.8 购买入口', () => {
  it('iOS 默认关闭；打开并保存时带版本号', async () => {
    let saved: unknown;
    const entry = { android: true, ios: false, version: 3, wecom: { name: '考研Training 客服', wecom_id: 'kaoyan-training', reply_time: '工作日', note: '加客服', qr_uploaded: false } };
    g.mockApi['GET /admin/config/purchase-entry'] = () => ({ status: 200, body: entry });
    g.mockApi['PUT /admin/config/purchase-entry'] = async (req) => {
      saved = await req.json();
      return { status: 200, body: { ...entry, ios: true, version: 4 } };
    };
    wrap(<PurchaseEntryPanel />);
    expect(await screen.findByText('iOS · 显示购买入口')).toBeTruthy();
    expect(screen.getByText('还没上传')).toBeTruthy();
    const switches = screen.getAllByRole('switch');
    const [androidSwitch, iosSwitch] = switches as [HTMLElement, HTMLElement];
    expect(androidSwitch.getAttribute('aria-checked')).toBe('true');
    expect(iosSwitch.getAttribute('aria-checked')).toBe('false');
    fireEvent.click(iosSwitch);
    fireEvent.click(screen.getByRole('button', { name: /保\s*存/ }));
    await waitFor(() =>
      expect(saved).toEqual({ android: true, ios: true, version: 3, wecom: { name: '考研Training 客服', wecom_id: 'kaoyan-training', reply_time: '工作日', note: '加客服' } }),
    );
  });
});

describe('7.4 单个发码 · 手动收款', () => {
  it('填手机号、档位、金额、收款方式，生成后显示完整兑换码', async () => {
    let sent: unknown;
    g.mockApi['POST /admin/redeem/single-codes'] = async (req) => {
      sent = await req.json();
      return { status: 200, body: { code: 'K7Q2HNXA', order_no: 'W20261026120000123456', batch_id: 9 } };
    };
    wrap(<ManualIssue onIssued={() => {}} />);
    fireEvent.change(screen.getByPlaceholderText('186 2210 7731'), { target: { value: '186 2210 7731' } });
    fireEvent.change(screen.getByRole('spinbutton'), { target: { value: '169' } });
    fireEvent.change(screen.getByPlaceholderText('小红书私信来的'), { target: { value: '小红书私信来的' } });
    fireEvent.click(screen.getByRole('button', { name: '生成并复制兑换码' }));
    expect(await screen.findByText('K7Q2HNXA')).toBeTruthy();
    expect(sent).toEqual({ phone: '18622107731', tier: 'season', amount_cents: 16900, pay_method: '企业微信转账', remark: '小红书私信来的' });
  });
});
