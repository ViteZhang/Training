import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { App as AntApp } from 'antd';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { ReactElement } from 'react';
import { MemoryRouter } from 'react-router';
import { beforeEach, describe, expect, it } from 'vitest';
import { visibleMenu } from '../lib/menu';
import { Applications, hoursAgo, toCSV } from './Applications';
import { Catalog, splitBooks } from './Catalog';
import { formatOf } from './OfficialImport';
import { rubricSum, Samples } from './Samples';

type Handler = (req: Request) => { status: number; body?: unknown } | Promise<{ status: number; body?: unknown }>;
const g = globalThis as unknown as { mockApi: Record<string, Handler>; apiCalls: Request[] };

function wrap(ui: ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <AntApp>{ui}</AntApp>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}
const calls = (method: string, path: RegExp) => g.apiCalls.filter((r) => r.method === method && path.test(new URL(r.url).pathname));

const group = (over: object = {}) => ({
  id: 4, school: '四川大学', major: '哲学', subject_name: '西方哲学史', subject_code: '812', discipline: '哲学', status: 'suggested', applicant_count: 3,
  paid_count: 1, seed_count: 1, pending_since: new Date(Date.now() - 31 * 3_600_000).toISOString(), overdue: true, last_applied_at: '2026-11-02T02:00:00Z', ...over,
});

beforeEach(() => {
  g.apiCalls.length = 0;
  g.mockApi = {
    'GET /admin/course-applications/summary': () => ({ status: 200, body: { pending: 2, suggested: 1, overdue: 1, week_new: 12, school_majors: 9 } }),
    'GET /admin/course-applications': () => ({ status: 200, body: { items: [group(), group({ id: 5, school: '南京大学', status: 'rejected', reason: '真题太少', overdue: false, pending_since: undefined })] } }),
    'GET /admin/course-applications/{id}/applicants': () => ({
      status: 200,
      body: { items: [{ nickname: '研友A', exam_year: 2027, entry: 'onboarding', applied_at: '2026-11-02T00:00:00Z', paid: false, seed: true }] },
    }),
    'POST /admin/course-applications/{id}/reject': () => ({ status: 204 }),
  };
});

describe('菜单（PRD v3 增补）', () => {
  it('官方题库分组按增补的顺序', () => {
    const official = visibleMenu(['admin']).find((m) => m.key === 'official')!;
    expect(official.pages.map((p) => p.title)).toEqual(['申请收录', '需求洞察', '官方题库', 'AI 解析导入', '内容生产', '审核队列', '版本发布', '通用体验题']);
    expect(visibleMenu(['admin']).find((m) => m.key === 'config')!.pages.map((p) => p.title)).toContain('院校专业课库');
  });
});

describe('7.16 申请收录', () => {
  it('提醒条、统计与列表；超 24 小时标红', async () => {
    wrap(<Applications roles={['content_lead']} />);
    expect(await screen.findByText(/1 门课超过 24 小时未处理，1 门课达到立项阈值/)).toBeTruthy();
    expect(screen.getByText('四川大学 · 哲学 · 812 西方哲学史')).toBeTruthy();
    expect(screen.getByText('建议立项')).toBeTruthy();
    expect(screen.getByText('暂不收录')).toBeTruthy();
    expect(screen.getByText('31 小时前')).toBeTruthy();
  });

  it('暂不收录必须填原因，提交后调接口', async () => {
    wrap(<Applications roles={['content_lead']} />);
    fireEvent.click((await screen.findAllByRole('button', { name: /^处\s?理$/ }))[0]!);
    expect(await screen.findByText(/用户 研友A/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '暂不收录' }));
    fireEvent.click(await screen.findByRole('button', { name: /OK|确 定|确定/ }));
    expect(await screen.findByText('暂不收录要填原因')).toBeTruthy();
    fireEvent.change(screen.getByLabelText('原因（会通知申请人）'), { target: { value: '暂时没有足够的公开真题' } });
    fireEvent.click(screen.getByRole('button', { name: /OK|确 定|确定/ }));
    await waitFor(() => expect(calls('POST', /\/course-applications\/4\/reject$/)).toHaveLength(1));
  });

  it('数据分析只看不处理', async () => {
    wrap(<Applications roles={['analyst']} />);
    fireEvent.click((await screen.findAllByRole('button', { name: /^处\s?理$/ }))[0]!);
    expect(await screen.findByText(/用户 研友A/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: '立项，进入官方题库' })).toBeNull();
  });

  it('导出 CSV 与时间显示', () => {
    const csv = toCSV([group() as never]);
    expect(csv.startsWith('﻿')).toBe(true);
    expect(csv).toContain('"四川大学","哲学","西方哲学史","812"');
    expect(hoursAgo(undefined)).toBe('—');
    expect(hoursAgo(new Date(Date.now() - 10 * 60_000).toISOString())).toBe('1 小时内');
  });
});

