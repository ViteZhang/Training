import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import EssayBookPage from '../../app/essay/book';
import EssayHomePage from '../../app/essay/index';
import ModelEssayPage from '../../app/essay/model/[id]';
import EssayResultPage from '../../app/essay/result/[id]';
import EssayRubricPage from '../../app/essay/rubric';
import EssayWritePage from '../../app/essay/write/[id]';
import { getJSON, setJSON, storage } from '../lib/storage';

const mockPush = jest.fn();
const mockReplace = jest.fn();
let mockParams: Record<string, string> = {};
jest.mock('expo-router', () => ({
  router: { push: (...a: unknown[]) => mockPush(...a), replace: (...a: unknown[]) => mockReplace(...a), back: jest.fn() },
  useLocalSearchParams: () => mockParams,
  Redirect: () => null,
}));
let mockUuid = 0;
jest.mock('expo-crypto', () => ({ randomUUID: () => `uuid-${++mockUuid}` }));
jest.mock('expo-file-system', () => ({
  File: class {
    uri: string;
    size = 2048;
    constructor(uri: string) {
      this.uri = uri;
    }
    upload = async () => ({ status: 200 });
  },
}));
jest.mock('expo-image-picker', () => ({
  requestCameraPermissionsAsync: async () => ({ granted: true, canAskAgain: true }),
  launchCameraAsync: async () => ({ canceled: false, assets: [{ uri: 'file:///shot.jpg', mimeType: 'image/jpeg', fileSize: 2048 }] }),
}));

const mockCalls: { method: string; path: string; init?: unknown }[] = [];
let mockResponses: Record<string, () => unknown> = {};
function mockRespond(method: string, path: string, init?: unknown) {
  mockCalls.push({ method, path, init });
  const r = mockResponses[`${method} ${path}`];
  const v = r ? r() : { status: 200, data: {} };
  const { status, data, error } = v as { status: number; data?: unknown; error?: unknown };
  return Promise.resolve({ data, error, response: new Response(null, { status }) });
}
jest.mock('../lib/api', () => {
  const actual = jest.requireActual('@training/api-client');
  const m = (method: string) => (path: string, init?: unknown) => mockRespond(method, path, init);
  return { api: { GET: m('GET'), POST: m('POST'), PUT: m('PUT'), PATCH: m('PATCH'), DELETE: m('DELETE') }, unwrap: actual.unwrap };
});

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, refetchInterval: false } } });
  const metrics = { frame: { x: 0, y: 0, width: 390, height: 844 }, insets: { top: 0, left: 0, right: 0, bottom: 0 } };
  return render(
    <SafeAreaProvider initialMetrics={metrics}>
      <QueryClientProvider client={qc}>{ui}</QueryClientProvider>
    </SafeAreaProvider>,
  );
}

