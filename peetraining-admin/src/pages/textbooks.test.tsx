import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { App as AntApp } from 'antd';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';
import { visibleMenu } from '../lib/menu';
import { experienceLicense, kindName, Textbooks } from './Textbooks';

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
    const input = screen.getByRole('searchbox', { name: '搜索资料' }); fireEvent.change(input, { target: { value: '文论' } }); fireEvent.keyDown(input, { key: 'Enter', code: 'Enter', charCode: 13 });
    await waitFor(() => expect(calls('GET', '/admin/textbooks').some((r) => new URL(r.url).searchParams.get('q') === '文论')).toBe(true));
    fireEvent.click(screen.getByRole('button', { name: /重\s*置/ })); expect((input as HTMLInputElement).value).toBe('');
  });
  it('创建先校验授权，网络失败重试复用同一个请求标识', async () => {
    let attempts = 0; g.mockApi['POST /admin/textbooks'] = () => ++attempts === 1 ? { status: 503, body: { code: 'INTERNAL', message: '暂时失败' } } : { status: 200, body: book };
    mount(); fireEvent.click(await screen.findByRole('button', { name: /新建资料/ })); const modal = within(await screen.findByRole('dialog'));
    for (const [label, value] of [['标准书名', '文学理论教程'], ['作者 / 编者', '童庆炳'], ['适用学科', '文学'], ['出版 / 讲义版本', '第五版'], ['授权说明', '出版社授权']]) fireEvent.change(modal.getByLabelText(label!), { target: { value } });
    fireEvent.click(modal.getByRole('button', { name: /保\s*存/ })); expect(await modal.findByText('请确认素材使用授权')).toBeTruthy(); expect(calls('POST', '/admin/textbooks')).toHaveLength(0);
    fireEvent.click(modal.getByLabelText('确认可用于应用内检索及相关摘录展示')); fireEvent.click(modal.getByRole('button', { name: /保\s*存/ })); expect(await modal.findByText('暂时失败')).toBeTruthy();
    fireEvent.click(modal.getByRole('button', { name: /保\s*存/ })); await waitFor(() => expect(calls('POST', '/admin/textbooks')).toHaveLength(2));
    const bodies = await Promise.all(calls('POST', '/admin/textbooks').map((r) => r.json())); expect(bodies[0].idempotency_key).toBe(bodies[1].idempotency_key); expect(bodies[0]).toMatchObject({ title: '文学理论教程', initial_edition: { authorized: true, edition: '第五版' } });
  });
  it('T55 四种资料类型；经验总结不用填授权，提交时自动记为已获授权', async () => {
    expect([kindName('textbook'), kindName('handout'), kindName('syllabus'), kindName('experience'), kindName(undefined)]).toEqual(['教材', '讲义', '考纲', '经验总结', '教材']);
    g.mockApi['POST /admin/textbooks'] = () => ({ status: 200, body: { ...book, kind: 'experience' } });
    mount(); fireEvent.click(await screen.findByRole('button', { name: /新建资料/ })); const modal = within(await screen.findByRole('dialog'));
    fireEvent.mouseDown(modal.getByLabelText('类型'));
    fireEvent.click(await screen.findByTitle('经验总结'));
    expect(await modal.findByText('经验总结上传即视为已获授权，不用另外填写授权说明')).toBeTruthy();
    expect(modal.queryByLabelText('授权说明')).toBeNull();
    for (const [label, value] of [['标准书名', '808 上岸经验'], ['作者 / 编者', '北师大808上岸学长'], ['适用学科', '文学'], ['出版 / 讲义版本', '2025']]) fireEvent.change(modal.getByLabelText(label!), { target: { value } });
    fireEvent.click(modal.getByRole('button', { name: /保\s*存/ }));
    await waitFor(() => expect(calls('POST', '/admin/textbooks')).toHaveLength(1));
    expect(await calls('POST', '/admin/textbooks')[0]!.json()).toMatchObject({ kind: 'experience', initial_edition: { authorized: true, license_note: experienceLicense } });
  });
  it('冲突保留原文，不将失败保存显示为成功或发布', async () => {
    g.mockApi['PUT /admin/textbooks/{id}/editions/{id}/revisions/{id}'] = () => ({ status: 409, body: { code: 'CONFLICT', message: '内容已被更新，请刷新后重试' } });
    mount(); fireEvent.click(await screen.findByRole('button', { name: /管\s*理/ })); fireEvent.click(await screen.findByRole('button', { name: /校对原文/ }));
    const text = await screen.findByDisplayValue('文学活动的四要素。'); fireEvent.change(text, { target: { value: '我正在编辑的原文。' } });
    fireEvent.click(screen.getByRole('button', { name: /保存草稿/ })); expect(await screen.findByText('内容已被更新，请刷新后重试')).toBeTruthy(); expect(screen.getByDisplayValue('我正在编辑的原文。')).toBeTruthy();
    const body = await calls('PUT', '/revisions/30')[0]!.json(); expect(body).toMatchObject({ expected_version: 1, mark_reviewed: false, sections: [{ text: '我正在编辑的原文。' }] }); expect(screen.queryByRole('button', { name: /^发\s*布$/ })).toBeNull();
  });
  it('导入草稿后自动打开原文校对；确认校对后自动打开索引与发布，无需手动关窗口或刷新', async () => {
    let applied = false; let reviewed = false;
    const imp = '/admin/textbooks/{id}/editions/{id}/revisions/{id}/imports';
    const job = { id: 40, revision_id: 30, file_name: '教材.pdf', format: 'pdf', status: 'ready', page_count: 1, completed_pages: 1, uncertain_pages: 0, attempt: 1, created_at: time, updated_at: time };
    g.mockApi['GET /admin/textbook-import-settings'] = () => ({ status: 200, body: { max_file_mb: 50, max_pages: 1000, max_imports_per_revision: 50, max_concurrent_imports: 2, max_daily_pdf_pages: 2000 } });
    g.mockApi[`GET ${imp}`] = () => ({ status: 200, body: { items: [{ ...job, status: applied ? 'applied' : 'ready' }] } });
    g.mockApi[`POST ${imp}/{id}/apply`] = () => { applied = true; return { status: 200, body: { ...revision, version: 2, sections: [{ heading: '第二章', text: '导入的新正文。' }] } }; };
    g.mockApi['GET /admin/textbooks/{id}/editions/{id}/revisions/{id}'] = () => ({ status: 200, body: reviewed ? { ...revision, version: 3, status: 'reviewed', sections: [{ heading: '第二章', text: '导入的新正文。' }] } : applied ? { ...revision, version: 2, sections: [{ heading: '第二章', text: '导入的新正文。' }] } : revision });
    g.mockApi['PUT /admin/textbooks/{id}/editions/{id}/revisions/{id}'] = () => { reviewed = true; return { status: 200, body: { ...revision, version: 3, status: 'reviewed', sections: [{ heading: '第二章', text: '导入的新正文。' }] } }; };
    g.mockApi['GET /admin/textbooks/{id}/editions/{id}/revisions/{id}/indexes'] = () => ({ status: 200, body: { items: [] } });
    g.mockApi['GET /admin/textbook-retrieval-settings'] = () => ({ status: 200, body: { embedding_enabled: true, embedding_provider: 'openai', embedding_model: 'm', embedding_dimensions: 1024, vector_configured: true, profile_id: 'p', production: false, parameters: { version: 'v1' } } });
    mount(); fireEvent.click(await screen.findByRole('button', { name: /管\s*理/ })); fireEvent.click(await screen.findByRole('button', { name: /导入文件/ }));
    fireEvent.click(await screen.findByRole('button', { name: /导入草稿/ })); fireEvent.click(await screen.findByRole('button', { name: /确认导入/ }));
    expect(await screen.findByDisplayValue('导入的新正文。')).toBeTruthy();
    expect(screen.queryByText('教材文件导入')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: /^确认校对$/ }));
    await waitFor(() => expect(screen.getAllByRole('button', { name: /^确认校对$/ }).length).toBe(2));
    fireEvent.click(screen.getAllByRole('button', { name: /^确认校对$/ }).at(-1)!); // 二次确认弹窗里的按钮
    expect(await screen.findByText(/索引与发布$/)).toBeTruthy();
    expect(await calls('PUT', '/revisions/30')[0]!.json()).toMatchObject({ expected_version: 2, mark_reviewed: true });
    await waitFor(() => expect((screen.getByRole('button', { name: /构建新索引代次/ }) as HTMLButtonElement).disabled).toBe(false));
  });
});


