import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { App as AntApp } from 'antd';
import { fireEvent, render, screen } from '@testing-library/react';
import type { ReactElement } from 'react';
import { beforeEach, describe, expect, it } from 'vitest';
import { Activation } from './Activation';

type Handler = (req: Request) => { status: number; body?: unknown };
const g = globalThis as unknown as { mockApi: Record<string, Handler>; apiCalls: Request[] };

function wrap(ui: ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <AntApp>{ui}</AntApp>
    </QueryClientProvider>,
  );
}

const step = (key: string, label: string, reached: number, stuck: number, median_hours?: number) => ({ key, label, hint: label, reached, stuck, median_hours });

beforeEach(() => {
  g.apiCalls.length = 0;
  g.mockApi = {
    'GET /admin/activation': () => ({
      status: 200,
      body: {
        from: '2026-10-14', to: '2026-11-12', total: 10,
        steps: [
          step('registered', '注册', 10, 3), step('onboarded', '完成引导', 7, 2), step('has_bank', '有题库', 5, 1, 2.5),
          step('first_answer', '首次作答', 4, 1, 30), step('first_grading', '看到批改', 3, 1, 72), step('return_day', '隔天再练', 2, 1, 40),
          step('week_later', '7 日后仍在练', 1, 1, 200), step('paid', '付费', 0, 0),
        ],
      },
    }),
    'GET /admin/activation/users': () => ({
      status: 200,
      body: { items: [{ user_id: 42, phone_masked: '139****0042', created_at: '2026-11-01T02:00:00Z', current: 'registered', milestones: [] }] },
    }),
  };
});

describe('7.20 激活漏斗', () => {
  it('各级人数、占比、较上一级与用时中位数；点「停在这里」列出用户', async () => {
    wrap(<Activation roles={['support']} />);
    expect(await screen.findByText('2026-10-14 至 2026-11-12 注册')).toBeTruthy();
    expect(screen.getByText('2.5 小时')).toBeTruthy();
    expect(screen.getByText('8.3 天')).toBeTruthy();
    expect(screen.getByText('70.0%')).toBeTruthy(); // 完成引导 / 注册
    fireEvent.click(screen.getByText('3 人 · 30.0%'));
    expect(await screen.findByText('停在「注册」的用户')).toBeTruthy();
    expect(await screen.findByText('139****0042')).toBeTruthy();
    const call = g.apiCalls.find((r) => r.url.includes('/admin/activation/users'));
    expect(call?.url).toContain('stage=registered');
  });

  it('数据分析只看汇总，不能点进具体用户', async () => {
    wrap(<Activation roles={['analyst']} />);
    expect(await screen.findByText('3 人 · 30.0%')).toBeTruthy();
    expect(screen.queryByRole('button', { name: '3 人 · 30.0%' })).toBeNull();
  });
});
