import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import TrainTab from '../../app/(tabs)/train';
import NormPage from '../../app/practice/norm';
import PracticeRunner from '../../app/practice/[id]';
import { storage } from '../lib/storage';

const mockPush = jest.fn();
let mockParams: Record<string, string> = {};
jest.mock('expo-router', () => ({
  router: { push: (...a: unknown[]) => mockPush(...a), replace: jest.fn(), back: jest.fn() },
  useLocalSearchParams: () => mockParams,
}));
let mockUuid = 0;
jest.mock('expo-crypto', () => ({ randomUUID: () => `uuid-${++mockUuid}` }));
const mockUpload = jest.fn(async () => ({ status: 200 }));
jest.mock('expo-file-system', () => ({
  File: class {
    uri: string;
    size = 2048;
    constructor(uri: string) {
      this.uri = uri;
    }
    upload = (...a: unknown[]) => mockUpload(...(a as []));
  },
}));
let mockShot = 0;
jest.mock('expo-image-picker', () => ({
  requestCameraPermissionsAsync: async () => ({ granted: true, canAskAgain: true }),
  launchCameraAsync: async () => ({ canceled: false, assets: [{ uri: `file:///shot-${++mockShot}.jpg`, mimeType: 'image/jpeg', fileSize: 2048 }] }),
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
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const metrics = { frame: { x: 0, y: 0, width: 390, height: 844 }, insets: { top: 0, left: 0, right: 0, bottom: 0 } };
  return render(
    <SafeAreaProvider initialMetrics={metrics}>
      <QueryClientProvider client={qc}>{ui}</QueryClientProvider>
    </SafeAreaProvider>,
  );
}

const termQ = { id: 201, qtype: 'term', stem: '意境', answer: '意境是情景交融、虚实相生的艺术境界。', source: 'exam', origin_tags: [], knowledge_points: [{ id: 3, name: '意境', is_primary: true }], rubric_count: 3 };
const session = { id: 50, kind: 'type_drill', title: '题型专项 · 名词解释', subject_id: 7, status: 'in_progress', cursor_index: 0, started_at: '2026-10-02T02:00:00Z', done_count: 0, questions: [termQ] };
const grading = {
  grading_id: 9, attempt_id: 1, question_id: 201, status: 'done', score: 4, full_score: 5, counts: { hit: 2, partial: 0, miss: 1 },
  points: [{ seq: 1, content: '情景交融', score: 2, got: 2, verdict: 'hit', quote: '情景交融' }], rubric_version: 1, rubric_source: 'user_confirmed', structure_ok: true,
  suggestions: [], loss: [{ type: 'norm', points: 1, reason: '知道但没写到位' }], kp_changes: [], wrong_book: 'added', trigger: 'submit', disputed: false, quota_charged: true,
};

beforeEach(() => {
  mockCalls.length = 0;
  mockPush.mockReset();
  mockUpload.mockClear();
  storage.clearAll();
  storage.set('permission_explained_camera', true);
  mockResponses = {
    'GET /quota': () => ({ status: 200, data: { items: [{ quota_type: 'grading', used: 0, limit: 3, period: 'daily' }], membership: { is_member: false } } }),
    'GET /feature-flags': () => ({ status: 200, data: { flags: {} } }),
  };
});

describe('4.5 拍手写稿识别', () => {
  it('拍照、识别、不确定字词高亮、续拍、确认后按拍照作答提交批改', async () => {
    mockParams = { id: '50' };
    mockResponses['GET /practice-sessions/{sessionId}'] = () => ({ status: 200, data: session });
    let n = 0;
    mockResponses['POST /handwriting/upload-requests'] = () => {
      n++;
      const items = Array.from({ length: n }, (_, i) => ({ object_key: `u/1/hw/k${i}.jpg`, upload_url: `https://oss/k${i}`, upload_headers: {}, expires_at: '2026-10-03T00:00:00Z' }));
      return { status: 200, data: { items } };
    };
    mockResponses['POST /handwriting/recognize'] = () => ({
      status: 200,
      data: n === 1
        ? { text: '意境是情景交融的境界', pages: [], low_confidence: [{ start: 5, end: 7 }], uncertain_count: 1 }
        : { text: '意境是情景交融的境界\n虚实相生', pages: [], low_confidence: [{ start: 5, end: 7 }], uncertain_count: 1 },
    });
    mockResponses['POST /practice-sessions/{sessionId}/gradings'] = () => ({ status: 200, data: grading });
    await wrap(<PracticeRunner />);
    await fireEvent.press(await screen.findByText('拍手写稿'));
    await fireEvent.press(screen.getByText('拍手写稿'));
    expect(await screen.findByText('1 处不确定，请核对')).toBeTruthy();
    expect(screen.getByText('交融')).toBeTruthy();
    expect(mockUpload).toHaveBeenCalledWith('https://oss/k0', { httpMethod: 'PUT', headers: {} });
    await fireEvent.press(screen.getByText('再拍一张（续写）'));
    await waitFor(() => expect(screen.getByLabelText('识别文字').props.value).toBe('意境是情景交融的境界\n虚实相生'));
    await fireEvent.changeText(screen.getByLabelText('识别文字'), '意境是情景交融的境界，虚实相生');
    await fireEvent.press(screen.getByText('确认并提交批改'));
    expect(await screen.findByText('/ 5 分')).toBeTruthy();
    const body = (mockCalls.find((c) => c.path === '/practice-sessions/{sessionId}/gradings')?.init as { body: Record<string, unknown> }).body;
    expect(body).toMatchObject({ answer_text: '意境是情景交融的境界，虚实相生', answer_mode: 'photo', photo_keys: ['u/1/hw/k0.jpg', 'u/1/hw/k1.jpg'] });
    // 失分归因「答题不规范」→ 4.13
    await fireEvent.press(screen.getByText('看规范写法'));
    expect(mockPush).toHaveBeenCalledWith({ pathname: '/practice/norm', params: { subjectId: '7', qtype: 'term' } });
  });
});

describe('4.13 答题规范', () => {
  it('结构、高分写法、你上次的写法，按结构写一道', async () => {
    mockParams = { subjectId: '7', qtype: 'term' };
    mockResponses['GET /subjects/{subjectId}/answer-norms/{qtype}'] = () => ({
      status: 200,
      data: {
        qtype: 'term',
        elements: [
          { name: '定义', desc: '一句话说清是什么', share: 0.4 },
          { name: '特征要点', desc: '分点写出关键特征', share: 0.4 },
          { name: '出处或例子', desc: '写明出处或举例', share: 0.2 },
        ],
        tips: ['三段式，80–150 字'],
        example: { question_id: 201, stem: '意境', original_text: '意境是情景交融、虚实相生、韵味无穷的艺术境界。', rubric_points: ['情景交融', '虚实相生'], source_ref: { material_id: 1, file_name: '讲义.docx', page: 3 } },
        last: { question_id: 201, stem: '意境', answer_text: '意境是情景交融的境界。', grading_id: 9, score: 3, full_score: 5, missing: ['虚实相生'] },
        practice_question_id: 202,
      },
    });
    mockResponses['GET /questions/{questionId}'] = () => ({ status: 200, data: { id: 202, stem: '典型' } });
    mockResponses['POST /questions/{questionId}/norm-check'] = () => ({
      status: 200,
      data: { grading_id: 11, complete: false, elements: [{ name: '定义', present: true, quote: '典型是共性与个性的统一' }, { name: '特征要点', present: true, quote: '一是' }, { name: '出处或例子', present: false, suggestion: '举一个作品例子' }], suggestions: ['补上例子'] },
    });
    await wrap(<NormPage />);
    expect(await screen.findByText('一句话说清是什么，约占 40%')).toBeTruthy();
    expect(screen.getByText('意境是情景交融、虚实相生、韵味无穷的艺术境界。')).toBeTruthy();
    expect(screen.getByText('出自 讲义.docx 第 3 页')).toBeTruthy();
    expect(screen.getByText('你上次的写法 · 3 / 5 分')).toBeTruthy();
    expect(screen.getByText('缺：虚实相生')).toBeTruthy();
    await fireEvent.press(screen.getByText('按三段结构写一道'));
    expect(await screen.findByText('典型')).toBeTruthy();
    await fireEvent.changeText(screen.getByLabelText('按结构作答'), '典型是共性与个性的统一。一是……');
    await fireEvent.press(screen.getByText('提交，看结构'));
    expect(await screen.findByText('结构还不完整')).toBeTruthy();
    expect(screen.getByText('✗ 出处或例子')).toBeTruthy();
    expect((mockCalls.find((c) => c.path === '/questions/{questionId}/norm-check')?.init as { body: Record<string, unknown> }).body).toMatchObject({ answer_text: '典型是共性与个性的统一。一是……' });
  });
});

describe('今日训练 AI 补题开关', () => {
  it('默认关闭；打开后开始今日训练带 ai_fill', async () => {
    mockResponses['GET /subjects'] = () => ({ status: 200, data: { items: [{ id: 7, name: '语言文学基础', full_score: 150, is_essay: false, bank_id: 1, question_count: 4, kp_count: 2, material_count: 1 }], max_subjects: 3, can_add: true } });
    mockResponses['GET /subjects/{subjectId}/practice'] = () => ({
      status: 200,
      data: { subject_id: 7, stage: 'strengthen', total_questions: 4, recite_due: 0, paper_first: false, today: { total: 3, done: 0, minutes: 10 }, qtype_counts: [], wrong_book: { total: 0, due: 0 } },
    });
    mockResponses['GET /gradings/pending'] = () => ({ status: 200, data: { items: [], remaining_today: 3 } });
    mockResponses['POST /practice-sessions'] = () => ({ status: 201, data: session });
    await wrap(<TrainTab />);
    const sw = await screen.findByLabelText('今日训练 AI 补题');
    expect(sw.props.value).toBe(false);
    await fireEvent(sw, 'valueChange', true);
    await fireEvent.press(screen.getByText('开始训练'));
    await waitFor(() => expect(mockCalls.find((c) => c.path === '/practice-sessions')?.init).toEqual({ body: { subject_id: 7, kind: 'daily', ai_fill: true } }));
  });
});
