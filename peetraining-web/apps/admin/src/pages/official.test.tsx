import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { App as AntApp } from 'antd';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { ReactElement } from 'react';
import { MemoryRouter } from 'react-router';
import { beforeEach, describe, expect, it } from 'vitest';
import { Demand } from './Demand';
import { Produce } from './Produce';
import { Release } from './Release';
import { ReviewQueue } from './Review';

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

const project = {
  id: 3, school: '某某大学', major: '中国古代文学', subject_code: '654', subject_name: '中国语言文学基础', bank_id: 9, stage: 'producing', editor_ids: [5], materials: [],
  kp_count: 2, question_count: 1, exam_count: 0, version: '1.0', subscribers: 12, created_at: '2026-10-01T02:00:00Z',
};

beforeEach(() => {
  g.apiCalls.length = 0;
  g.mockApi = { 'GET /admin/official/projects': () => ({ status: 200, body: { items: [project] } }) };
});

const posted = (method: string, path: RegExp) => g.apiCalls.filter((r) => r.method === method && path.test(new URL(r.url).pathname));

describe('7.10 需求洞察', () => {
  beforeEach(() => {
    g.mockApi['GET /admin/demand'] = () => ({
      status: 200,
      body: {
        items: [
          { subject_code: '654', users: 40, avg_questions: 120, week_new: 6, with_target: 30, paid_rate: 0.2, avg_review: 15, unplanned: false },
          { subject_code: '811', users: 3, avg_questions: 40, week_new: 1, with_target: 1, paid_rate: 0, avg_review: 2, unplanned: true },
        ],
      },
    });
    g.mockApi['PUT /admin/demand/{code}'] = () => ({ status: 204 });
  });

  it('只显示统计；达到门槛的可以立项，内容负责人可标为未规划', async () => {
    wrap(<Demand roles={['content_lead']} />);
    expect(await screen.findByText('达到立项门槛')).toBeTruthy();
    expect(screen.getByText('未规划')).toBeTruthy();
    fireEvent.click(screen.getAllByRole('button', { name: /标为未规划/ })[0]!);
    await waitFor(() => expect(posted('PUT', /\/admin\/demand\/654$/)).toHaveLength(1));
  });

  it('数据分析只能看，不能立项', async () => {
    wrap(<Demand roles={['analyst']} />);
    expect(await screen.findByText('达到立项门槛')).toBeTruthy();
    expect(screen.queryByRole('button', { name: /立项做官方题库/ })).toBeNull();
  });
});

describe('7.12 内容生产', () => {
  beforeEach(() => {
    g.mockApi['GET /admin/official/projects/{id}/items'] = () => ({ status: 200, body: { items: [] } });
    g.mockApi['GET /admin/official/projects/{id}/drafts'] = () => ({ status: 200, body: { items: [] } });
    g.mockApi['POST /admin/official/projects/{id}/kp-candidates'] = () => ({
      status: 200,
      body: {
        items: [
          { section: '文学理论', chapter: '审美范畴', name: '形象思维', original_text: '借助具体形象进行的思维。', rubric: [{ content: '借助具体形象', score: 5 }], alignment: 'new' },
          { section: '文学理论', chapter: '审美范畴', name: '意境', original_text: '情景交融。', rubric: [], alignment: 'differ', existing_id: 8, existing_text: '虚实相生。' },
        ],
      },
    });
    g.mockApi['POST /admin/official/projects/{id}/drafts'] = () => ({ status: 200, body: {} });
  });

  it('AI 拆出的候选与现有知识点树对齐；新增的存为草稿', async () => {
    wrap(<Produce />);
    fireEvent.change(await screen.findByPlaceholderText('资料原文'), { target: { value: '形象思维：借助具体形象进行的思维。意境……' } });
    fireEvent.click(screen.getByRole('button', { name: /拆知识点/ }));
    expect(await screen.findByText('新增')).toBeTruthy();
    expect(screen.getByText('表述差异')).toBeTruthy();
    expect(screen.getByText('现有：虚实相生。')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /存为草稿/ }));
    fireEvent.click((await screen.findAllByRole('button', { name: /存\s*为\s*草\s*稿/ })).at(-1)!);
    await waitFor(() => expect(posted('POST', /\/drafts$/)).toHaveLength(1));
    const body = (await posted('POST', /\/drafts$/)[0]!.json()) as { change_type: string; payload: { name: string; rubric: unknown[] } };
    expect(body.change_type).toBe('add');
    expect(body.payload.name).toBe('形象思维');
    expect(body.payload.rubric).toHaveLength(1);
  });
});