describe('教材使用范围', () => {
  function scopesBook() {
    return { ...book, editions: [
      { ...book.editions[0]!, version: 2, scope_note: '北京师范大学 · 文学专业' },
      { ...book.editions[0]!, id: 21, edition: '第六版', version: 3, scope_note: '全国院校 · 文学理论课程' },
    ] };
  }
  it('列表按版次展示范围，编辑回填并保存修改和清空，刷新列表', async () => {
    let current = scopesBook();
    g.mockApi['GET /admin/textbooks'] = () => ({ status: 200, body: { items: [current], total: 1, page: 1, page_size: 20 } });
    g.mockApi['GET /admin/textbooks/{id}'] = () => ({ status: 200, body: current });
    g.mockApi['PUT /admin/textbooks/{id}'] = async (req) => {
      const body = await req.json();
      current = { ...current, version: 2, editions: current.editions.map((e) => ({ ...e, scope_note: body.edition_scopes.find((v: { edition_id: number }) => v.edition_id === e.id)?.scope_note ?? e.scope_note })) };
      return { status: 200, body: current };
    };
    mount();
    expect(await screen.findByText('北京师范大学 · 文学专业')).toBeTruthy();
    expect(screen.getByText('全国院校 · 文学理论课程')).toBeTruthy();
    expect(screen.getByRole('columnheader', { name: '使用范围' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /管\s*理/ }));
    fireEvent.click(await screen.findByRole('button', { name: '编辑资料信息' }));
    const modal = within((await screen.findByLabelText('第五版 · 使用范围')).closest('[role="dialog"]') as HTMLElement);
    expect((modal.getByLabelText('第五版 · 使用范围') as HTMLTextAreaElement).value).toBe('北京师范大学 · 文学专业');
    fireEvent.change(modal.getByLabelText('第五版 · 使用范围'), { target: { value: '北京师范大学 · 808 文学综合' } });
    fireEvent.change(modal.getByLabelText('第六版 · 使用范围'), { target: { value: '' } });
    fireEvent.click(modal.getByRole('button', { name: /保\s*存/ }));
    await waitFor(() => expect(calls('PUT', '/admin/textbooks/10')).toHaveLength(1));
    expect(await calls('PUT', '/admin/textbooks/10')[0]!.json()).toMatchObject({ expected_version: 1, edition_scopes: [
      { edition_id: 20, expected_version: 2, scope_note: '北京师范大学 · 808 文学综合' },
      { edition_id: 21, expected_version: 3, scope_note: '' },
    ] });
    await waitFor(() => expect(screen.queryByLabelText('第五版 · 使用范围')).toBeNull());
    await waitFor(() => expect(within(screen.getByRole('columnheader', { name: '使用范围' }).closest('table') as HTMLElement).getByText('北京师范大学 · 808 文学综合')).toBeTruthy());
    expect(within(screen.getByRole('columnheader', { name: '使用范围' }).closest('table') as HTMLElement).getByText('未填写')).toBeTruthy();
  });
  it('版本冲突保留范围输入，不显示保存成功', async () => {
    const current = scopesBook();
    g.mockApi['GET /admin/textbooks/{id}'] = () => ({ status: 200, body: current });
    g.mockApi['PUT /admin/textbooks/{id}'] = () => ({ status: 409, body: { code: 'CONFLICT', message: '内容已被更新，请刷新后重试' } });
    mount(); fireEvent.click(await screen.findByRole('button', { name: /管\s*理/ }));
    fireEvent.click(await screen.findByRole('button', { name: '编辑资料信息' }));
    const modal = within((await screen.findByLabelText('第五版 · 使用范围')).closest('[role="dialog"]') as HTMLElement);
    fireEvent.change(modal.getByLabelText('第五版 · 使用范围'), { target: { value: '尚未保存的范围' } });
    fireEvent.click(modal.getByRole('button', { name: /保\s*存/ }));
    expect(await modal.findByText('内容已被更新，请刷新后重试')).toBeTruthy();
    expect(modal.getByDisplayValue('尚未保存的范围')).toBeTruthy();
    const body = await calls('PUT', '/admin/textbooks/10')[0]!.json();
    expect(body.edition_scopes).toEqual([{ edition_id: 20, expected_version: 2, scope_note: '尚未保存的范围' }]);
  });
});


