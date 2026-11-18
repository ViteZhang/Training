import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { App as AntApp } from 'antd';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { ReactElement } from 'react';
import type { Schemas } from '@/api-client';
import { MemoryRouter } from 'react-router';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { visibleMenu } from '../lib/menu';
import { uploadTextbookFile } from '../lib/textbookUpload';
import { Applications, hoursAgo, toCSV } from './Applications';
import { Catalog, splitBooks } from './Catalog';
import { canBatchAccept, citationSource, splitKPNames, fillLabel, fillProgress, formatOf, ImportSection, itemTitle, sameYearExams, hasUsableTextbook, yearFromName } from './OfficialImport';
import { rubricSum, Samples } from './Samples';

// jsdom 的 fetch 发不了 File，OSS 直传换成假的，只记录调用。
vi.mock('../lib/textbookUpload', () => ({ uploadTextbookFile: vi.fn(async () => undefined) }));

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
    expect(official.pages.map((p) => p.title)).toEqual(['申请收录', '需求洞察', '官方题库', '审核队列', '版本发布', '通用体验题']);
    expect(visibleMenu(['admin']).find((m) => m.key === 'config')!.pages.map((p) => p.title)).toContain('考试科目库');
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

  it('AI 批量生成可以关联已发布的教材，未发布的版本不列出', async () => {
    const editions = [
      { id: 21, book_id: 9, edition: '第二版', publisher: '', isbn: '', license_note: '授权', authorized: true, scope_note: '', status: 'published', revisions: [] },
      { id: 22, book_id: 9, edition: '第三版', publisher: '', isbn: '', license_note: '授权', authorized: true, scope_note: '', status: 'unpublished', revisions: [] },
    ];
    g.mockApi['GET /admin/textbooks'] = () => ({ status: 200, body: { items: [{ id: 9, title: '西方哲学史', author: '张志伟', kind: 'textbook', discipline: '哲学', aliases: [], version: 1, created_at: '2026-11-01T00:00:00Z', updated_at: '2026-11-01T00:00:00Z', editions }], total: 1, page: 1, page_size: 50 } });
    g.mockApi['POST /admin/samples/generate'] = () => ({ status: 200, body: { created: 3 } });
    wrap(<Samples roles={['content_lead']} />);
    fireEvent.click(await screen.findByRole('button', { name: /AI 批量生成初稿/ }));
    const select = await screen.findByRole('combobox', { name: '关联已上传教材' });
    fireEvent.mouseDown(select);
    fireEvent.click(await screen.findByText('【教材】西方哲学史 · 张志伟 · 第二版'));
    expect(screen.queryByText('【教材】西方哲学史 · 张志伟 · 第三版')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: /确\s*定|OK/ }));
    await waitFor(() => expect(calls('POST', /\/samples\/generate$/)).toHaveLength(1));
    expect(await calls('POST', /\/samples\/generate$/)[0]!.json()).toMatchObject({ direction_id: 3, qtype: 'term', score: 5, count: 3, edition_ids: [21] });
  });

  it('采分点合计', () => {
    expect(rubricSum([{ score: 2 }, { score: 3 }, {}])).toBe(5);
  });
});