describe('7.13 审核队列', () => {
  beforeEach(() => {
    g.mockApi['GET /admin/official/reviews'] = () => ({
      status: 200,
      body: {
        items: [
          {
            id: 21, draft_id: 4, project_id: 3, editor_id: 5, mode: 'full', entity_type: 'knowledge_point', entity_id: 8, change_type: 'revise',
            payload: { section: '文学理论', chapter: '审美范畴', name: '意境', original_text: '新表述', rubric: [{ content: '情景交融', score: 5 }] },
            before: { section: '文学理论', chapter: '审美范畴', name: '意境', original_text: '旧表述', rubric: [] }, created_at: '2026-10-02T02:00:00Z',
          },
        ],
      },
    });
    g.mockApi['POST /admin/official/reviews/{id}'] = () => ({ status: 204 });
  });

  it('修订左右对比；退回必须填原因', async () => {
    wrap(<ReviewQueue />);
    fireEvent.click(await screen.findByText('全量审核'));
    expect(await screen.findByText('修改前')).toBeTruthy();
    expect(await screen.findByText('旧表述')).toBeTruthy();
    expect(screen.getByText('新表述')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /退\s*回/ }));
    const ok = (await screen.findAllByRole('button', { name: /退\s*回/ })).at(-1)!;
    expect((ok as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(screen.getByPlaceholderText(/退回原因/), { target: { value: '表述没有出处' } });
    fireEvent.click((await screen.findAllByRole('button', { name: /退\s*回/ })).at(-1)!);
    await waitFor(() => expect(posted('POST', /\/reviews\/21$/)).toHaveLength(1));
    expect(await posted('POST', /\/reviews\/21$/)[0]!.json()).toEqual({ decision: 'rejected', reason: '表述没有出处' });
  });
});

describe('7.14 版本发布', () => {
  const versions = {
    items: [
      { id: 2, version: '1.1', changes: [], published_at: '2026-10-02T02:00:00Z' },
      { id: 1, version: '1.0', changes: [], published_at: '2026-10-01T02:00:00Z' },
    ],
  };
  beforeEach(() => {
    g.mockApi['GET /admin/official/projects/{id}/releases'] = () => ({ status: 200, body: versions });
  });

  it('检查没通过不能发布；只能回滚最新版本', async () => {
    g.mockApi['GET /admin/official/projects/{id}/release-preview'] = () => ({
      status: 200,
      body: {
        changes: [{ draft_id: 4, entity_type: 'knowledge_point', change_type: 'revise', entity_id: 8, title: '意境', rubric_changed: true }],
        checks: [{ name: '所有知识点都有采分点', passed: false, detail: '形象思维' }],
        next_version: '1.2', notice: '「中国语言文学基础」官方题库更新到 1.2 版', subscribers: 12,
      },
    });
    wrap(<Release />);
    expect(await screen.findByText(/采分点有变化/)).toBeTruthy();
    expect(screen.getByText('未通过')).toBeTruthy();
    expect((screen.getByRole('button', { name: /发\s*布/ }) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText('通知只发给已添加的 12 位用户')).toBeTruthy();
    expect(await screen.findAllByRole('button', { name: /回\s*滚/ })).toHaveLength(1);
  });

  it('检查通过后发布', async () => {
    g.mockApi['GET /admin/official/projects/{id}/release-preview'] = () => ({
      status: 200,
      body: { changes: [{ draft_id: 4, entity_type: 'question', change_type: 'add', entity_id: 0, title: '名词解释：意境', rubric_changed: false }],
        checks: [{ name: '有已审核的变更', passed: true }], next_version: '1.2', notice: '更新', subscribers: 12 },
    });
    g.mockApi['POST /admin/official/projects/{id}/releases'] = () => ({ status: 200, body: { id: 3, version: '1.2', changes: [], published_at: '2026-10-03T02:00:00Z' } });
    wrap(<Release />);
    fireEvent.click(await screen.findByRole('button', { name: /发\s*布/ }));
    fireEvent.click(await screen.findByRole('button', { name: /^(OK|确\s*定)$/ }));
    await waitFor(() => expect(posted('POST', /\/releases$/)).toHaveLength(1));
  });
});