it('保存返回200但回读仍为旧数据时，提示未保存并保留范围输入', async () => {
 const current = { ...book, editions: [{ ...book.editions[0]!, version: 2, scope_note: '原使用范围' }] };
 g.mockApi['GET /admin/textbooks/{id}'] = () => ({ status: 200, body: current });
 g.mockApi['PUT /admin/textbooks/{id}'] = () => ({ status: 200, body: current });
 mount(); fireEvent.click(await screen.findByRole('button', { name: /管\s*理/ }));
 fireEvent.click(await screen.findByRole('button', { name: '编辑资料信息' }));
 const modal = within((await screen.findByLabelText('第五版 · 使用范围')).closest('[role="dialog"]') as HTMLElement);
 fireEvent.change(modal.getByLabelText('第五版 · 使用范围'), { target: { value: '本次修改的范围' } });
 fireEvent.click(modal.getByRole('button', { name: /保\s*存/ }));
 expect(await modal.findByText('使用范围未保存成功，当前输入已保留，请重试')).toBeTruthy();
 expect(modal.getByDisplayValue('本次修改的范围')).toBeTruthy();
 expect(screen.queryByText('资料信息已保存')).toBeNull();
 expect(calls('GET', '/admin/textbooks/10').length).toBeGreaterThanOrEqual(2);
});