const generic = { id: 1, name: '通用五维度标准', source: 'generic', full_score: 150 };
const home = (over: object = {}) => ({
  weekly_goal: 2, week_done: 1, avg_score: 112, rubric: { id: 9, name: '学校评分细则', source: 'user_material', full_score: 150 }, no_material: false,
  weekly_remaining: 0, ai_topics: [], drafts: [],
  exam_topics: [{ id: 30, stem: '以「慢」为题，写一篇不少于 1000 字的文章，文体不限', exam_year: 2024, required_words: 1000, drafts: 2, best: 112, model_essay_count: 2 }],
  ...over,
});
const essay = (over: object = {}) => ({
  id: 70, subject_id: 8, topic_source: 'exam', topic_question_id: 30, topic: '以「慢」为题，写一篇不少于 1000 字的文章', required_words: 1000, time_limit_minutes: 60,
  draft_no: 1, content: '服务器上的草稿', photo_keys: [], word_count: 7, timed: true, duration_seconds: 0, status: 'draft', counts_for_estimate: false,
  disputed: false, created_at: '2026-10-02T08:00:00Z', ...over,
});
const graded = (over: object = {}) =>
  essay({
    status: 'graded', score: 112, full_score: 150, prev_delta: 4, word_count: 1126, counts_for_estimate: true,
    rubric: { id: 9, name: '学校评分细则', source: 'user_material', full_score: 150 },
    dimensions: [{ name: '立意', score: 22, max: 30, comment: '切题' }, { name: '内容与论证', score: 20, max: 30 }],
    weakest_dimension: '内容与论证', thesis: '慢是对事物本身的尊重。', highlights: ['开头意象具体'], problems: ['中段事例并列'], suggestions: ['第 3 段后加一层转折'],
    paragraphs: ['外婆煮粥从不用高压锅。', '后来我去城里读书，生活变得越来越快，每个人都很忙碌，我也渐渐忘了那碗粥的味道。'],
    annotations: [{ paragraph: 2, quote: '生活变得越来越快，每个人都很忙碌', issue: '表述空泛', suggestion: '换成一个具体画面' }],
    model_essays: [{ id: 5, title: '慢中见真', structure: { opening: '开门见山', points: ['快的代价', '慢的价值'] } }],
    ...over,
  });

beforeEach(() => {
  mockCalls.length = 0;
  mockPush.mockReset();
  mockReplace.mockReset();
  storage.clearAll();
  mockParams = { subjectId: '8' };
  mockResponses = {};
});

describe('5.1 作文训练', () => {
  it('本周目标、评分标准；真题题目显示稿数、最高分与范文数，去写带上题目', async () => {
    mockResponses['GET /subjects/{subjectId}/essay-home'] = () => ({ status: 200, data: home() });
    mockResponses['POST /essays'] = () => ({ status: 201, data: essay() });
    const r = await wrap(<EssayHomePage />);
    expect(await screen.findByText('本周目标 2 篇')).toBeTruthy();
    expect(screen.getByText('已完成 1 · 平均 112 分')).toBeTruthy();
    expect(screen.getByText('本周还能批改 0 篇')).toBeTruthy();
    expect(screen.getByText('评分标准：按你资料里的评分细则（满分 150）')).toBeTruthy();
    expect(screen.getByText('已写 2 稿 · 最高 112 分 · 附 2 篇你导入的范文')).toBeTruthy();
    await fireEvent.press(screen.getAllByLabelText(/^去写：/)[0]!);
    await waitFor(() => expect(mockPush).toHaveBeenCalledWith({ pathname: '/essay/write/[id]', params: { id: '70' } }));
    expect(mockCalls.find((c) => c.path === '/essays')?.init).toMatchObject({ body: { subject_id: 8, topic_source: 'exam', question_id: 30, idempotency_key: 'uuid-1' } });
    await r.unmount();
  });

  it('5.1b 没有资料：引导导入，AI 命题再出一道，自拟题目开始写', async () => {
    let topics: object[] = [];
    mockResponses['GET /subjects/{subjectId}/essay-home'] = () => ({
      status: 200,
      data: home({ no_material: true, exam_topics: [], rubric: generic, avg_score: undefined, week_done: 0, ai_topics: topics }),
    });
    mockResponses['POST /subjects/{subjectId}/essay-topics'] = () => {
      topics = [{ id: 3, topic: '以「留白」为题，写一篇文章', required_words: 800, drafts: 0, created_at: '2026-10-02T08:00:00Z' }];
      return { status: 201, data: topics[0] };
    };
    mockResponses['POST /essays'] = () => ({ status: 201, data: essay({ id: 71 }) });
    const r = await wrap(<EssayHomePage />);
    expect(await screen.findByText('导入作文资料，练得更准')).toBeTruthy();
    expect(screen.getByText('评分标准：通用五维度（分数只作参考）')).toBeTruthy();
    await fireEvent.press(screen.getByText('AI 出一道题'));
    expect(await screen.findByText('以「留白」为题，写一篇文章')).toBeTruthy();
    expect(screen.getByText('AI 出题')).toBeTruthy();
    await fireEvent.press(screen.getByText('自拟题目'));
    await fireEvent.changeText(screen.getByLabelText('自拟题目'), '以「家」为题');
    await fireEvent.press(screen.getByText('开始写'));
    await waitFor(() => expect(mockCalls.find((c) => c.path === '/essays')?.init).toMatchObject({ body: { topic_source: 'custom', topic_text: '以「家」为题' } }));
    await r.unmount();
  });
});

