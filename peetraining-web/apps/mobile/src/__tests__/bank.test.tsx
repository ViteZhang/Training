import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import KPCard from '../../app/bank/kp/[id]';
import QuestionScreen from '../../app/bank/question/[id]';
import SearchScreen from '../../app/bank/search';
import { materialMeta, type Material } from '../features/bank/api';
import { Marked } from '../features/bank/Marked';
import { MaterialList } from '../features/bank/MaterialList';
import { QuestionList } from '../features/bank/QuestionList';
import { KnowledgeTree } from '../features/bank/Tree';

const mockPush = jest.fn();
let mockParams: Record<string, string> = {};
jest.mock('expo-router', () => ({
  router: { push: (...a: unknown[]) => mockPush(...a), replace: jest.fn(), back: jest.fn() },
  useLocalSearchParams: () => mockParams,
}));
jest.mock('expo-crypto', () => ({ randomUUID: () => 'uuid' }));

const mockCalls: { method: string; path: string; init?: { body?: unknown; params?: { query?: Record<string, unknown> } } }[] = [];
let mockResponses: Record<string, () => unknown> = {};
jest.mock('../lib/api', () => {
  const actual = jest.requireActual('@training/api-client');
  const m = (method: string) => (path: string, init?: { body?: unknown }) => {
    mockCalls.push({ method, path, init });
    const r = mockResponses[`${method} ${path}`];
    const { status, data, error } = (r ? r() : { status: 200, data: {} }) as { status: number; data?: unknown; error?: unknown };
    return Promise.resolve({ data, error, response: new Response(null, { status }) });
  };
  return { api: { GET: m('GET'), POST: m('POST'), PUT: m('PUT'), PATCH: m('PATCH'), DELETE: m('DELETE') }, unwrap: actual.unwrap };
});

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const metrics = { frame: { x: 0, y: 0, width: 390, height: 844 }, insets: { top: 0, left: 0, right: 0, bottom: 0 } };
  return render(
    <SafeAreaProvider initialMetrics={metrics}>
      <QueryClientProvider client={qc}>{ui}</QueryClientProvider>
    </SafeAreaProvider>,
  );
}

beforeEach(() => {
  mockCalls.length = 0;
  mockResponses = {};
  mockParams = {};
  mockPush.mockReset();
});

const kp = {
  id: 5,
  level: 'point',
  name: '意境',
  path: ['文学理论', '第三章'],
  origin: 'ai_extracted',
  exam_count: 3,
  mastery: { m: 46, state: 'consolidating' },
  original_text: '意境是情景交融、虚实相生的诗意空间。',
  source: { material_id: 9, file_name: '讲义.docx', page: 3 },
  rubric_points: [{ id: 1, seq: 1, content: '情景交融', origin: 'ai_extracted', keywords: ['情景交融'] }],
  ai_explanation: '诗人把情感放进景物里……',
  related_questions: [{ id: 11, qtype: 'term', stem: '意境', source: 'exam', exam_year: 2024 }],
  needs_review: false,
};

describe('3.1 知识点树', () => {
  it('板块显示知识点数、掌握度与短板，展开后能进入知识点卡片', async () => {
    const sections = [
      {
        id: 1,
        level: 'section' as const,
        name: '外国文学',
        kp_count: 8,
        consolidating_count: 0,
        avg_mastery: 12,
        is_weak: true,
        children: [{ id: 2, level: 'chapter' as const, name: '第一章', children: [{ id: 3, level: 'point' as const, name: '荷马史诗', exam_count: 2, state: 'learning' as const, children: [] }] }],
      },
    ];
    await render(<KnowledgeTree sections={sections} expandAll={false} subjectId={7} />);
    expect(screen.getByText('8 个知识点 · 短板')).toBeTruthy();
    expect(screen.getByText('12%')).toBeTruthy();
    expect(screen.queryByText('荷马史诗')).toBeNull();
    await fireEvent.press(screen.getByText('外国文学'));
    expect(screen.getByText('真题 2 次')).toBeTruthy();
    await fireEvent.press(screen.getByText('荷马史诗'));
    expect(mockPush).toHaveBeenCalledWith({ pathname: '/bank/kp/[id]', params: { id: '3', subjectId: '7' } });
  });
});

