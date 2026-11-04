import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { App as AntApp } from 'antd';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';
import { type components } from '@training/api-client';
import { TextbookIndexes } from './TextbookIndexes';

type Handler = (req: Request) => { status: number; body?: unknown };
const g = globalThis as unknown as { mockApi: Record<string, Handler>; apiCalls: Request[] };
const path = { bookId: 10, editionId: 20, revisionId: 30 };
const root = '/admin/textbooks/{id}/editions/{id}/revisions/{id}';
const params = { version: 'v1', chunk_runes: 700, overlap_runes: 100, recall_k: 20, result_k: 8, context_runes: 6000 };
const revision = { id: 30, edition_id: 20, version: 2, revision_no: 1, status: 'reviewed', index_status: 'ready', content_hash: 'abc', ready_generation_id: 40, sections: [] };
const index = { id: 40, revision_id: 30, source_version: 2, content_hash: 'abc', profile_id: 'profile1', model: 'qwen3.7-text-embedding-flash', dimensions: 1024, status: 'ready', chunk_count: 2, completed_chunks: 2, parameters: params, failure_message: '', created_at: '2026-10-30T00:00:00Z', updated_at: '2026-10-30T00:00:00Z' };
const book = { id: 10, title: '文学理论教程', version: 1 } as components['schemas']['Textbook'];
const edition = { id: 20, edition: '第五版', version: 3, authorized: true } as components['schemas']['TextbookEdition'];
beforeEach(() => { g.apiCalls.length = 0; g.mockApi = {
  [`GET ${root}`]: () => ({ status: 200, body: revision }),
  [`GET ${root}/indexes`]: () => ({ status: 200, body: { items: [index] } }),
  'GET /admin/textbook-retrieval-settings': () => ({ status: 200, body: { embedding_enabled: true, embedding_provider: 'openai', embedding_model: index.model, embedding_dimensions: 1024, vector_configured: true, profile_id: 'profile1', production: false, parameters: params } }),
}; });
function mount() { const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } }); render(<QueryClientProvider client={qc}><AntApp><TextbookIndexes path={path} book={book} edition={edition} onClose={() => undefined} /></AntApp></QueryClientProvider>); return qc; }
describe('教材索引与发布', () => {
  it('未配置向量服务不能构建，不把校对当成索引成功', async () => {
    g.mockApi['GET /admin/textbook-retrieval-settings'] = () => ({ status: 200, body: { embedding_enabled: false, vector_configured: false, embedding_dimensions: 1024 } });
    g.mockApi[`GET ${root}/indexes`] = () => ({ status: 200, body: { items: [] } });
    mount(); expect((await screen.findByRole('button', { name: /构建新索引代次/ }) as HTMLButtonElement).disabled).toBe(true); expect(await screen.findByText('还没有索引代次')).toBeTruthy();
  });
  it('发布明确确认且固定弹窗打开时版本，冲突保留说明', async () => {
    g.mockApi[`POST ${root}/publish`] = () => ({ status: 409, body: { code: 'CONFLICT', message: '内容已被更新，请刷新后重试' } });
    const qc = mount(); fireEvent.click(await screen.findByRole('button', { name: /^校验发布$/ }));
    expect(g.apiCalls.filter(r => r.method === 'POST')).toHaveLength(0);
    fireEvent.change(screen.getByLabelText('发布变更说明'), { target: { value: '校对并更新第一章' } });
    qc.setQueryData(['admin', 'textbooks', 10, 20, 30], { ...revision, version: 9 });
    fireEvent.click(screen.getByRole('button', { name: /^校验并发布$/ }));
    expect(await screen.findByText('内容已被更新，请刷新后重试')).toBeTruthy();
    const request = g.apiCalls.find(r => r.method === 'POST')!; expect(await request.json()).toMatchObject({ expected_version: 2, expected_edition_version: 3, generation_id: 40, summary: '校对并更新第一章' }); expect(screen.getByDisplayValue('校对并更新第一章')).toBeTruthy();
  });
  it('展示真实片段和两类召回排名，不将试检索直接发布', async () => {
    g.mockApi[`POST ${root}/search`] = () => ({ status: 200, body: { generation_id: 40, profile_id: 'profile1', fusion: 'rrf-v1', notice: '仅供召回诊断', hits: [{ ordinal: 1, section_no: 1, start_rune: 0, end_rune: 15, heading: '第一章', text: '文学具有审美意识形态性质。', text_hash: 'hash', source_page: 4, fulltext_rank: 2, vector_rank: 1, fusion_score: .0325 }] } });
    mount(); fireEvent.change(await screen.findByLabelText('检索问题'), { target: { value: '如何理解文学的性质' } });
    await waitFor(() => expect((screen.getByRole('button', { name: /试检索原文/ }) as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByRole('button', { name: /试检索原文/ }));
    expect(await screen.findByText('文学具有审美意识形态性质。')).toBeTruthy(); expect(screen.getByText(/全文排名 2 · 向量排名 1/)).toBeTruthy(); expect(g.apiCalls.filter(r => r.method === 'POST')).toHaveLength(1);
  });
  it('失败代次提供续跑，未就绪不能试检索或发布', async () => {
    g.mockApi[`GET ${root}/indexes`] = () => ({ status: 200, body: { items: [{ ...index, status: 'failed', completed_chunks: 1 }] } });
    g.mockApi[`POST ${root}/indexes/{id}/retry`] = () => ({ status: 200, body: { ...index, status: 'queued' } });
    mount(); fireEvent.click(await screen.findByRole('button', { name: /继续构建/ }));
    await waitFor(() => expect(g.apiCalls.filter(r => r.method === 'POST')).toHaveLength(1));
    expect((screen.getByRole('button', { name: /^校验发布$/ }) as HTMLButtonElement).disabled).toBe(true);
  });
});
