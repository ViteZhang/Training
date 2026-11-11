import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import BankTab from '../../app/(tabs)/bank';

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

const overview = (over: object = {}) => ({
  subject_id: 5, bank_id: 7, question_count: 340, kp_count: 120, needs_review_count: 0, material_count: 6, paper_count: 10,
  mastery_distribution: { unlearned: 0, learning: 0, consolidating: 0, mastered: 0 }, has_official: false, ...over,
});

beforeEach(() => {
  mockCalls.length = 0;
  mockPush.mockReset();
  mockResponses = {
    'GET /subjects': () => ({ status: 200, data: { items: [{ id: 5, name: '西方哲学史', code: '812', is_essay: false }] } }),
    'GET /subjects/{subjectId}/bank': () => ({ status: 200, data: overview() }),
    'GET /subjects/{subjectId}/knowledge-tree': () => ({ status: 200, data: { sections: [], needs_review_count: 0 } }),
    'GET /feature-flags': () => ({ status: 200, data: { flags: { official_bank: true, course_apply: true } } }),
    'GET /official-banks': () => ({ status: 200, data: { items: [] } }),
    'GET /profile': () => ({ status: 200, data: {} }),
  };
});

describe('3.1 题库顶部', () => {
  it('标题栏只有一个「＋ 添加」；考情分析、知识图谱放在课程下面', async () => {
    await wrap(<BankTab />);
    expect(await screen.findByText('基于 10 套真题')).toBeTruthy();
    expect(screen.getByText('120 个知识点的关系')).toBeTruthy();
    expect(screen.queryByText('添加官方题库')).toBeNull();
    expect(screen.queryByText('申请收录这门课')).toBeNull();
    await fireEvent.press(screen.getByLabelText('考情分析'));
    expect(mockPush).toHaveBeenCalledWith({ pathname: '/bank/profile', params: { subjectId: '5' } });
  });

  it('「＋ 添加」弹层：导入我的资料、添加官方题库，底部小字申请收录', async () => {
    await wrap(<BankTab />);
    await fireEvent.press(await screen.findByLabelText('添加'));
    expect(await screen.findByText('添加到 812 西方哲学史')).toBeTruthy();
    expect(await screen.findByText('添加官方题库')).toBeTruthy();
    expect(screen.getByText('申请收录')).toBeTruthy();
    await fireEvent.press(screen.getByLabelText('导入我的资料'));
    expect(mockPush).toHaveBeenCalledWith({ pathname: '/import', params: { subjectId: '5' } });
  });

  it('已添加官方题库：弹层里不再提申请收录', async () => {
    mockResponses['GET /subjects/{subjectId}/bank'] = () => ({ status: 200, data: overview({ has_official: true }) });
    await wrap(<BankTab />);
    await fireEvent.press(await screen.findByLabelText('添加'));
    expect(await screen.findByText('添加官方题库')).toBeTruthy();
    await waitFor(() => expect(screen.queryByText('申请收录')).toBeNull());
  });
});