describe('3.4 知识点卡片', () => {
  it('显示原文、出处、采分点、AI 解读与相关题目；自评后更新', async () => {
    mockParams = { id: '5', subjectId: '7' };
    mockResponses['GET /knowledge-points/{kpId}'] = () => ({ status: 200, data: kp });
    mockResponses['PUT /knowledge-points/{kpId}/self-assessment'] = () => ({ status: 200, data: { m: 30, state: 'learning', last_self_assess: 'vague' } });
    await wrap(<KPCard />);
    expect(await screen.findByText('真题出现 3 次')).toBeTruthy();
    expect(screen.getByText('出自：讲义.docx · 第 3 页')).toBeTruthy();
    expect(screen.getByText('AI 生成')).toBeTruthy();
    expect(screen.getByText('名词解释：意境')).toBeTruthy();
    await fireEvent.press(screen.getByText('查看原文'));
    expect(mockPush).toHaveBeenCalledWith({ pathname: '/bank/page', params: { materialId: '9', page: '3', highlight: kp.original_text } });
    await fireEvent.press(screen.getByText('模糊'));
    await waitFor(() => expect(mockCalls.some((c) => c.path === '/knowledge-points/{kpId}/self-assessment')).toBe(true));
    expect(await screen.findByText('学习中')).toBeTruthy();
  });

  it('更多操作：拆分', async () => {
    mockParams = { id: '5', subjectId: '7' };
    mockResponses['GET /knowledge-points/{kpId}'] = () => ({ status: 200, data: kp });
    mockResponses['POST /knowledge-points/{kpId}/split'] = () => ({ status: 200, data: { items: [] } });
    await wrap(<KPCard />);
    await fireEvent.press(await screen.findByText('更多'));
    await fireEvent.press(screen.getByText('拆分为多个知识点'));
    await fireEvent.changeText(screen.getByLabelText('第 1 个知识点'), '意');
    await fireEvent.changeText(screen.getByLabelText('第 2 个知识点'), '境');
    await fireEvent.press(screen.getByText('拆分'));
    await waitFor(() => {
      const call = mockCalls.find((c) => c.path === '/knowledge-points/{kpId}/split');
      expect(call?.init?.body).toEqual({ parts: [{ name: '意' }, { name: '境' }] });
    });
  });
});

describe('3.1b 题目与 3.3 详情', () => {
  it('题型筛选带数量，点题型重新查询', async () => {
    mockResponses['GET /subjects/{subjectId}/questions'] = () => ({
      status: 200,
      data: {
        items: [{ id: 1, qtype: 'term', stem: '意境', score: 5, source: 'exam', exam_year: 2024, status_tag: 'wrong', last_score: 3, attempt_count: 2, path: ['文学理论'] }],
        facets: { by_qtype: { term: 6, short_answer: 4 }, by_source: { exam: 10 }, exam_years: [2024] },
      },
    });
    await wrap(<QuestionList subjectId={7} />);
    expect(await screen.findByText('名词解释 6')).toBeTruthy();
    expect(screen.getByText('3 / 5')).toBeTruthy();
    expect(screen.getByText('文学理论 · 做过 2 次')).toBeTruthy();
    await fireEvent.press(screen.getByText('简答 4'));
    await waitFor(() => expect(mockCalls.some((c) => c.init?.params?.query?.qtype === 'short_answer')).toBe(true));
  });

  it('详情显示作答记录与遗漏的采分点；编辑时采分点合计不对不能保存', async () => {
    mockParams = { id: '11' };
    mockResponses['GET /questions/{questionId}'] = () => ({
      status: 200,
      data: {
        id: 11,
        qtype: 'term',
        stem: '意境',
        score: 5,
        source: 'exam',
        exam_year: 2024,
        answer: '情景交融',
        answer_origin: 'imported',
        origin_tags: [],
        rubric_points: [{ id: 1, seq: 1, content: '情景交融', score: 5, origin: 'user_confirmed' }],
        rubric_version: 1,
        knowledge_points: [{ id: 5, name: '意境', is_primary: true }],
        attempts: [{ attempt_id: 1, answered_at: '2026-09-27T02:00:00Z', score: 3, full_score: 5, missed_points: ['虚实相生'], loss_types: ['norm'] }],
        in_wrong_book: true,
        needs_review: false,
      },
    });
    await wrap(<QuestionScreen />);
    expect(await screen.findByText('遗漏「虚实相生」 · 答题不规范')).toBeTruthy();
    expect(screen.getByText('已确认 · 合计 5 分')).toBeTruthy();
    await fireEvent.press(screen.getByText('编辑'));
    await fireEvent.changeText(screen.getByLabelText('第 1 个采分点的分值'), '2');
    expect(screen.getByText('采分点分值合计要等于题目分值 5 分')).toBeTruthy();
  });
});