describe('7.18 通用体验题', () => {
  beforeEach(() => {
    g.mockApi['GET /admin/samples'] = () => ({
      status: 200,
      body: { disciplines: 2, directions: 8, questions: 3, pending: 1, week_tries: 5, items: [{ id: 3, discipline: '哲学', name: '西方哲学', question_count: 2 }] },
    });
    g.mockApi['GET /admin/samples/questions'] = () => ({
      status: 200,
      body: {
        items: [
          { id: 11, direction_id: 3, origin: 'manual', textbook: '张志伟《西方哲学史》', qtype: 'term', score: 5, stem: '绝对命令', answer: '康德', rubric: [{ content: '康德', score: 5 }], status: 'online', try_count: 7, created_at: '2026-11-01T00:00:00Z' },
          { id: 12, direction_id: 3, origin: 'ai_batch', qtype: 'term', score: 5, stem: '先验统觉', answer: '自我意识', rubric: [{ content: '统一', score: 5 }], status: 'pending', try_count: 0, created_at: '2026-11-01T00:00:00Z' },
        ],
      },
    });
    g.mockApi['POST /admin/samples/questions/{id}/status'] = () => ({ status: 204 });
  });

  it('列出题目与状态；内容负责人审核上线', async () => {
    wrap(<Samples roles={['content_lead']} />);
    expect(await screen.findByText('绝对命令')).toBeTruthy();
    expect(screen.getByText('待审核')).toBeTruthy();
    expect(screen.getByText('AI 出题')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '审核通过' }));
    await waitFor(() => expect(calls('POST', /\/samples\/questions\/12\/status$/)).toHaveLength(1));
  });

  it('内容编辑不能审核', async () => {
    wrap(<Samples roles={['content_editor']} />);
    expect(await screen.findByText('先验统觉')).toBeTruthy();
    expect(screen.queryByRole('button', { name: '审核通过' })).toBeNull();
  });

  it('采分点合计', () => {
    expect(rubricSum([{ score: 2 }, { score: 3 }, {}])).toBe(5);
  });
});

describe('院校专业课库', () => {
  beforeEach(() => {
    g.mockApi['GET /admin/catalog/courses'] = () => ({
      status: 200,
      body: { items: [{ id: 1, school: '四川大学', major: '哲学', discipline: '哲学', subject_name: '西方哲学史', subject_code: '812', textbooks: ['张志伟《西方哲学史》'], official_status: 'producing', status: 'active', chosen_count: 4 }] },
    });
    g.mockApi['GET /admin/catalog/pending'] = () => ({
      status: 200,
      body: { items: [{ id: 9, school: '南京大学', major: '中国语言文学', subject_name: '文学理论', subject_code: '831', request_count: 2, first_at: '2026-11-01T00:00:00Z', last_at: '2026-11-02T00:00:00Z' }] },
    });
    g.mockApi['POST /admin/catalog/pending/{id}/resolve'] = () => ({ status: 200, body: { course_id: 2 } });
  });

  it('课程带官方题库状态；待入库可以忽略', async () => {
    wrap(<Catalog />);
    expect(await screen.findByText('812 西方哲学史')).toBeTruthy();
    expect(screen.getByText('生产中')).toBeTruthy();
    fireEvent.click(screen.getByRole('tab', { name: /待入库/ }));
    expect(await screen.findByText('831 文学理论')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /^忽\s?略$/ }));
    await waitFor(() => expect(calls('POST', /\/catalog\/pending\/9\/resolve$/)).toHaveLength(1));
  });

  it('参考书目一行一本', () => {
    expect(splitBooks('a\n\n b ')).toEqual(['a', 'b']);
  });
});

describe('7.17 AI 解析导入', () => {
  it('按扩展名判断格式', () => {
    expect(formatOf('2024真题.PDF')).toBe('pdf');
    expect(formatOf('讲义.docx')).toBe('docx');
    expect(formatOf('回忆版.jpg')).toBe('image');
    expect(formatOf('题目.csv')).toBe('csv');
    expect(formatOf('旧版.doc')).toBeUndefined();
  });
});
