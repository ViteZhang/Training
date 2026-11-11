import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import OfficialBanksPage from '../../app/bank/official';
import OfficialUpdates, { groupBySchool } from '../../app/official/updates';
import { countMajors, matchTarget, OfficialNotice } from '../features/official/OfficialNotice';
import { messageHref } from '../features/messages/api';
import { needsGroupPick } from '../features/official/GroupPicker';

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

  it('添加后有限选题分组、方向一组都不匹配：请选报考方向，选中组的专业记为报考方向', async () => {
    const groups = [
      { label: '第一组', majors: '文艺学、比较文学与世界文学', matches: false },
      { label: '第二组', majors: '中国古代文学', matches: false },
    ];
    mockResponses['GET /official-banks'] = () => ({ status: 200, data: { items: [bank({ groups })] } });
    mockResponses['PATCH /subjects/{subjectId}'] = () => ({ status: 200, data: { id: 2 } });
    await wrap(<OfficialBanksPage />);
    await fireEvent.press(await screen.findByText('添加'));
    await fireEvent.press(await screen.findByText('654 中国语言文学基础'));
    expect(await screen.findByText('你报考的是哪个方向？')).toBeTruthy();
    expect(screen.getByText('文艺学、比较文学与世界文学')).toBeTruthy();
    await fireEvent.press(screen.getByText('中国古代文学'));
    await waitFor(() => expect(mockCalls.find((c) => c.method === 'PATCH')).toBeTruthy());
    expect(mockCalls.find((c) => c.method === 'PATCH')!.init).toEqual({ params: { path: { subjectId: 2 } }, body: { my_majors: '中国古代文学' } });
  });

  it('方向已匹配某一组，或没有分组：添加后不问', async () => {
    const groups = [
      { label: '第一组', majors: '文艺学', matches: false },
      { label: '第二组', majors: '中国古代文学', matches: true },
    ];
    expect(needsGroupPick({ groups })).toBe(false);
    expect(needsGroupPick({ groups: [] })).toBe(false);
    expect(needsGroupPick({})).toBe(false);
    expect(needsGroupPick({ groups: groups.map((g) => ({ ...g, matches: false })) })).toBe(true);
  });

  it('官方题库的消息跳到添加页或题库', () => {
    const m = { id: 1, mtype: 'official_bank', title: '', body: '', created_at: '', read: false } as const;
    expect(messageHref({ ...m, page: 'official_banks' } as never)).toBe('/bank/official');
    expect(messageHref({ ...m, page: 'bank', params: { subject_id: 2 } } as never)).toBe('/(tabs)/bank');
  });
});

describe('已上线官方题库公告', () => {
  const items = [
    bank({ bank_id: 1, school: '乙大学', major: '哲学', subject_code: '812', subject_name: '西方哲学史', updated_at: '2026-11-08T02:00:00Z', is_new: true }),
    bank({ bank_id: 2, school: '甲大学', major: '中国古代文学', updated_at: '2026-10-01T02:00:00Z', is_new: false }),
    bank({ bank_id: 3, school: '甲大学', major: '中国古代文学', subject_code: '855', subject_name: '文学综合', updated_at: '2026-09-01T02:00:00Z', is_new: false }),
  ];
  beforeEach(() => {
    mockResponses['GET /feature-flags'] = () => ({ status: 200, data: { flags: { official_bank: true, course_apply: true } } });
    mockResponses['GET /official-banks'] = () => ({ status: 200, data: { items } });
  });

  it('院校专业按院校 + 专业去重计数；分组时有「新」的院校在前，可搜索', () => {
    expect(countMajors(items)).toBe(2);
    expect(groupBySchool(items as never, '').map(([s]) => s)).toEqual(['乙大学', '甲大学']);
    expect(groupBySchool(items as never, '文学综合').map(([s, l]) => [s, l.length])).toEqual([['甲大学', 1]]);
  });

  it('目标院校专业对得上的官方题库：院校相同就算，专业也相同的排前面', () => {
    expect(matchTarget(items, '甲大学 · 中国古代文学').map((b) => b.bank_id)).toEqual([2, 3]);
    expect(matchTarget(items, '乙大学 · 历史学').map((b) => b.bank_id)).toEqual([1]);
    expect(matchTarget(items, '丙大学')).toEqual([]);
    expect(matchTarget(items, null)).toEqual([]);
  });

  it('添加官方题库页：目标院校的排最前，标「你的院校」', async () => {
    mockResponses['GET /profile'] = () => ({ status: 200, data: { target_school_major: '甲大学 · 中国古代文学' } });
    await wrap(<OfficialBanksPage />);
    expect(await screen.findAllByText('你的院校')).toHaveLength(2);
  });

  it('横幅显示院校专业数和 7 天内更新数，点进列表', async () => {
    await wrap(<OfficialNotice from="onboarding" />);
    expect(await screen.findByText('已有 2 个院校专业上线官方题库')).toBeTruthy();
    expect(screen.getByText('近 7 天新上线或更新 1 门课')).toBeTruthy();
    await fireEvent.press(screen.getByLabelText('查看已上线的官方题库'));
    expect(mockPush).toHaveBeenCalledWith({ pathname: '/official/updates', params: { from: 'onboarding' } });
  });

  it('开关关闭时不显示横幅', async () => {
    mockResponses['GET /feature-flags'] = () => ({ status: 200, data: { flags: {} } });
    await wrap(<OfficialNotice from="bank" />);
    await waitFor(() => expect(mockCalls.some((c) => c.path === '/feature-flags')).toBe(true));
    await waitFor(() => expect(screen.queryByText(/上线官方题库/)).toBeNull());
  });

  it('列表按院校分组、标新，底部可申请收录', async () => {
    mockParams.from = 'bank';
    await wrap(<OfficialUpdates />);
    expect(await screen.findByText('乙大学')).toBeTruthy();
    expect(screen.getByText('812 西方哲学史')).toBeTruthy();
    expect(screen.getAllByText('新')).toHaveLength(1);
    await fireEvent.press(await screen.findByText('申请收录'));
    expect(mockPush).toHaveBeenCalledWith({ pathname: '/apply', params: { entry: 'bank' } });
    delete mockParams.from;
  });
});
