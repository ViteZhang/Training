import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import ExamProfileScreen from '../../app/bank/profile';
import { EssayKnowledgeBase } from '../features/bank/EssayKB';
import { layoutGraph, radiusOf } from '../features/bank/graphLayout';

const mockPush = jest.fn();
let mockParams: Record<string, string> = {};
jest.mock('expo-router', () => ({
  router: { push: (...a: unknown[]) => mockPush(...a), replace: jest.fn(), back: jest.fn() },
  useLocalSearchParams: () => mockParams,
}));
jest.mock('expo-crypto', () => ({ randomUUID: () => 'uuid' }));

const mockCalls: { method: string; path: string; init?: { body?: unknown } }[] = [];
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

describe('图谱布局', () => {
  it('确定性、落在画布内，200 个节点很快算完', () => {
    const nodes = Array.from({ length: 200 }, (_, i) => ({ id: i + 1, sectionId: (i % 5) + 1, examCount: i % 4 }));
    const edges = Array.from({ length: 199 }, (_, i) => ({ source: i + 1, target: i + 2 }));
    const t = Date.now();
    const a = layoutGraph(nodes, edges, 800);
    const took = Date.now() - t;
    const b = layoutGraph(nodes, edges, 800);
    expect(a.size).toBe(200);
    for (const [id, p] of a) {
      expect(p.x).toBeGreaterThanOrEqual(0);
      expect(p.x).toBeLessThanOrEqual(800);
      expect(p.y).toBeGreaterThanOrEqual(0);
      expect(p.y).toBeLessThanOrEqual(800);
      expect(b.get(id)).toEqual(p);
    }
    expect(took).toBeLessThan(3000);
    expect(radiusOf(0)).toBeLessThan(radiusOf(3));
    expect(radiusOf(100)).toBe(radiusOf(6));
    expect(layoutGraph([], []).size).toBe(0);
  });
});

describe('3.8 考情分析', () => {
  it('不足 2 套真题时提示再导入', async () => {
    mockParams = { subjectId: '7' };
    mockResponses['GET /subjects'] = () => ({ status: 200, data: { items: [], max_subjects: 3, can_add: true } });
    mockResponses['GET /subjects/{subjectId}/exam-profile'] = () => ({
      status: 200,
      data: { ready: false, paper_count: 1, min_papers: 2, years: [2024], excluded_count: 0, structure: [], sections: [], high_freq: [], missing_sections: [], style_tags: [], basis: [] },
    });
    await wrap(<ExamProfileScreen />);
    expect(await screen.findByText('目前只有 1 套真题')).toBeTruthy();
    await fireEvent.press(screen.getByText('导入真题'));
    expect(mockPush).toHaveBeenCalledWith({ pathname: '/import', params: { subjectId: '7' } });
  });

  it('显示结构与用时、板块占比、缺资料提醒、高频考点和风格', async () => {
    mockParams = { subjectId: '7' };
    mockResponses['GET /subjects'] = () => ({ status: 200, data: { items: [{ id: 7, name: '语言文学基础', code: '654', full_score: 150, is_essay: false, bank_id: 1, question_count: 0, kp_count: 0, material_count: 0 }], max_subjects: 3, can_add: true } });
    mockResponses['GET /subjects/{subjectId}/exam-profile'] = () => ({
      status: 200,
      data: {
        ready: true,
        paper_count: 10,
        min_papers: 2,
        years: [2024, 2015],
        excluded_count: 8,
        structure: [{ qtype: 'term', count: 6, score_each: 5, total: 30, suggested_minutes: 25 }],
        stable_years: 5,
        changed_years: [],
        total_minutes: 180,
        check_minutes: 15,
        sections: [{ id: 1, name: '外国文学', share: 0.15, mastery: 12, kp_count: 8 }],
        high_freq: [{ kp_id: 9, name: '意境', path: ['文学理论'], exam_count: 3, state: 'consolidating' }],
        high_freq_total: 38,
        missing_sections: [{ id: 1, name: '外国文学', share: 0.15, mastery: 12, kp_count: 8 }],
        style_tags: ['偏原文表述', '论述跨板块'],
        basis: [{ material_id: 3, file_name: '654 历年真题汇编.pdf' }],
      },
    });
    await wrap(<ExamProfileScreen />);
    expect(await screen.findByText('654 语言文学基础怎么考')).toBeTruthy();
    expect(screen.getByText('依据：654 历年真题汇编.pdf 中 2015–2024 年 10 套')).toBeTruthy();
    expect(screen.getByText('近 5 年未变')).toBeTruthy();
    expect(screen.getByText('25′')).toBeTruthy();
    expect(screen.getByText('外国文学占 15% 分值，你的资料里只有 8 个知识点、掌握度 12%。可以补一份外国文学的讲义或笔记')).toBeTruthy();
    expect(screen.getByText('偏原文表述')).toBeTruthy();
    expect(screen.getByText(/另有 8 道回忆版/)).toBeTruthy();
  });
});

describe('3.10 作文知识库', () => {
  it('写作方法、素材、范文与收藏', async () => {
    mockResponses['GET /subjects/{subjectId}/essay-kb'] = () => ({
      status: 200,
      data: {
        rubric: { id: 1, name: '908 评分细则', full_score: 150, source: 'user_material', dimensions: [{ name: '立意', score: 40 }] },
        methods: [{ id: 1, title: '审题与立意', content: '先找关键词', origin: 'ai_extracted', state: 'learning', dimension: '立意', source: { material_id: 3, file_name: '写作笔记.docx', page: 2 } }],
        materials: [{ id: 5, theme: '创新', content: '屠呦呦提取青蒿素', origin: 'ai_extracted', favorite: false, exam_count: 2 }],
        model_essays: [{ id: 8, title: '守正方能出新', topic: '守正与创新', structure: { opening: '守正是根基', points: ['传统'], ending: '不迷失' } }],
        topics: [{ id: 2, stem: '以「守正与创新」为题', exam_year: 2024, required_words: 800, model_essay_count: 1 }],
      },
    });
    mockResponses['GET /subjects/{subjectId}/materials'] = () => ({ status: 200, data: { items: [] } });
    mockResponses['PUT /essay-materials/{essayMaterialId}/favorite'] = () => ({ status: 204 });
    await wrap(<EssayKnowledgeBase subjectId={7} label="908 作文" />);
    expect(await screen.findByText('审题与立意')).toBeTruthy();
    expect(screen.getByText('立意 · 出自写作笔记 第 2 页')).toBeTruthy();
    expect(screen.getByText('1 条 · 真题考过 2 次')).toBeTruthy();
    await fireEvent.press(screen.getByText('创新'));
    await fireEvent.press(screen.getByText('收藏'));
    await waitFor(() => expect(mockCalls.find((c) => c.method === 'PUT')?.init?.body).toEqual({ favorite: true }));
    await fireEvent.press(screen.getByText('看结构拆解'));
    expect(screen.getByText('开头立意：守正是根基')).toBeTruthy();
    await fireEvent.press(screen.getByText('作文题 1'));
    expect(screen.getByText('2024 真题 · 不少于 800 字 · 附 1 篇范文')).toBeTruthy();
  });
});