describe('5.2 写作文', () => {
  beforeEach(() => {
    mockParams = { id: '70' };
  });

  it('本地草稿优先、真题倒计时可关闭、插入素材、提交后进入批改中', async () => {
    setJSON('draft:essay:70', { content: '本地还没同步的正文', seconds: 120 });
    mockResponses['GET /essays/{essayId}'] = () => ({ status: 200, data: essay() });
    mockResponses['GET /subjects/{subjectId}/essay-kb'] = () => ({
      status: 200,
      data: { methods: [], model_essays: [], topics: [], materials: [{ id: 1, theme: '时间与节奏', content: '木心《从前慢》', origin: 'ai_extracted', favorite: false, exam_count: 0 }, { id: 2, theme: '时间与节奏', content: '匠人精神', origin: 'ai_generated', favorite: false, exam_count: 0 }] },
    });
    mockResponses['PUT /essays/{essayId}/draft'] = () => ({ status: 200, data: essay({ content: 'x' }) });
    mockResponses['POST /essays/{essayId}/submit'] = () => ({ status: 200, data: essay({ status: 'grading' }) });
    const r = await wrap(<EssayWritePage />);
    expect(await screen.findByDisplayValue('本地还没同步的正文')).toBeTruthy();
    expect(screen.getByLabelText('剩余时间').props.children).toBe('00:58:00');
    await fireEvent.press(screen.getByText('关闭计时'));
    expect(screen.getByText('不计时')).toBeTruthy();
    await fireEvent.press(screen.getByText('素材'));
    expect(await screen.findByText('AI 补充')).toBeTruthy();
    await fireEvent.press(screen.getAllByText('插入')[0]!);
    expect(screen.getByDisplayValue('本地还没同步的正文木心《从前慢》')).toBeTruthy();
    await fireEvent.press(screen.getByText('提交'));
    await waitFor(() => expect(mockReplace).toHaveBeenCalledWith({ pathname: '/essay/result/[id]', params: { id: '70' } }));
    expect(mockCalls.find((c) => c.method === 'PUT')?.init).toMatchObject({ body: { content: '本地还没同步的正文木心《从前慢》', timed: false } });
    expect(getJSON('draft:essay:70')).toBeUndefined();
    await r.unmount();
  });

  it('本周批改次数用完：弹额度提示，草稿保留', async () => {
    mockResponses['GET /essays/{essayId}'] = () => ({ status: 200, data: essay() });
    mockResponses['PUT /essays/{essayId}/draft'] = () => ({ status: 200, data: essay() });
    mockResponses['POST /essays/{essayId}/submit'] = () => ({ status: 402, error: { code: 'QUOTA_EXCEEDED', message: '作文批改次数不足' } });
    const r = await wrap(<EssayWritePage />);
    await fireEvent.press(await screen.findByText('提交'));
    expect(await screen.findByText('本周的作文批改次数用完了')).toBeTruthy();
    expect(getJSON('draft:essay:70')).toBeTruthy();
    await r.unmount();
  });
});