describe('考试科目库（T50）', () => {
  beforeEach(() => {
    g.mockApi['GET /admin/catalog/courses'] = () => ({
      status: 200,
      body: {
        items: [
          { id: 1, scope: 'school', school: '四川大学', major: '哲学', majors: ['哲学', '宗教学'], discipline: '哲学', subject_name: '西方哲学史', subject_code: '812',
            textbooks: ['张志伟《西方哲学史》'], official_status: 'producing', status: 'active', chosen_count: 4, school_count: 0, project_id: 3 },
          { id: 7, scope: 'national', school: '', major: '', majors: [], discipline: '教育学', subject_name: '教育综合', subject_code: '333',
            textbooks: [], official_status: 'none', status: 'active', chosen_count: 2, school_count: 2 },
        ],
      },
    });
    g.mockApi['GET /admin/catalog/disciplines'] = () => ({ status: 200, body: { items: ['哲学', '教育学', '文学'] } });
    g.mockApi['GET /admin/catalog/courses/{id}/schools'] = () => ({ status: 200, body: { items: ['华东师范大学', '陕西师范大学'] } });
    g.mockApi['PUT /admin/catalog/courses/{id}/schools'] = () => ({ status: 204 });
    g.mockApi['POST /admin/official/projects'] = () => ({ status: 200, body: { id: 11 } });
    g.mockApi['GET /admin/catalog/pending'] = () => ({
      status: 200,
      body: { items: [{ id: 9, school: '南京大学', major: '中国语言文学', subject_name: '文学理论', subject_code: '831', request_count: 2, first_at: '2026-11-01T00:00:00Z', last_at: '2026-11-02T00:00:00Z' }] },
    });
    g.mockApi['POST /admin/catalog/pending/{id}/resolve'] = () => ({ status: 200, body: { course_id: 2 } });
  });

  it('考试科目带命题方式、适用专业和官方题库状态；待入库可以忽略', async () => {
    wrap(<Catalog roles={['content_lead']} />);
    expect(await screen.findByText('812 西方哲学史')).toBeTruthy();
    expect(screen.getByText('生产中')).toBeTruthy();
    expect(screen.getByText('哲学、宗教学')).toBeTruthy();
    expect(screen.getByText('全国统考')).toBeTruthy();
    expect(screen.getByText('已登记 2 所')).toBeTruthy();
    fireEvent.click(screen.getByRole('tab', { name: /待入库/ }));
    expect(await screen.findByText('831 文学理论')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /^忽\s?略$/ }));
    await waitFor(() => expect(calls('POST', /\/catalog\/pending\/9\/resolve$/)).toHaveLength(1));
  });

  it('统考可以登记院校；没立项的从考试科目直接立项', async () => {
    wrap(<Catalog roles={['content_lead']} />);
    expect(await screen.findByText('333 教育综合')).toBeTruthy();
    // 已立项的只能查看项目，没立项的有「立项」。
    expect(screen.getAllByRole('button', { name: /^立\s?项$/ })).toHaveLength(1);
    fireEvent.click(screen.getByRole('button', { name: '登记院校' }));
    expect(await screen.findByText('华东师范大学')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /^保\s?存$/ }));
    await waitFor(() => expect(calls('PUT', /\/catalog\/courses\/7\/schools$/)).toHaveLength(1));
    const put = calls('PUT', /\/catalog\/courses\/7\/schools$/)[0]!;
    expect(await put.clone().json()).toEqual({ schools: ['华东师范大学', '陕西师范大学'] });
    fireEvent.click(screen.getByRole('button', { name: /^立\s?项$/ }));
    // 确认框里的「立项」。
    await waitFor(() => expect(screen.getAllByRole('button', { name: /^立\s?项$/ }).length).toBe(2));
    fireEvent.click(screen.getAllByRole('button', { name: /^立\s?项$/ }).at(-1)!);
    await waitFor(() => expect(calls('POST', /\/admin\/official\/projects$/)).toHaveLength(1));
    expect(await calls('POST', /\/admin\/official\/projects$/)[0]!.clone().json()).toEqual({ catalog_course_id: 7 });
  });

  it('数据分析等没有立项权限的看不到立项按钮', async () => {
    wrap(<Catalog roles={[]} />);
    expect(await screen.findByText('333 教育综合')).toBeTruthy();
    expect(screen.queryByRole('button', { name: /^立\s?项$/ })).toBeNull();
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
    expect(formatOf('2025真题.md')).toBe('md');
    expect(formatOf('讲义.Markdown')).toBe('md');
  });

  it('文件名里只有一个年份时才当真题年份，多年合集留给逐题识别', () => {
    expect(yearFromName('2024真题.pdf')).toBe(2024);
    expect(yearFromName('四川大学2019年中国哲学史.docx')).toBe(2019);
    expect(yearFromName('四川大学中国哲学史真题（1997—2026）.docx')).toBeUndefined();
    expect(yearFromName('812真题.pdf')).toBeUndefined();
    expect(yearFromName('真题12024.pdf')).toBeUndefined();
  });

  it('解析结果标题带上年份', () => {
    const it = { item_type: 'question', payload: { qtype: 'term', stem: '仁', exam_year: 1997 } } as unknown as Parameters<typeof itemTitle>[0];
    expect(itemTitle(it)).toBe('1997 · 名词解释：仁');
  });

  it('一次选多个文件：每个文件建一条解析记录、各自直传并开始解析，文件名里的年份带上', async () => {
    const project = { id: 7, school: '四川大学', major: '中国哲学', subject_code: '812', subject_name: '中国哲学史', stage: 'materials' };
    let next = 100;
    g.mockApi = {
      'GET /admin/official/projects': () => ({ status: 200, body: { items: [project] } }),
      'GET /admin/official/projects/{id}/imports': () => ({ status: 200, body: { items: [] } }),
      'POST /admin/official/projects/{id}/imports': () => {
        next += 1;
        return { status: 200, body: { id: next, kind: 'exam', file_name: 'x', format: 'docx', status: 'uploading', question_count: 0, kp_count: 0, reviewed_count: 0, upload_url: `http://oss.test/put/${next}`, upload_headers: {} } };
      },
      'POST /admin/official/projects/{id}/imports/{id}/confirm': () => ({ status: 204 }),
    };
    wrap(<ImportSection projectId={7} />);
    // 只有一个项目时自动选中
    fireEvent.click(await screen.findByText('+ 上传资料'));
    const input = document.querySelector('input[type=file]') as HTMLInputElement;
    expect(input.multiple).toBe(true);
    const files = [new File(['a'], '四川大学中国哲学史真题（1997—2026）.docx'), new File(['b'], '2024真题.pdf')];
    fireEvent.change(input, { target: { files } });
    expect(await screen.findByText('已选 2 个文件，每个文件单独解析、单独核对')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /确\s*定/ }));
    await waitFor(() => expect(calls('POST', /\/imports\/\d+\/confirm$/)).toHaveLength(2));
    const created = await Promise.all(calls('POST', /\/projects\/7\/imports$/).map((r) => r.json()));
    expect(created).toEqual([
      expect.objectContaining({ kind: 'exam', format: 'docx', file_name: '四川大学中国哲学史真题（1997—2026）.docx' }),
      expect.objectContaining({ kind: 'exam', format: 'pdf', file_name: '2024真题.pdf', exam_year: 2024 }),
    ]);
    expect(created[0]).not.toHaveProperty('exam_year');
    expect(vi.mocked(uploadTextbookFile).mock.calls.map(([f, t]) => [f.name, t.upload_url])).toEqual([
      ['四川大学中国哲学史真题（1997—2026）.docx', 'http://oss.test/put/101'],
      ['2024真题.pdf', 'http://oss.test/put/102'],
    ]);
  });
  it('T49 补全按钮、进度与引用出处', () => {
    expect(fillLabel({ answer: '' })).toBe('生成答案');
    expect(fillLabel({ answer: '逻格斯是…' })).toBe('提取采分点');
    expect(fillProgress({ fill: { status: 'running', total: 293, done: 40, failed: 2 } })).toBe('补全中 40/293，失败 2');
    expect(fillProgress({ fill: { status: 'idle', total: 0, done: 0, failed: 0 } })).toBeUndefined();
    expect(citationSource({ title: '西方哲学史', heading: '第二章 赫拉克利特', printed_page: '35' })).toBe('西方哲学史 · 第二章 赫拉克利特 · 第 35 页');
  });

  it('T49 上传时可勾「缺答案时由 AI 生成」，默认不勾', async () => {
    const project = { id: 7, school: '四川大学', major: '哲学', subject_code: '812', subject_name: '西方哲学史', stage: 'materials' };
    g.mockApi = {
      'GET /admin/official/projects': () => ({ status: 200, body: { items: [project] } }),
      'GET /admin/official/projects/{id}/imports': () => ({ status: 200, body: { items: [] } }),
      'POST /admin/official/projects/{id}/imports': () => ({ status: 200, body: { id: 1, kind: 'exam', file_name: '粘贴的内容', format: 'text', status: 'parsing', question_count: 0, kp_count: 0, reviewed_count: 0 } }),
    };
    wrap(<ImportSection projectId={7} />);
    fireEvent.click(await screen.findByText('+ 上传资料'));
    fireEvent.change(screen.getByPlaceholderText(/没有文件时可以粘贴真题或讲义原文/), { target: { value: '一、名词解释\n1. 理念' } });
    fireEvent.click(screen.getByRole('button', { name: /确\s*定/ }));
    await waitFor(() => expect(calls('POST', /\/projects\/7\/imports$/)).toHaveLength(1));
    expect(await calls('POST', /\/projects\/7\/imports$/)[0]!.json()).toEqual(expect.objectContaining({ format: 'text', auto_answer: false }));
    g.apiCalls.length = 0;
    fireEvent.click(await screen.findByText('+ 上传资料'));
    fireEvent.change(screen.getByPlaceholderText(/没有文件时可以粘贴真题或讲义原文/), { target: { value: '一、名词解释\n1. 理念' } });
    fireEvent.click(screen.getByText('缺答案时由 AI 生成'));
    fireEvent.click(screen.getByRole('button', { name: /确\s*定/ }));
    await waitFor(() => expect(calls('POST', /\/projects\/7\/imports$/)).toHaveLength(1));
    expect(await calls('POST', /\/projects\/7\/imports$/)[0]!.json()).toEqual(expect.objectContaining({ auto_answer: true }));
  });

  it('T49 核对区：没有采分点不能接受；点「生成答案」后显示 AI 生成、分值待核对与引用原文；可一键补全', async () => {
    const project = { id: 7, school: '四川大学', major: '哲学', subject_code: '812', subject_name: '西方哲学史', stage: 'materials' };
    const file = { id: 5, kind: 'exam', file_name: '四川大学西方哲学史真题（1997—2026）.docx', format: 'docx', status: 'parsed', question_count: 2, kp_count: 0, reviewed_count: 0, fill: { status: 'idle', total: 0, done: 0, failed: 0 } };
    const item = { id: 11, item_type: 'question', seq: 1, payload: { qtype: 'term', stem: '逻格斯（赫拉克利特）', exam_year: 2026, kp_names: [], rubric: [] }, confidence: 'low', status: 'pending', rubric_mismatch: false, needs_fill: true, fill_error: 'AI 暂时不可用，请稍后再试' };
    const item2 = { ...item, id: 12, seq: 2, payload: { ...item.payload, stem: '理念（柏拉图）' }, fill_error: undefined };
    const filled = {
      ...item,
      payload: { ...item.payload, score: 5, score_inferred: true, answer_ai: true, answer: '逻格斯是赫拉克利特提出的万物运动的尺度。', rubric: [{ content: '赫拉克利特提出', score: 2 }, { content: '万物运动的尺度', score: 3 }] },
      confidence: 'medium',
      needs_fill: false,
      fill_error: undefined,
      citations: [{ reference_id: 3, title: '西方哲学史', heading: '第二章 赫拉克利特', kind: 'official_textbook', quote: '逻各斯是万物运动变化的尺度。', claim: '万物运动的尺度', printed_page: '35' }],
    };
    g.mockApi = {
      'GET /admin/official/projects': () => ({ status: 200, body: { items: [project] } }),
      'GET /admin/official/projects/{id}/imports': () => ({ status: 200, body: { items: [file] } }),
      'GET /admin/official/projects/{id}/imports/{id}': () => ({ status: 200, body: { file, text: '一、名词解释\n1.逻格斯（赫拉克利特）', items: [item, item2] } }),
      'POST /admin/official/projects/{id}/import-items/{id}/fill': () => ({ status: 200, body: filled }),
      'POST /admin/official/projects/{id}/imports/{id}/fill': () => ({ status: 200, body: { ...file, fill: { status: 'running', total: 2, done: 0, failed: 0 } } }),
    };
    wrap(<ImportSection projectId={7} />);
    fireEvent.click(await screen.findByText(/四川大学西方哲学史真题/));
    expect(await screen.findByText('上次没补上：AI 暂时不可用，请稍后再试')).toBeTruthy();
    expect(screen.getByRole('button', { name: /接受，下一题/ }).hasAttribute('disabled')).toBe(true);
    fireEvent.click(screen.getByRole('button', { name: '生成答案' }));
    await waitFor(() => expect(calls('POST', /\/projects\/7\/import-items\/11\/fill$/)).toHaveLength(1));
    expect(await screen.findByText('AI 生成')).toBeTruthy();
    expect(screen.getByText('分值待核对')).toBeTruthy();
    expect(screen.getByText('西方哲学史 · 第二章 赫拉克利特 · 第 35 页')).toBeTruthy();
    expect(screen.getByText('「逻各斯是万物运动变化的尺度。」')).toBeTruthy();
    expect(screen.getByRole('button', { name: /接受，下一题/ }).hasAttribute('disabled')).toBe(false);
    fireEvent.click(screen.getByRole('button', { name: /一键补全/ }));
    await waitFor(() => expect(calls('POST', /\/projects\/7\/imports\/5\/fill$/)).toHaveLength(1));
  });

  it('单题生成在后台排队：点了显示「生成中」，生成完之前不能接受、修改，也不能再点', async () => {
    const project = { id: 7, school: '北京师范大学', major: '', subject_code: '708', subject_name: '中国语言文学综合', stage: 'materials' };
    const file = { id: 5, kind: 'exam', file_name: '北师大708真题.md', format: 'md', status: 'parsed', question_count: 1, kp_count: 0, reviewed_count: 0, fill: { status: 'idle', total: 0, done: 0, failed: 0 } };
    const item = { id: 11, item_type: 'question', seq: 1, payload: { qtype: 'short_answer', stem: '请列出《说文解字》对六书的划分', score: 10, exam_year: 2026, kp_names: [], rubric: [] }, confidence: 'medium', status: 'pending', rubric_mismatch: true, needs_fill: true };
    const queued = { ...item, filling: true };
    g.mockApi = {
      'GET /admin/official/projects': () => ({ status: 200, body: { items: [project] } }),
      'GET /admin/official/projects/{id}/imports': () => ({ status: 200, body: { items: [file] } }),
      'GET /admin/official/projects/{id}/imports/{id}': () => ({ status: 200, body: { file, text: '1. 请列出《说文解字》对六书的划分', items: [item] } }),
      'POST /admin/official/projects/{id}/import-items/{id}/fill': () => ({ status: 200, body: queued }),
    };
    wrap(<ImportSection projectId={7} />);
    fireEvent.click(await screen.findByText('北师大708真题.md'));
    fireEvent.click(await screen.findByRole('button', { name: '生成答案' }));
    await waitFor(() => expect(calls('POST', /\/projects\/7\/import-items\/11\/fill$/)).toHaveLength(1));
    expect(await screen.findByText('正在生成答案')).toBeTruthy();
    expect(screen.getByText('生成中')).toBeTruthy();
    expect(screen.queryByRole('button', { name: '生成答案' })).toBeNull();
    expect(screen.getByRole('button', { name: /接受，下一题/ }).hasAttribute('disabled')).toBe(true);
    expect(screen.getByRole('button', { name: /修\s*改/ }).hasAttribute('disabled')).toBe(true);
    expect(canBatchAccept({ ...queued, needs_fill: false, rubric_mismatch: false, payload: { ...item.payload, rubric: [{ content: '六书', score: 10 }] } } as never)).toBe(false);
  });

  it('T56 知识点名称用「；」隔开（名称里可以有顿号）；知识点对不上的不批量接受', () => {
    expect(splitKPNames('六书；单义词、多义词与反义词;六书\n同音词')).toEqual(['六书', '单义词、多义词与反义词', '同音词']);
    const ok = { id: 1, item_type: 'question', seq: 1, status: 'pending', confidence: 'high', rubric_mismatch: false, payload: { qtype: 'term', stem: '六书', rubric: [{ content: '六书', score: 10 }] } };
    expect(canBatchAccept(ok as never)).toBe(true);
    expect(canBatchAccept({ ...ok, missing_kps: ['六书'] } as never)).toBe(false);
  });

  it('T52 Markdown 直接带内容提交；同一年份已有真题卷时先提醒', async () => {
    const project = { id: 7, school: '北京师范大学', major: '', subject_code: '808', subject_name: '中国语言文学', stage: 'materials' };
    const old: Schemas['OfficialImportFile'] = { id: 3, kind: 'exam', file_name: '2025 真题.pdf', format: 'pdf', status: 'parsed', question_count: 18, kp_count: 0, reviewed_count: 0, exam_year: 2025, created_at: '2026-11-05T02:00:00Z' };
    expect(sameYearExams([old, { ...old, id: 4, status: 'failed' }, { ...old, id: 5, kind: 'handout' }], [2025, undefined])).toEqual([old]);
    expect(sameYearExams([old], [2024])).toEqual([]);
    g.mockApi = {
      'GET /admin/official/projects': () => ({ status: 200, body: { items: [project] } }),
      'GET /admin/official/projects/{id}/imports': () => ({ status: 200, body: { items: [old] } }),
      'POST /admin/official/projects/{id}/imports': () => ({ status: 200, body: { id: 9, kind: 'exam', file_name: '2025 真题.md', format: 'md', status: 'parsing', question_count: 0, kp_count: 0, reviewed_count: 0 } }),
    };
    vi.mocked(uploadTextbookFile).mockClear();
    wrap(<ImportSection projectId={7} />);
    fireEvent.click(await screen.findByText('+ 上传资料'));
    expect((document.querySelector('input[type=file]') as HTMLInputElement).accept).toContain('.md');
    fireEvent.change(document.querySelector('input[type=file]')!, { target: { files: [new File(['## 一、名词解释\n**1.** 复句'], '2025 真题.md')] } });
    fireEvent.change(await screen.findByRole('spinbutton'), { target: { value: '2025' } });
    fireEvent.click(screen.getByRole('button', { name: /确\s*定/ }));
    expect((await screen.findAllByText('这一年的真题已经传过')).length).toBeGreaterThan(0);
    expect(screen.getByText('2025 年：《2025 真题.pdf》（18 题）')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '继续上传' }));
    await waitFor(() => expect(calls('POST', /\/projects\/7\/imports$/)).toHaveLength(1));
    const body = await calls('POST', /\/projects\/7\/imports$/)[0]!.json();
    expect(body).toEqual(expect.objectContaining({ format: 'md', exam_year: 2025, text: '## 一、名词解释\n**1.** 复句' }));
    expect(body).not.toHaveProperty('size');
    expect(uploadTextbookFile).not.toHaveBeenCalled();
  });

  it('T52 核对区：重复题标红、说明和谁重复，可一键驳回；别的年份考过只提示', async () => {
    const project = { id: 7, school: '北京师范大学', major: '', subject_code: '808', subject_name: '中国语言文学', stage: 'materials' };
    const file = { id: 5, kind: 'exam', file_name: '2025 真题（重传）', format: 'text', status: 'parsed', question_count: 2, kp_count: 0, reviewed_count: 0, fill: { status: 'idle', total: 0, done: 0, failed: 0 } };
    const base = { item_type: 'question', confidence: 'low', status: 'pending', rubric_mismatch: false, needs_fill: false };
    const dup = { ...base, id: 21, seq: 1, payload: { qtype: 'term', stem: '复句', exam_year: 2025, rubric: [{ content: '分句', score: 6 }], score: 6 }, duplicate_of: '与《2025 真题.md》第 1 题相同' };
    const seen = { ...base, id: 22, seq: 2, confidence: 'high', payload: { qtype: 'term', stem: '儒家十三经', exam_year: 2025, rubric: [{ content: '十三部', score: 6 }], score: 6 }, seen_years: [2019, 2023] };
    g.mockApi = {
      'GET /admin/official/projects': () => ({ status: 200, body: { items: [project] } }),
      'GET /admin/official/projects/{id}/imports': () => ({ status: 200, body: { items: [file] } }),
      'GET /admin/official/projects/{id}/imports/{id}': () => ({ status: 200, body: { file, text: '1. 复句\n2. 儒家十三经', items: [dup, seen] } }),
      'POST /admin/official/projects/{id}/imports/{id}/reject-duplicates': () => ({ status: 200, body: { rejected: 1 } }),
    };
    wrap(<ImportSection projectId={7} />);
    fireEvent.click(await screen.findByText('2025 真题（重传）'));
    expect(await screen.findByText('重复：与《2025 真题.md》第 1 题相同')).toBeTruthy();
    expect(screen.getByText('重复')).toBeTruthy();
    fireEvent.click(screen.getByText('2. 2025 · 名词解释：儒家十三经'));
    expect(await screen.findByText('2019、2023 年也考过这道题')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /驳回重复题（1 题）/ }));
    await waitFor(() => expect(calls('POST', /\/projects\/7\/imports\/5\/reject-duplicates$/)).toHaveLength(1));
    expect(await screen.findByText('已驳回 1 道重复题')).toBeTruthy();
  });
  it('批量勾选：批量接受跳过缺采分点和重复的题，批量重新生成只做 AI 答案', async () => {
    const project = { id: 7, school: '北京师范大学', major: '', subject_code: '708', subject_name: '中国语言文学综合', stage: 'materials' };
    const file = { id: 5, kind: 'exam', file_name: '北师大708真题', format: 'text', status: 'parsed', question_count: 4, kp_count: 0, reviewed_count: 0, fill: { status: 'idle', total: 0, done: 0, failed: 0 } };
    const base = { item_type: 'question', confidence: 'medium', status: 'pending', rubric_mismatch: false, needs_fill: false };
    const ok = (id: number, stem: string, more: object = {}) => ({ ...base, id, seq: id, payload: { qtype: 'term', stem, exam_year: 2026, score: 6, rubric: [{ content: stem, score: 6 }], answer: stem, answer_ai: true }, answer_ai: true, ...more });
    const items = [
      ok(1, '组合关系', { answer_basis: { reason: 'not_relevant', message: '检索到的教材段落与本题不相关，答案按通用知识生成' } }),
      ok(2, '聚合关系'),
      { ...base, id: 3, seq: 3, payload: { qtype: 'term', stem: '语素', exam_year: 2026, rubric: [] }, needs_fill: true },
      ok(4, '音位', { duplicate_of: '与第 9 题相同' }),
    ];
    g.mockApi = {
      'GET /admin/official/projects': () => ({ status: 200, body: { items: [project] } }),
      'GET /admin/official/projects/{id}/imports': () => ({ status: 200, body: { items: [file] } }),
      'GET /admin/official/projects/{id}/imports/{id}': () => ({ status: 200, body: { file, text: '1. 组合关系', items } }),
      'POST /admin/official/projects/{id}/import-items/{id}/review': () => ({ status: 200, body: { draft_id: 1 } }),
      'POST /admin/official/projects/{id}/import-items/{id}/regenerate': (req) => {
        const id = Number(/import-items\/(\d+)/.exec(new URL(req.url).pathname)?.[1]);
        return id === 2 ? { status: 502, body: { code: 'AI_FAILED', message: 'AI 暂时不可用' } } : { status: 200, body: items.find((i) => i.id === id) };
      },
    };
    wrap(<ImportSection projectId={7} />);
    fireEvent.click(await screen.findByText('北师大708真题'));
    fireEvent.click(await screen.findByRole('checkbox', { name: /全选待核对/ }));
    expect(screen.getByText(/已选 4/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /批量接受（2）/ }));
    expect((await screen.findAllByText('接受 2 道题并提交审核？')).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/另有 2 道/).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/其中 1 道的 AI 答案没有引用教材/).length).toBeGreaterThan(0);
    fireEvent.click(screen.getAllByRole('button', { name: '批量接受' })[0]!);
    await waitFor(() => expect(calls('POST', /\/import-items\/\d+\/review$/)).toHaveLength(2));
    const reviewed = calls('POST', /\/import-items\/\d+\/review$/).map((r) => /import-items\/(\d+)/.exec(new URL(r.url).pathname)?.[1]);
    expect(reviewed).toEqual(['1', '2']);
    expect(await screen.findByText('批量接受：2 题都已完成')).toBeTruthy();

    // 接受过的题取消勾选；还勾着的 3、4 题里只有 4 是 AI 答案
    fireEvent.click(screen.getByRole('checkbox', { name: '选择第 2 题' }));
    fireEvent.click(screen.getByRole('button', { name: /批量重新生成（2）/ }));
    fireEvent.click((await screen.findAllByRole('button', { name: '重新生成' }))[0]!);
    await waitFor(() => expect(calls('POST', /\/import-items\/\d+\/regenerate$/)).toHaveLength(2));
    expect((await screen.findAllByText('批量重新生成：1 题成功，1 题失败')).length).toBeGreaterThan(0);
    expect(screen.getAllByText('第 2 题：AI 暂时不可用').length).toBeGreaterThan(0);
  });

  it('删除导入的资料：确认后调用删除接口，打开的核对区随之关掉；已发布的不能删时提示原因', async () => {
    const project = { id: 7, school: '北京师范大学', major: '', subject_code: '808', subject_name: '中国语言文学', stage: 'materials' };
    const file = (id: number, name: string) => ({ id, kind: 'exam', file_name: name, format: 'text', status: 'parsed', question_count: 1, kp_count: 0, reviewed_count: 0, fill: { status: 'idle', total: 0, done: 0, failed: 0 } });
    let files = [file(5, '2025 真题'), file(6, '2024 真题')];
    g.mockApi = {
      'GET /admin/official/projects': () => ({ status: 200, body: { items: [project] } }),
      'GET /admin/official/projects/{id}/imports': () => ({ status: 200, body: { items: files } }),
      'GET /admin/official/projects/{id}/imports/{id}': () => ({ status: 200, body: { file: files[0], text: '1. 复句', items: [] } }),
      'DELETE /admin/official/projects/{id}/imports/{id}': (req) => {
        const id = Number(/imports\/(\d+)/.exec(new URL(req.url).pathname)?.[1]);
        if (id === 6) return { status: 409, body: { code: 'CONFLICT', message: '这份资料里有 1 道题已经发布到题库，不能删除' } };
        files = files.filter((f) => f.id !== id);
        return { status: 204 };
      },
    };
    wrap(<ImportSection projectId={7} />);
    fireEvent.click(await screen.findByText('2025 真题'));
    expect(await screen.findByText('1. 复句')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '删除2025 真题' }));
    expect((await screen.findAllByText('删除「2025 真题」？')).length).toBeGreaterThan(0);
    fireEvent.click(screen.getAllByRole('button', { name: /删\s*除/ }).at(-1)!);
    await waitFor(() => expect(calls('DELETE', /\/projects\/7\/imports\/5$/)).toHaveLength(1));
    await waitFor(() => expect(screen.queryByText('2025 真题')).toBeNull());
    expect(screen.getByText('点一份已解析的资料开始核对')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: '删除2024 真题' }));
    expect((await screen.findAllByText('删除「2024 真题」？')).length).toBeGreaterThan(0);
    fireEvent.click(screen.getAllByRole('button', { name: /删\s*除/ }).at(-1)!);
    expect(await screen.findByText('这份资料里有 1 道题已经发布到题库，不能删除')).toBeTruthy();
    expect(screen.getByText('2024 真题')).toBeTruthy();
  });

  it('T55 引用出处标明考纲、经验总结', () => {
    const base = { reference_id: 1, title: '北师大808考纲', heading: '文学理论' };
    expect(citationSource({ ...base, material: 'syllabus' })).toBe('对应考纲 · 北师大808考纲 · 文学理论');
    expect(citationSource({ ...base, title: '808 上岸经验', heading: '名词解释', material: 'experience' })).toBe('经验总结 · 808 上岸经验 · 名词解释');
    expect(citationSource({ ...base, title: '文学理论教程', heading: '第八章', printed_page: '120', material: 'textbook' })).toBe('文学理论教程 · 第八章 · 第 120 页');
  });

  it('T53 勾了 AI 补答案、项目没有能检索的关联教材时先提醒', async () => {
    expect(hasUsableTextbook([])).toBe(false);
    expect(hasUsableTextbook([{ available: false }, { available: true }])).toBe(true);
    const project = { id: 7, school: '北京师范大学', major: '', subject_code: '808', subject_name: '中国语言文学', stage: 'materials' };
    g.mockApi = {
      'GET /admin/official/projects': () => ({ status: 200, body: { items: [project] } }),
      'GET /admin/official/projects/{id}/imports': () => ({ status: 200, body: { items: [] } }),
      'GET /admin/official/projects/{id}/textbooks': () => ({ status: 200, body: { items: [], version: 1 } }),
      'POST /admin/official/projects/{id}/imports': () => ({ status: 200, body: { id: 1, kind: 'exam', file_name: '粘贴的内容', format: 'text', status: 'parsing', question_count: 0, kp_count: 0, reviewed_count: 0 } }),
    };
    wrap(<ImportSection projectId={7} />);
    fireEvent.click(await screen.findByText('+ 上传资料'));
    fireEvent.change(screen.getByPlaceholderText(/没有文件时可以粘贴真题或讲义原文/), { target: { value: '一、名词解释\n1. 复句' } });
    fireEvent.click(screen.getByText('缺答案时由 AI 生成'));
    fireEvent.click(screen.getByRole('button', { name: /确\s*定/ }));
    expect((await screen.findAllByText('本项目还没有关联教材')).length).toBeGreaterThan(0);
    expect(calls('POST', /\/projects\/7\/imports$/)).toHaveLength(0);
    fireEvent.click(screen.getByRole('button', { name: '仍然上传' }));
    await waitFor(() => expect(calls('POST', /\/projects\/7\/imports$/)).toHaveLength(1));
  });

  it('T53 核对区：说明没引用教材的原因；可逐题或整份按关联教材重新生成', async () => {
    const project = { id: 7, school: '北京师范大学', major: '', subject_code: '808', subject_name: '中国语言文学', stage: 'materials' };
    const file = { id: 6, kind: 'exam', file_name: '北师大808真题.md', format: 'md', status: 'parsed', question_count: 1, kp_count: 0, reviewed_count: 0, fill: { status: 'idle', total: 0, done: 0, failed: 0 } };
    const item = {
      id: 31, item_type: 'question', seq: 1, confidence: 'medium', status: 'pending', rubric_mismatch: false, needs_fill: false, answer_ai: true,
      payload: { qtype: 'term', stem: '意境', exam_year: 2025, score: 5, answer_ai: true, answer: '意境是情景交融的艺术境界。', rubric: [{ content: '情景交融', score: 5 }] },
      answer_basis: { reason: 'unavailable', message: '关联的教材都不能检索，答案按通用知识生成', details: ['文学理论教程 第五版：还没有可检索的索引'] },
    };
    const cited = { ...item, answer_basis: undefined, citations: [{ reference_id: 1, title: '文学理论教程', heading: '第八章 文学意境', kind: 'official_textbook', quote: '意境是情景交融。', claim: '情景交融' }] };
    g.mockApi = {
      'GET /admin/official/projects': () => ({ status: 200, body: { items: [project] } }),
      'GET /admin/official/projects/{id}/imports': () => ({ status: 200, body: { items: [file] } }),
      'GET /admin/official/projects/{id}/imports/{id}': () => ({ status: 200, body: { file, text: '1. 意境', items: [item] } }),
      'POST /admin/official/projects/{id}/import-items/{id}/regenerate': () => ({ status: 200, body: cited }),
      'POST /admin/official/projects/{id}/imports/{id}/regenerate': () => ({ status: 200, body: { ...file, fill: { status: 'running', total: 1, done: 0, failed: 0 } } }),
    };
    wrap(<ImportSection projectId={7} />);
    fireEvent.click(await screen.findByText('北师大808真题.md'));
    expect(await screen.findByText('没有引用教材：关联的教材都不能检索，答案按通用知识生成')).toBeTruthy();
    expect(screen.getByText('文学理论教程 第五版：还没有可检索的索引')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '按关联教材重新生成' }));
    await waitFor(() => expect(calls('POST', /\/projects\/7\/import-items\/31\/regenerate$/)).toHaveLength(1));
    expect(await screen.findByText('已重新生成，答案引用了关联教材，请核对')).toBeTruthy();
    expect(screen.queryByText(/没有引用教材：/)).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: /按关联教材重新生成（1 题 AI 答案）/ }));
    fireEvent.click((await screen.findAllByRole('button', { name: '重新生成' }))[0]!);
    await waitFor(() => expect(calls('POST', /\/projects\/7\/imports\/6\/regenerate$/)).toHaveLength(1));
  });
});