it('书名更新未落库时不能静默成功', async () => {
 g.mockApi['PUT /admin/textbooks/{id}'] = () => ({ status: 200, body: book });
 mount(); fireEvent.click(await screen.findByRole('button', { name: /管\s*理/ }));
 fireEvent.click(await screen.findByRole('button', { name: '编辑资料信息' }));
 const modal = within((await screen.findByLabelText('第五版 · 使用范围')).closest('[role="dialog"]') as HTMLElement);
 fireEvent.change(modal.getByLabelText('标准书名'), { target: { value: '新的教材书名' } });
 fireEvent.click(modal.getByRole('button', { name: /保\s*存/ }));
 expect(await modal.findByText('资料信息未保存成功，当前输入已保留，请重试')).toBeTruthy();
 expect(modal.getByDisplayValue('新的教材书名')).toBeTruthy();
});
it('编辑套书名：提交去空格后的套书名，回读一致才算保存成功，详情显示套书名', async () => {
 let current: typeof book & { series?: string } = book;
 g.mockApi['GET /admin/textbooks/{id}'] = () => ({ status: 200, body: current });
 g.mockApi['PUT /admin/textbooks/{id}'] = async (req) => { const body = await req.json(); current = { ...current, version: 2, series: body.series }; return { status: 200, body: current }; };
 mount(); fireEvent.click(await screen.findByRole('button', { name: /管\s*理/ }));
 expect(await screen.findByText('套书名')).toBeTruthy();
 fireEvent.click(await screen.findByRole('button', { name: '编辑资料信息' }));
 const modal = within((await screen.findByLabelText('第五版 · 使用范围')).closest('[role="dialog"]') as HTMLElement);
 fireEvent.change(modal.getByLabelText('套书名（选填）'), { target: { value: ' 文学理论 ' } });
 fireEvent.click(modal.getByRole('button', { name: /保\s*存/ }));
 await waitFor(() => expect(calls('PUT', '/admin/textbooks/10')).toHaveLength(1));
 expect(await calls('PUT', '/admin/textbooks/10')[0]!.json()).toMatchObject({ series: '文学理论', aliases: ['文论教程'] });
 expect(await screen.findByText('文学理论')).toBeTruthy();
});