describe('3.1c 资料与 3.1d 删除', () => {
  const m = (extra: Partial<Material>): Material => ({
    id: 3,
    subject_id: 7,
    file_name: '真题.pdf',
    format: 'pdf',
    status: 'parsed',
    page_count: 86,
    billed_pages: 86,
    question_count: 128,
    kp_count: 0,
    created_at: '2026-09-26T02:00:00Z',
    ...extra,
  });
  it('资料说明：识别不完整时提醒', () => {
    expect(materialMeta(m({ paper_count: 10 }))).toBe('86 页 · 识别出 128 题 · 10 套真题卷');
    expect(materialMeta(m({ format: 'image', page_count: 3, status: 'partial', question_count: 4 }))).toBe('3 张 · 只识别出 4 题，可能有遗漏');
    expect(materialMeta(m({ status: 'parsing' }))).toBe('86 页 · 整理中');
  });
  it('删除前说明连带影响，确认后删除', async () => {
    mockResponses['GET /materials/{materialId}/deletion-impact'] = () => ({
      status: 200,
      data: { question_count: 128, attempt_count: 5, wrong_count: 1, kp_delete_count: 3, kp_keep_count: 2, paper_session_count: 1 },
    });
    mockResponses['DELETE /materials/{materialId}'] = () => ({ status: 204 });
    await wrap(<MaterialList items={[m({})]} subjectId={7} />);
    await fireEvent.press(screen.getByText('删除'));
    expect(await screen.findByText('· 从它识别出的 128 道题会一起删除，包括作答记录和错题')).toBeTruthy();
    expect(await screen.findByText('· 只来自这份资料的知识点会删除（3 个），其他资料里也有的会保留')).toBeTruthy();
    const buttons = screen.getAllByText('删除');
    await fireEvent.press(buttons[buttons.length - 1]!);
    await waitFor(() => expect(mockCalls.some((c) => c.method === 'DELETE' && c.path === '/materials/{materialId}')).toBe(true));
  });
});

describe('3.2 搜索与 3.7 原文', () => {
  it('结果分三组并高亮', async () => {
    mockParams = { subjectId: '7' };
    mockResponses['GET /subjects/{subjectId}/search'] = () => ({
      status: 200,
      data: {
        knowledge_points: [{ id: 5, name: '意境', path: ['文学理论'], hit: { text: '意境', highlights: [{ start: 0, end: 2 }] } }],
        questions: [],
        material_pages: [{ material_id: 9, file_name: '讲义.docx', page_no: 3, hit: { text: '…诗意空间，意境…', highlights: [{ start: 6, end: 8 }] } }],
      },
    });
    await wrap(<SearchScreen />);
    await fireEvent.changeText(screen.getByLabelText('搜索'), '意境');
    await fireEvent(screen.getByLabelText('搜索'), 'submitEditing');
    expect(await screen.findByText('知识点 · 1')).toBeTruthy();
    expect(screen.getByText('讲义.docx · 第 3 页')).toBeTruthy();
    expect(screen.queryByText(/题目 ·/)).toBeNull();
  });

  it('Marked 按码点分段高亮', async () => {
    await render(<Marked text="ab情景交融cd" highlights={[{ start: 2, end: 6 }]} low={[{ start: 7, end: 8 }]} />);
    expect(screen.getByText('情景交融')).toBeTruthy();
    expect(screen.getByText('d')).toBeTruthy();
  });
});