describe('5.5–5.6 批改', () => {
  beforeEach(() => {
    mockParams = { id: '70' };
  });

  it('批改中显示三步进度，可以先离开', async () => {
    mockResponses['GET /essays/{essayId}'] = () => ({ status: 200, data: essay({ status: 'grading', rubric: generic }) });
    const r = await wrap(<EssayResultPage />);
    expect(await screen.findByText('按通用五维度打分')).toBeTruthy();
    expect(screen.getByText('先离开，好了通知我')).toBeTruthy();
    await r.unmount();
  });

  it('总分、较上篇、各维度与失分主项；逐段批注标出原文；范文对比；复核与按建议重写', async () => {
    mockResponses['GET /essays/{essayId}'] = () => ({ status: 200, data: graded() });
    mockResponses['POST /essays/{essayId}/dispute'] = () => ({ status: 200, data: graded({ disputed: true, score_before: 112, score: 116 }) });
    mockResponses['POST /essays'] = () => ({ status: 201, data: essay({ id: 72, draft_no: 2 }) });
    const r = await wrap(<EssayResultPage />);
    expect(await screen.findByText('较上篇 +4')).toBeTruthy();
    expect(screen.getByText('按你的评分细则 · 计入预估分 ›')).toBeTruthy();
    expect(screen.getByText('22/30')).toBeTruthy();
    expect(screen.getByText(/失分主项/)).toBeTruthy();
    expect(screen.getByText(/第 3 段后加一层转折/)).toBeTruthy();
    await fireEvent.press(screen.getByText(/^逐段批注/));
    expect(screen.getByText('生活变得越来越快，每个人都很忙碌')).toBeTruthy();
    expect(screen.getByText('表述空泛')).toBeTruthy();
    await fireEvent.press(screen.getByText(/^范文对比/));
    expect(screen.getByText(/慢中见真/)).toBeTruthy();
    expect(screen.getByText('· 快的代价')).toBeTruthy();

    await fireEvent.press(screen.getByText('有异议'));
    await fireEvent.press(screen.getByText('分数给得不合理'));
    await fireEvent.press(screen.getByText('提交'));
    expect(await screen.findByText('已复核：原来 112 分，以复核结果为准')).toBeTruthy();
    expect(screen.queryByText('有异议')).toBeNull();

    await fireEvent.press(screen.getByText('按建议重写'));
    await waitFor(() => expect(mockCalls.find((c) => c.path === '/essays')?.init).toMatchObject({ body: { parent_essay_id: 70 } }));
    await r.unmount();
  });

  it('按通用标准批改：标明只作参考、不计入预估分', async () => {
    mockResponses['GET /essays/{essayId}'] = () => ({ status: 200, data: graded({ rubric: generic, counts_for_estimate: false, model_essays: [], topic_source: 'ai' }) });
    const r = await wrap(<EssayResultPage />);
    expect(await screen.findByText('按通用五维度标准 · 分数只作参考，不计入预估分 ›')).toBeTruthy();
    await fireEvent.press(screen.getByText(/^范文对比/));
    expect(screen.getByText('只有真题题目会对照你导入的同题范文')).toBeTruthy();
    await r.unmount();
  });
});

