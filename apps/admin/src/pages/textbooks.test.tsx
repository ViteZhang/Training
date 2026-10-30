import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { App as AntApp } from 'antd';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';
import { visibleMenu } from '../lib/menu';
import { Textbooks } from './Textbooks';

type Handler = (req: Request) => { status: number; body?: unknown } | Promise<{ status: number; body?: unknown }>;
const g = globalThis as unknown as { mockApi: Record<string, Handler>; apiCalls: Request[] };
const time = '2026-10-30T00:00:00Z';
const revision = { id: 30, edition_id: 20, revision_no: 1, version: 1, status: 'draft', content_hash: 'abc', index_status: 'missing', created_at: time, updated_at: time, sections: [{ heading: '第一章', text: '文学活动的四要素。' }] };
const book = { id: 10, title: '文学理论教程', author: '童庆炳', kind: 'textbook', discipline: '文学', aliases: ['文论教程'], version: 1, created_at: time, updated_at: time, editions: [{ id: 20, book_id: 10, edition: '第五版', publisher: '', isbn: '', license_note: '出版社授权', authorized: true, scope_note: '', status: 'unpublished', revisions: [revision] }] };
function mount() { const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } }); return render(<QueryClientProvider client={qc}><AntApp><Textbooks /></AntApp></QueryClientProvider>); }
const calls = (method: string, tail: string) => g.apiCalls.filter((r) => r.method === method && new URL(r.url).pathname.endsWith(tail));
beforeEach(() => { g.apiCalls.length = 0; g.mockApi = {
  'GET /admin/textbooks': () => ({ status: 200, body: { items: [book], total: 1, page: 1, page_size: 20 } }),
  'GET /admin/textbooks/{id}': () => ({ status: 200, body: book }),
  'GET /admin/textbooks/{id}/editions/{id}/revisions/{id}': () => ({ status: 200, body: revision }),
}; });
describe('7.16 独立官方教材库', () => {
  it('与题库菜单分开，只向管理员和内容负责人展示', () => {
    expect(visibleMenu(['content_lead']).find((m) => m.key === 'textbook')?.pages[0]?.path).toBe('/textbooks');
    for (const role of ['support', 'analyst', 'content_editor'] as const) expect(visibleMenu([role]).some((m) => m.key === 'textbook')).toBe(false);
    expect(visibleMenu(['admin']).some((m) => m.key === 'official')).toBe(true);
  });
  it('按书名查询和重置，目录显示版次与草稿状态', async () => {
    mount(); expect(await screen.findByText('文学理论教程')).toBeTruthy(); expect(screen.getByText(/最新 r1/)).toBeTruthy();
    const input = screen.getByRole('searchbox', { name: '搜索教材' }); fireEvent.change(input, { target: { value: '文论' } }); fireEvent.keyDown(input, { key: 'Enter', code: 'Enter', charCode: 13 });
    await waitFor(() => expect(calls('GET', '/admin/textbooks').some((r) => new URL(r.url).searchParams.get('q') === '文论')).toBe(true));
    fireEvent.click(screen.getByRole('button', { name: /重\s*置/ })); expect((input as HTMLInputElement).value).toBe('');
  });
  it('创建先校验授权，网络失败重试复用同一个请求标识', async () => {
    let attempts = 0; g.mockApi['POST /admin/textbooks'] = () => ++attempts === 1 ? { status: 503, body: { code: 'INTERNAL', message: '暂时失败' } } : { status: 200, body: book };
    mount(); fireEvent.click(await screen.findByRole('button', { name: /新建教材/ })); const modal = within(await screen.findByRole('dialog'));
    for (const [label, value] of [['标准书名', '文学理论教程'], ['作者 / 编者', '童庆炳'], ['适用学科', '文学'], ['出版 / 讲义版本', '第五版'], ['授权说明', '出版社授权']]) fireEvent.change(modal.getByLabelText(label!), { target: { value } });
    fireEvent.click(modal.getByRole('button', { name: /保\s*存/ })); expect(await modal.findByText('请确认素材使用授权')).toBeTruthy(); expect(calls('POST', '/admin/textbooks')).toHaveLength(0);
    fireEvent.click(modal.getByLabelText('确认可用于应用内检索及相关摘录展示')); fireEvent.click(modal.getByRole('button', { name: /保\s*存/ })); expect(await modal.findByText('暂时失败')).toBeTruthy();
    fireEvent.click(modal.getByRole('button', { name: /保\s*存/ })); await waitFor(() => expect(calls('POST', '/admin/textbooks')).toHaveLength(2));
    const bodies = await Promise.all(calls('POST', '/admin/textbooks').map((r) => r.json())); expect(bodies[0].idempotency_key).toBe(bodies[1].idempotency_key); expect(bodies[0]).toMatchObject({ title: '文学理论教程', initial_edition: { authorized: true, edition: '第五版' } });
  });
  it('冲突保留原文，不将失败保存显示为成功或发布', async () => {
    g.mockApi['PUT /admin/textbooks/{id}/editions/{id}/revisions/{id}'] = () => ({ status: 409, body: { code: 'CONFLICT', message: '内容已被更新，请刷新后重试' } });
    mount(); fireEvent.click(await screen.findByRole('button', { name: /管\s*理/ })); fireEvent.click(await screen.findByRole('button', { name: /校对原文/ }));
    const text = await screen.findByDisplayValue('文学活动的四要素。'); fireEvent.change(text, { target: { value: '我正在编辑的原文。' } });
    fireEvent.click(screen.getByRole('button', { name: /保存草稿/ })); expect(await screen.findByText('内容已被更新，请刷新后重试')).toBeTruthy(); expect(screen.getByDisplayValue('我正在编辑的原文。')).toBeTruthy();
    const body = await calls('PUT', '/revisions/30')[0]!.json(); expect(body).toMatchObject({ expected_version: 1, mark_reviewed: false, sections: [{ text: '我正在编辑的原文。' }] }); expect(screen.queryByRole('button', { name: /^发\s*布$/ })).toBeNull();
  });
});
