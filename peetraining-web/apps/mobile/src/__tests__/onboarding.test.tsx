import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import Setup from '../../app/(onboarding)/setup';
import SubjectStep from '../../app/(onboarding)/subject';
import Target from '../../app/(onboarding)/target';
import { routeFor } from '../features/auth/routing';
import { saveDraft } from '../features/onboarding/draft';

const mockPush = jest.fn();
jest.mock('expo-router', () => ({
  router: { push: (...a: unknown[]) => mockPush(...a), replace: jest.fn(), back: jest.fn() },
  useLocalSearchParams: () => ({}),
}));
jest.mock('expo-crypto', () => ({ randomUUID: () => 'uuid' }));

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

const years = { items: [{ exam_year: 2027, label: '2027 研考', first_exam_start: '2026-12-19', first_exam_end: '2026-12-20', subject_exam_date: '2026-12-20', days_to_exam: 79, suggested_stage: 'strengthen' }] };
const subject = (id: number, name: string, extra = {}) => ({ id, name, full_score: 150, is_essay: false, bank_id: id, question_count: 0, kp_count: 0, material_count: 0, ...extra });

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
  mockPush.mockReset();
  mockResponses = {};
});

describe('引导分流', () => {
  it('按中断的步骤回到对应页面', () => {
    expect(routeFor({ loggedIn: true, onboardingStep: '1.1' })).toBe('/(onboarding)/subject');
    expect(routeFor({ loggedIn: true, onboardingStep: '1.2' })).toBe('/(onboarding)/target');
    expect(routeFor({ loggedIn: true, onboardingStep: '1.3' })).toBe('/(onboarding)/setup');
    expect(routeFor({ loggedIn: true, onboardingStep: '1.6' })).toBe('/(onboarding)/import');
  });
});

describe('1.1 你考哪门专业课', () => {
  it('没有专业课时不能下一步；添加后可以', async () => {
    let list = [] as unknown[];
    mockResponses = {
      'GET /exam-years': () => ({ status: 200, data: years }),
      'GET /subjects': () => ({ status: 200, data: { items: list, max_subjects: 3, can_add: true } }),
      'POST /subjects': () => {
        list = [subject(1, '中国古代文学史')];
        return { status: 201, data: list[0] };
      },
    };
    await wrap(<SubjectStep />);
    expect(await screen.findByText('至少添加一门专业课')).toBeTruthy();
    await fireEvent.changeText(screen.getByLabelText('添加专业课'), '中国古代文学史');
    await fireEvent.press(screen.getByText('添加'));
    await waitFor(() => expect(screen.getByText('中国古代文学史')).toBeTruthy());
    expect(mockCalls.find((c) => c.method === 'POST')?.init).toEqual({ body: { name: '中国古代文学史', code: null, full_score: 150 } });
    expect(screen.queryByText('至少添加一门专业课')).toBeNull();
  });

  it('免费版第 4 门：提示开通会员', async () => {
    mockResponses = {
      'GET /exam-years': () => ({ status: 200, data: years }),
      'GET /subjects': () => ({ status: 200, data: { items: [subject(1, 'a'), subject(2, 'b'), subject(3, 'c')], max_subjects: 3, can_add: false } }),
    };
    await wrap(<SubjectStep />);
    await fireEvent.changeText(await screen.findByLabelText('添加专业课'), '第四门');
    await fireEvent.press(screen.getByText('添加'));
    expect(screen.getByText('免费版最多添加 3 门专业课')).toBeTruthy();
    expect(mockCalls.some((c) => c.method === 'POST')).toBe(false);
  });
});

describe('1.2 设定目标分', () => {
  it('默认取满分的 70%，可不设', async () => {
    mockResponses = {
      'GET /subjects': () => ({ status: 200, data: { items: [subject(1, '中国文学'), subject(2, '写作')], max_subjects: 3, can_add: true } }),
      'PATCH /subjects/{subjectId}': () => ({ status: 200, data: subject(1, 'x') }),
      'PATCH /me': () => ({ status: 200, data: {} }),
    };
    await wrap(<Target />);
    expect((await screen.findAllByText(/^105/)).length).toBe(2);
    await fireEvent.press(screen.getAllByText('先不设这门')[1]!);
    await fireEvent.press(screen.getByText('下一步'));
    await waitFor(() => expect(mockPush).toHaveBeenCalledWith('/(onboarding)/setup'));
    const patches = mockCalls.filter((c) => c.path === '/subjects/{subjectId}').map((c) => (c.init as { body: unknown }).body);
    expect(patches).toEqual([{ full_score: 150, target_score: 105 }, { full_score: 150, target_score: null }]);
  });
});

describe('1.3 备考安排', () => {
  it('默认选中系统建议的阶段与 45 分钟，提交备考档案', async () => {
    saveDraft({ examYear: 2027, targetSchoolMajor: '海南大学 · 中国语言文学' });
    mockResponses = {
      'GET /exam-years': () => ({ status: 200, data: years }),
      'PUT /profile': () => ({ status: 200, data: {} }),
      'PATCH /me': () => ({ status: 200, data: {} }),
    };
    await wrap(<Setup />);
    expect(await screen.findByText('79')).toBeTruthy();
    expect(screen.getByLabelText('强化期').props.accessibilityState.selected).toBe(true);
    await fireEvent.press(screen.getByText('下一步'));
    await waitFor(() => expect(mockPush).toHaveBeenCalledWith('/(onboarding)/import'));
    expect(mockCalls.find((c) => c.path === '/profile')?.init).toEqual({
      body: { exam_year: 2027, stage: 'strengthen', daily_minutes: 45, target_school_major: '海南大学 · 中国语言文学' },
    });
    expect(mockCalls.find((c) => c.path === '/me')?.init).toEqual({ body: { onboarding_step: '1.4' } });
  });
});
