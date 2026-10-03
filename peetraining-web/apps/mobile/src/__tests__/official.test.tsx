import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import OfficialBanksPage from '../../app/bank/official';
import { messageHref } from '../features/messages/api';

const mockPush = jest.fn();
const mockReplace = jest.fn();
const mockParams: Record<string, string> = {};
jest.mock('expo-router', () => ({
  router: { push: (...a: unknown[]) => mockPush(...a), replace: (...a: unknown[]) => mockReplace(...a), back: jest.fn() },
  useLocalSearchParams: () => mockParams,
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


const bank = (over: object = {}) => ({
  bank_id: 9, title: '某某大学 654 中国语言文学基础', school: '某某大学', major: '中国古代文学', subject_code: '654', subject_name: '中国语言文学基础',
  version: '1.0', kp_count: 58, question_count: 246, suggested_subject_id: 2, ...over,
});

beforeEach(() => {
  mockCalls.length = 0;
  mockResponses = {
    'GET /subjects': () => ({ status: 200, data: { items: [{ id: 1, name: '政治', is_essay: false }, { id: 2, name: '中国语言文学基础', code: '654', is_essay: false }, { id: 3, name: '写作', is_essay: true }] } }),
    'GET /official-banks': () => ({ status: 200, data: { items: [bank()] } }),
    'PUT /official-banks/{bankId}/subscription': () => ({ status: 204 }),
    'DELETE /official-banks/{bankId}/subscription': () => ({ status: 204 }),
  };
});

describe('添加官方题库（T30）', () => {
  it('列出官方题库；添加时建议代码相同的专业课，作文课不出现', async () => {
    await wrap(<OfficialBanksPage />);
    expect(await screen.findByText('某某大学 654 中国语言文学基础')).toBeTruthy();
    expect(screen.getByText(/58 个知识点 · 246 道题 · 1.0 版/)).toBeTruthy();
    await fireEvent.press(screen.getByText('添加'));
    expect(await screen.findByText('建议')).toBeTruthy();
    expect(screen.queryByText('写作')).toBeNull();
    await fireEvent.press(screen.getByText('654 中国语言文学基础'));
    await waitFor(() => expect(mockCalls.find((c) => c.method === 'PUT')).toBeTruthy());
    const put = mockCalls.find((c) => c.method === 'PUT')!;
    expect(put.init).toEqual({ params: { path: { bankId: 9 } }, body: { subject_id: 2 } });
  });

  it('已添加的显示添加到哪门课，可以移除（先确认）', async () => {
    mockResponses['GET /official-banks'] = () => ({ status: 200, data: { items: [bank({ added_subject_id: 2 })] } });
    await wrap(<OfficialBanksPage />);
    expect(await screen.findByText('已添加到 654 中国语言文学基础')).toBeTruthy();
    await fireEvent.press(screen.getByText('移除'));
    expect(await screen.findByText(/没改过的官方内容会连同练习记录一起删除/)).toBeTruthy();
    const buttons = screen.getAllByText('移除');
    await fireEvent.press(buttons[buttons.length - 1]!);
    await waitFor(() => expect(mockCalls.find((c) => c.method === 'DELETE')).toBeTruthy());
  });

  it('官方题库的消息跳到添加页或题库', () => {
    const m = { id: 1, mtype: 'official_bank', title: '', body: '', created_at: '', read: false } as const;
    expect(messageHref({ ...m, page: 'official_banks' } as never)).toBe('/bank/official');
    expect(messageHref({ ...m, page: 'bank', params: { subject_id: 2 } } as never)).toBe('/(tabs)/bank');
  });
});
