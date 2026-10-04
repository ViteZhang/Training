import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import RecitePage from '../../app/recite/[id]';
import ReciteDonePage from '../../app/recite/done/[id]';
import { storage } from '../lib/storage';

const mockPush = jest.fn();
const mockReplace = jest.fn();
let mockParams: Record<string, string> = {};
jest.mock('expo-router', () => ({
  router: { push: (...a: unknown[]) => mockPush(...a), replace: (...a: unknown[]) => mockReplace(...a), back: jest.fn() },
  useLocalSearchParams: () => mockParams,
}));
let mockUuid = 0;
jest.mock('expo-crypto', () => ({ randomUUID: () => `uuid-${++mockUuid}` }));
const mockUpload = jest.fn(async () => ({ status: 200 }));
jest.mock('expo-file-system', () => ({
  File: class {
    uri: string;
    size = 4096;
    constructor(uri: string) {
      this.uri = uri;
    }
    upload = (...a: unknown[]) => mockUpload(...(a as []));
  },
}));
const mockRecorder = { prepareToRecordAsync: jest.fn(async () => {}), record: jest.fn(), stop: jest.fn(async () => {}), uri: 'file:///rec.m4a' };
jest.mock('expo-audio', () => ({
  RecordingPresets: { HIGH_QUALITY: {} },
  useAudioRecorder: () => mockRecorder,
  requestRecordingPermissionsAsync: async () => ({ granted: true, canAskAgain: true }),
  setAudioModeAsync: async () => {},
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

const item = (id: number, name: string) => ({
  kp_id: id, name, path: ['中国古代文学', '魏晋南北朝'], original_text: '建安风骨，指以三曹和建安七子为代表的风格。',
  segments: [{ text: '建安风骨，指以', blank: false }, { text: '三曹', blank: true }, { text: '和', blank: false }, { text: '建安七子', blank: true }, { text: '为代表的风格。', blank: false }],
  keywords: ['三曹', '建安七子'], source_ref: { material_id: 1, file_name: '654 历年真题汇编.pdf', page: 12 },
});
const session = (oral = false) => ({ id: 70, subject_id: 7, title: '背诵', done_count: 0, oral_enabled: oral, items: [item(1, '建安风骨'), item(2, '正始之音')] });

beforeEach(() => {
  mockCalls.length = 0;
  mockPush.mockReset();
  mockReplace.mockReset();
  storage.clearAll();
  storage.set('permission_explained_microphone', true);
  mockParams = { id: '70' };
  mockResponses = { 'GET /recite-sessions/{sessionId}': () => ({ status: 200, data: session() }) };
});

describe('4.14 挖空', () => {
  it('关键词遮住，点空格查看，自评后进入下一条；口述开关关闭时不显示', async () => {
    mockResponses['POST /recite-sessions/{sessionId}/records'] = () => ({ status: 200, data: { result: 'remembered', next_review_on: '2026-10-06', m: 8 } });
    await wrap(<RecitePage />);
    expect(await screen.findByText('建安风骨')).toBeTruthy();
    expect(screen.getByText('原文出自 654 历年真题汇编.pdf 第 12 页')).toBeTruthy();
    expect(screen.queryByText('口述')).toBeNull();
    expect(screen.queryByText('三曹')).toBeNull();
    await fireEvent.press(screen.getByLabelText('第 1 个空'));
    expect(screen.getByText('三曹')).toBeTruthy();
    await fireEvent.press(screen.getByText('全部显示'));
    expect(screen.getByText('建安七子')).toBeTruthy();
    await fireEvent.press(screen.getByText('记住了'));
    expect(await screen.findByText('正始之音')).toBeTruthy();
    expect((mockCalls.find((c) => c.method === 'POST')?.init as { body: Record<string, unknown> }).body).toMatchObject({ kp_id: 1, mode: 'cloze', result: 'remembered' });
  });

  it('背完最后一条进入 4.17', async () => {
    mockResponses['GET /recite-sessions/{sessionId}'] = () => ({ status: 200, data: { ...session(), items: [item(1, '建安风骨')] } });
    mockResponses['POST /recite-sessions/{sessionId}/records'] = () => ({ status: 200, data: { result: 'forgot', next_review_on: '2026-10-04', m: 0 } });
    await wrap(<RecitePage />);
    await fireEvent.press(await screen.findByText('没记住'));
    await waitFor(() => expect(mockReplace).toHaveBeenCalledWith({ pathname: '/recite/done/[id]', params: { id: '70', subjectId: '7' } }));
  });
});

describe('4.15 默写', () => {
  it('写完对照原文：关键词覆盖，遗漏的标出', async () => {
    mockResponses['POST /recite-sessions/{sessionId}/records'] = () => ({
      status: 200,
      data: { result: 'vague', next_review_on: '2026-10-05', m: 2, coverage: { hit: 1, total: 2, keywords: [{ text: '三曹', hit: true }, { text: '建安七子', hit: false }] } },
    });
    await wrap(<RecitePage />);
    await fireEvent.press(await screen.findByText('默写'));
    await fireEvent.changeText(screen.getByLabelText('默写'), '以三曹为代表');
    await fireEvent.press(screen.getByText('写好了，对照原文'));
    expect(await screen.findByText('漏：建安七子')).toBeTruthy();
    expect(screen.getByText('模糊')).toBeTruthy();
    expect((mockCalls.find((c) => c.method === 'POST')?.init as { body: Record<string, unknown> }).body).toMatchObject({ kp_id: 1, mode: 'dictation', text: '以三曹为代表' });
    await fireEvent.press(screen.getByText('下一条'));
    expect(await screen.findByText('正始之音')).toBeTruthy();
  });
});

describe('4.16 口述', () => {
  it('开关打开时可录音，上传后按关键词检查', async () => {
    mockResponses['GET /recite-sessions/{sessionId}'] = () => ({ status: 200, data: session(true) });
    mockResponses['POST /recite-sessions/{sessionId}/audio-upload'] = () => ({ status: 200, data: { object_key: 'u/1/recite/70-a.m4a', upload_url: 'https://oss/a', upload_headers: {}, expires_at: '2026-10-03T00:00:00Z' } });
    mockResponses['POST /recite-sessions/{sessionId}/records'] = () => ({
      status: 200,
      data: { result: 'remembered', next_review_on: '2026-10-06', m: 8, transcript: '三曹和建安七子', coverage: { hit: 2, total: 2, keywords: [{ text: '三曹', hit: true }, { text: '建安七子', hit: true }] } },
    });
    await wrap(<RecitePage />);
    await fireEvent.press(await screen.findByText('口述'));
    await fireEvent.press(screen.getByLabelText('开始录音'));
    await waitFor(() => expect(mockRecorder.record).toHaveBeenCalled());
    await fireEvent.press(screen.getByLabelText('结束录音'));
    expect(await screen.findByText('三曹和建安七子')).toBeTruthy();
    expect(mockUpload).toHaveBeenCalledWith('https://oss/a', { httpMethod: 'PUT', headers: {} });
    const rec = mockCalls.find((c) => c.path === '/recite-sessions/{sessionId}/records')?.init as { body: Record<string, unknown> };
    expect(rec.body).toMatchObject({ mode: 'oral', audio_key: 'u/1/recite/70-a.m4a' });
  });
});

describe('4.17 背诵完成', () => {
  it('统计、与上一轮对比、复习安排、再背没记住的', async () => {
    mockParams = { id: '70', subjectId: '7' };
    mockResponses['POST /recite-sessions/{sessionId}/finish'] = () => ({
      status: 200,
      data: { remembered: 7, vague: 2, forgot: 1, forgot_count: 1, previous_remembered: 5, schedule: { tomorrow: 1, two_days: 2, later: 7 } },
    });
    mockResponses['POST /recite-sessions'] = () => ({ status: 201, data: { ...session(), id: 71, title: '再背没记住的' } });
    await wrap(<ReciteDonePage />);
    expect(await screen.findByText('10 条里记住了 7 条，比上一轮多 2 条')).toBeTruthy();
    expect(screen.getByText('7 条')).toBeTruthy();
    await fireEvent.press(screen.getByText('再背没记住的'));
    await waitFor(() => expect(mockReplace).toHaveBeenCalledWith({ pathname: '/recite/[id]', params: { id: '71' } }));
    expect(mockCalls.find((c) => c.path === '/recite-sessions')?.init).toEqual({ body: { subject_id: 7, retry_of: 70 } });
  });
});