describe('5.7–5.9', () => {
  it('作文本：篇数、平均、最高、趋势、最弱维度与列表', async () => {
    mockResponses['GET /subjects/{subjectId}/essays'] = () => ({
      status: 200,
      data: {
        count: 2, avg_score: 105, best: 112, dim_avgs: [],
        trend: [{ essay_id: 1, date: '2026-09-18T08:00:00Z', score: 98, full_score: 150 }, { essay_id: 2, date: '2026-09-23T08:00:00Z', score: 112, full_score: 150 }],
        weakest: { name: '内容与论证', avg_score: 19, avg_max: 30 },
        items: [{ id: 2, topic: '以「慢」为题', topic_source: 'exam', draft_no: 2, status: 'graded', score: 112, full_score: 150, word_count: 1126, counts_for_estimate: true, created_at: '2026-09-23T08:00:00Z', graded_at: '2026-09-23T08:00:00Z' }],
      },
    });
    const r = await wrap(<EssayBookPage />);
    expect(await screen.findByText('105')).toBeTruthy();
    expect(screen.getByText('9 月 23 日 · 真题 · 第 2 稿')).toBeTruthy();
    expect(screen.getByText('内容与论证')).toBeTruthy();
    await fireEvent.press(screen.getByLabelText('以「慢」为题'));
    expect(mockPush).toHaveBeenCalledWith({ pathname: '/essay/result/[id]', params: { id: '2' } });
    await r.unmount();
  });

  it('评分标准：你的资料与通用标准，可编辑、可切换', async () => {
    const user = { id: 9, name: '学校评分细则', source: 'user_material', origin: 'ai_extracted', full_score: 60, dimensions: [{ name: '立意', score: 30, description: '切题' }, { name: '结构', score: 30, bands: [{ range: '25–30', description: '一类' }] }], source_ref: { material_id: 1, file_name: '908 历年作文真题.pdf', page: 2 } };
    const gen = { ...generic, origin: 'official', dimensions: [{ name: '立意', score: 30 }] };
    let active = 9;
    mockResponses['GET /subjects/{subjectId}/essay-rubrics'] = () => ({ status: 200, data: { active_id: active, user, generic: gen } });
    mockResponses['PUT /subjects/{subjectId}/essay-rubrics/active'] = () => {
      active = 1;
      return { status: 200, data: { active_id: 1, user, generic: gen } };
    };
    mockResponses['PUT /subjects/{subjectId}/essay-rubrics/user'] = () => ({ status: 200, data: { active_id: 9, user: { ...user, name: '改过的细则', origin: 'user_confirmed' }, generic: gen } });
    const r = await wrap(<EssayRubricPage />);
    expect(await screen.findByText('学校评分细则 · 满分 60 · 从 908 历年作文真题.pdf 第 2 页识别')).toBeTruthy();
    expect(screen.getByText('25–30')).toBeTruthy();
    expect(screen.getByText('一类')).toBeTruthy();
    await fireEvent.press(screen.getByText('编辑'));
    await fireEvent.changeText(screen.getByLabelText('标准名称'), '改过的细则');
    await fireEvent.changeText(screen.getByLabelText('维度 2 分值'), '40');
    expect(screen.getByText('满分 = 各维度分值之和：70')).toBeTruthy();
    await fireEvent.press(screen.getByText('保存'));
    await waitFor(() => expect(mockCalls.find((c) => c.path === '/subjects/{subjectId}/essay-rubrics/user')?.init).toMatchObject({ body: { name: '改过的细则', dimensions: [{ name: '立意', score: 30 }, { name: '结构', score: 40 }] } }));
    expect(await screen.findByText(/你改过/)).toBeTruthy();
    await fireEvent.press(screen.getByText('改用通用标准'));
    await waitFor(() => expect(mockCalls.find((c) => c.path === '/subjects/{subjectId}/essay-rubrics/active')?.init).toMatchObject({ body: { source: 'generic' } }));
    await r.unmount();
  });

  it('范文详情：结构拆解与「只有你自己能看到」', async () => {
    mockParams = { id: '5' };
    mockResponses['GET /model-essays/{modelEssayId}'] = () => ({
      status: 200,
      data: { id: 5, title: '慢中见真', topic: '以「慢」为题', content: '第一段。\n第二段。', structure: { opening: '把慢从速度问题转为态度问题', points: ['先写快的代价'], ending: '回扣题目' }, source_ref: { material_id: 2, file_name: '范文.pdf', page: 3 } },
    });
    const r = await wrap(<ModelEssayPage />);
    expect(await screen.findByText('开头立意')).toBeTruthy();
    expect(screen.getByText('分论点一')).toBeTruthy();
    expect(screen.getByText('第二段。')).toBeTruthy();
    expect(screen.getByText(/只有你自己能看到/)).toBeTruthy();
    await r.unmount();
  });
});
