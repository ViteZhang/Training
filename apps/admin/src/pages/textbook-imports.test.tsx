import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { App as AntApp } from 'antd';
import { fireEvent, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';
import { TextbookImports } from './TextbookImports';

type Handler = (req: Request) => { status: number; body?: unknown } | Promise<{ status: number; body?: unknown }>;
const g = globalThis as unknown as { mockApi: Record<string, Handler>; apiCalls: Request[] };
const path = { bookId: 10, editionId: 20, revisionId: 30 };
const root = '/admin/textbooks/{id}/editions/{id}/revisions/{id}/imports';
const revPath = '/admin/textbooks/{id}/editions/{id}/revisions/{id}';
const job = { id: 40, revision_id: 30, file_name: '教材.pdf', format: 'pdf', status: 'ready', page_count: 2, completed_pages: 2, uncertain_pages: 1, attempt: 1, created_at: '2026-10-30T00:00:00Z', updated_at: '2026-10-30T00:00:00Z' };
beforeEach(() => { g.apiCalls.length = 0; g.mockApi = {
  'GET /admin/textbook-import-settings': () => ({ status: 200, body: { max_file_mb: 50, max_pages: 1000, max_imports_per_revision: 50, max_concurrent_imports: 2, max_daily_pdf_pages: 2000 } }),
  [`GET ${root}`]: () => ({ status: 200, body: { items: [job] } }),
  [`GET ${revPath}`]: () => ({ status: 200, body: { id: 30, version: 1, status: 'draft', sections: [] } }),
}; });
function mount() { const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } }); render(<QueryClientProvider client={qc}><AntApp><TextbookImports path={path} onClose={() => undefined} /></AntApp></QueryClientProvider>); return qc; }
describe('教材导入任务', () => {
  it('展示实际限制与逐页进度，预览保留不确定文字', async () => {
    g.mockApi[`GET ${root}/{id}/pages/{id}`] = () => ({ status: 200, body: { page_no: 1, blank: false, printed_page: 'iv', blocks: [{ heading: '第一章', text: '原文中的□字', uncertain: true }] } });
    mount(); expect(await screen.findByText(/单文件最多 50 MB/)).toBeTruthy(); expect(await screen.findByText(/已完成 2 \/ 2 页/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /预\s*览/ })); expect(await screen.findByText('原文中的□字')).toBeTruthy(); expect(screen.getByText('待核对')).toBeTruthy(); expect(screen.getByText('书内页码：iv')).toBeTruthy();
  });
  it('采用必须明确确认，弹窗期间草稿变动仍使用确认时的版本，冲突不标成功', async () => {
    g.mockApi[`POST ${root}/{id}/apply`] = () => ({ status: 409, body: { code: 'CONFLICT', message: '内容已被更新，请刷新后重试' } });
    const qc = mount(); fireEvent.click(await screen.findByRole('button', { name: /导入草稿/ })); expect(g.apiCalls.some((r) => r.method === 'POST')).toBe(false);
    fireEvent.click(screen.getByLabelText('替换当前草稿全部原文'));
    qc.setQueryData(['admin', 'textbooks', 10, 20, 30], { id: 30, version: 2, status: 'draft', sections: [] });
    fireEvent.click(screen.getByRole('button', { name: /确认导入/ })); expect(await screen.findByText('内容已被更新，请刷新后重试')).toBeTruthy();
    const requests = g.apiCalls.filter((r) => r.method === 'POST'); expect(requests).toHaveLength(1); expect(await requests[0]!.json()).toEqual({ expected_version: 1, mode: 'replace' }); expect(screen.getByText('待导入草稿')).toBeTruthy();
  });
  it('上传中断显示恢复原件入口，不将待上传标成识别完成', async () => {
    g.mockApi[`GET ${root}`] = () => ({ status: 200, body: { items: [{ ...job, status: 'uploading', page_count: 0, completed_pages: 0, uncertain_pages: 0 }] } });
    mount(); expect(await screen.findByText('待上传')).toBeTruthy(); expect(screen.getByRole('button', { name: /重新上传原件/ })).toBeTruthy(); expect(screen.queryByRole('button', { name: /导入草稿/ })).toBeNull();
  });
});
