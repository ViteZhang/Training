import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import NoOfficial from '../../app/(onboarding)/no-official';
import Apply from '../../app/apply';
import Samples from '../../app/trial/samples';
import TrialPage from '../../app/trial/[id]';
import { saveDraft } from '../features/onboarding/draft';
import { courseNote, groupCourses, parseCourse, routeOf, RouteSubject } from '../features/official/RouteSubject';
import { promptText } from '../features/official/SupplementCard';

const mockPush = jest.fn();
const mockReplace = jest.fn();
let mockParams: Record<string, string> = {};
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

const flags = (on: string[]) => () => ({ status: 200, data: { flags: Object.fromEntries(on.map((k) => [k, true])) } });

beforeEach(() => {
  mockCalls.length = 0;
  mockPush.mockReset();
  mockReplace.mockReset();
  mockParams = {};
  mockResponses = {
    'GET /feature-flags': flags(['trial', 'sample_trial', 'course_apply']),
    'GET /trials/status': () => ({ status: 200, data: { limit: 1, used: 0, remaining: 1 } }),
    'GET /exam-years': () => ({ status: 200, data: { items: [{ exam_year: 2027, label: '2027 研考', suggested_stage: 'strengthen' }, { exam_year: 2028, label: '2028 研考', suggested_stage: 'foundation' }] } }),
    'GET /subjects': () => ({ status: 200, data: { items: [{ id: 5, name: '西方哲学史', code: '812', school: '四川大学', major: '哲学', is_essay: false }] } }),
    'GET /profile': () => ({ status: 200, data: { exam_year: 2027 } }),
    'PATCH /me': () => ({ status: 200, data: {} }),
    'GET /sample-questions': () => ({
      status: 200,
      data: { items: [{ name: '哲学', directions: [{ id: 3, name: '西方哲学', questions: [{ id: 11, qtype: 'term', score: 5, stem: '绝对命令', ai_generated: false }] }] }] },
    }),
  };
  saveDraft({ examYear: 2027, school: '四川大学', major: '哲学', noOfficial: [{ subjectId: 5, name: '西方哲学史', code: '812', major: '哲学' }], after: undefined });
});

describe('引导分流的纯函数（PRD v3 增补 1.1）', () => {
  it('代码 + 名称拆分', () => {
    expect(parseCourse('812 西方哲学史')).toEqual({ code: '812', name: '西方哲学史' });
    expect(parseCourse(' 中国哲学史 ')).toEqual({ code: '', name: '中国哲学史' });
  });
  it('有一门有官方题库就去 1.1a', () => {
    expect(routeOf([{ official_status: 'none' }, { official_status: 'available' }])).toBe('official');
    expect(routeOf([{ official_status: 'producing' }])).toBe('none');
    expect(routeOf([])).toBe('none');
  });
  it('补充资料提示文案按原因', () => {
    expect(promptText({ reason: 'section_gap', section_name: '近代哲学', official_count: 4 }).title).toBe('「近代哲学」你错得多，官方题只有 4 道');
    expect(promptText({ reason: 'dispute' }).title).toContain('老师');
  });
});

describe('1.1c 暂无官方题库', () => {
  it('三个入口；申请收录去 1.11', async () => {
    await wrap(<NoOfficial />);
    expect(await screen.findByText('用你手里的一道题，试试 AI 批改')).toBeTruthy();
    expect(screen.getByText('手边没有题？用示例题试试')).toBeTruthy();
    await fireEvent.press(screen.getByText('申请收录这门课'));
    expect(mockPush).toHaveBeenCalledWith({ pathname: '/apply', params: { entry: 'onboarding' } });
  });

  it('体验次数用完后前两个入口显示「已体验」', async () => {
    mockResponses['GET /trials/status'] = () => ({ status: 200, data: { limit: 1, used: 1, remaining: 0 } });
    await wrap(<NoOfficial />);
    await waitFor(() => expect(screen.getAllByText('已体验')).toHaveLength(2));
  });

  it('还没有上线的示例题时不显示示例题入口', async () => {
    mockResponses['GET /sample-questions'] = () => ({ status: 200, data: { items: [{ name: '哲学', directions: [{ id: 3, name: '西方哲学', questions: [] }] }] } });
    await wrap(<NoOfficial />);
    expect(await screen.findByText('用你手里的一道题，试试 AI 批改')).toBeTruthy();
    await waitFor(() => expect(mockCalls.some((c) => c.path === '/sample-questions')).toBe(true));
    expect(screen.queryByText('手边没有题？用示例题试试')).toBeNull();
  });

  it('开关都关闭时只剩导入资料', async () => {
    mockResponses['GET /feature-flags'] = flags([]);
    await wrap(<NoOfficial />);
    expect(await screen.findByText('直接导入我的资料')).toBeTruthy();
    expect(screen.queryByText('申请收录这门课')).toBeNull();
  });
});

describe('1.1 院校库里没有的院校', () => {
  it('可以直接用输入的院校名，手动添加专业课后去 1.1c', async () => {
    saveDraft({ examYear: undefined, school: undefined, picked: [], noOfficial: [] });
    mockResponses['GET /subjects'] = () => ({ status: 200, data: { items: [] } });
    mockResponses['GET /catalog/schools'] = () => ({ status: 200, data: { items: [] } });
    mockResponses['GET /catalog/courses'] = () => ({ status: 200, data: { items: [] } });
    mockResponses['POST /subjects'] = () => ({ status: 200, data: { id: 8, name: '文学理论', code: '831', is_essay: false } });
    mockResponses['PUT /profile'] = () => ({ status: 200, data: {} });
    await wrap(<RouteSubject />);
    await fireEvent.changeText(await screen.findByLabelText('搜索院校'), '南京大学');
    await fireEvent.press(await screen.findByText('就用「南京大学」'));
    expect(await screen.findByText('这所院校的自命题科目还没录入，手动添加后我们会尽快补上')).toBeTruthy();
    await fireEvent.press(screen.getByText('列表里没有？手动添加专业课'));
    await fireEvent.changeText(screen.getByLabelText('专业课代码和名称'), '831 文学理论');
    await fireEvent.press(screen.getByText('添加'));
    expect(await screen.findByText('手动添加')).toBeTruthy();
    await fireEvent.changeText(screen.getByLabelText('报考专业'), '中国语言文学');
    await fireEvent.press(await screen.findByText('下一步'));
    await waitFor(() => expect(mockPush).toHaveBeenCalledWith('/(onboarding)/no-official'));
    const post = mockCalls.find((c) => c.method === 'POST' && c.path === '/subjects')!;
    expect(post.init).toEqual({ body: { name: '文学理论', code: '831', full_score: 150, school: '南京大学', major: '中国语言文学' } });
  });
});

// T50 考试科目：一套卷子一条，专业只是选填；全国统考任何院校都能选。
const course = (id: number, code: string, name: string, discipline: string, extra: Record<string, unknown> = {}) => ({
  id, scope: 'school', school: '北京师范大学', major: '', majors: [], discipline, subject_name: name, subject_code: code, textbooks: [], official_status: 'none', ...extra,
});
const national = (id: number, code: string, name: string, discipline: string, extra: Record<string, unknown> = {}) =>
  course(id, code, name, discipline, { scope: 'national', school: '', registered: false, ...extra });

describe('1.1 考试科目分组', () => {
  it('本校科目少于 4 门直接列出，统考另列；4 门及以上按学科分组', () => {
    const few = groupCourses([course(1, '808', '中国语言文学', '文学'), national(2, '333', '教育综合', '教育学')] as never);
    expect(few.flat).toBe(true);
    expect(few.own.map((c) => c.id)).toEqual([1]);
    expect(few.others.map((c) => c.id)).toEqual([2]);
    // 登记过的统考算本校的。
    const many = groupCourses([
      course(1, '808', '中国语言文学', '文学'), course(2, '809', '外国语言文学', '文学'), course(3, '701', '哲学综合', '哲学'),
      national(4, '333', '教育综合', '教育学', { registered: true }), national(5, '313', '历史学基础', '历史学'),
    ] as never);
    expect(many.flat).toBe(false);
    expect(many.disciplines).toEqual(['文学', '哲学', '教育学', '历史学']);
    expect(courseNote({ scope: 'national', majors: [] })).toBe('全国统考');
    expect(courseNote({ scope: 'school', majors: ['文艺学', '古代文学'] })).toBe('文艺学、古代文学');
  });

  it('科目多时先选学科；选的统考科目带上目标院校和选填的专业', async () => {
    saveDraft({ examYear: undefined, school: undefined, picked: [], noOfficial: [], major: undefined });
    mockResponses['GET /subjects'] = () => ({ status: 200, data: { items: [] } });
    mockResponses['GET /catalog/schools'] = () => ({ status: 200, data: { items: [{ name: '北京师范大学', course_count: 4 }] } });
    mockResponses['GET /catalog/courses'] = () => ({
      status: 200,
      data: { items: [
        course(1, '808', '中国语言文学', '文学', { majors: ['文艺学', '古代文学'] }), course(2, '809', '外国语言文学', '文学'),
        course(3, '701', '哲学综合', '哲学'), course(4, '702', '马克思主义哲学', '哲学'), national(9, '333', '教育综合', '教育学'),
      ] },
    });
    mockResponses['POST /subjects'] = () => ({ status: 200, data: { id: 11, name: '教育综合', code: '333', is_essay: false } });
    mockResponses['PUT /profile'] = () => ({ status: 200, data: {} });
    await wrap(<RouteSubject />);
    await fireEvent.changeText(await screen.findByLabelText('搜索院校'), '北京师范');
    await fireEvent.press(await screen.findByText('北京师范大学'));
    // 默认第一个学科：只列文学的两门。
    expect(await screen.findByText('808 中国语言文学')).toBeTruthy();
    expect(screen.getByText('文艺学、古代文学')).toBeTruthy();
    expect(screen.queryByText('701 哲学综合')).toBeNull();
    const call = mockCalls.find((c) => c.path === '/catalog/courses')!;
    expect(call.init).toEqual({ params: { query: { school: '北京师范大学', exam_year: 2027, include_national: true } } });
    await fireEvent.press(screen.getByText('教育学'));
    expect(screen.queryByText('808 中国语言文学')).toBeNull();
    await fireEvent.press(await screen.findByText('333 教育综合'));
    expect(screen.getByText('全国统考')).toBeTruthy();
    await fireEvent.changeText(screen.getByLabelText('报考专业'), '教育学原理');
    await fireEvent.press(screen.getByText('下一步'));
    await waitFor(() => expect(mockPush).toHaveBeenCalledWith('/(onboarding)/no-official'));
    const post = mockCalls.find((c) => c.method === 'POST' && c.path === '/subjects')!;
    expect(post.init).toEqual({ body: { name: '教育综合', code: '333', full_score: 150, catalog_course_id: 9, school: '北京师范大学', major: '教育学原理' } });
    const profile = mockCalls.find((c) => c.method === 'PUT' && c.path === '/profile')!;
    expect((profile.init as { body: { target_school_major: string } }).body.target_school_major).toBe('北京师范大学 · 教育学原理');
  });

  it('库里没有的院校列出统考；手动填 333 时提示直接选统考', async () => {
    saveDraft({ examYear: undefined, school: undefined, picked: [], noOfficial: [], major: undefined });
    mockResponses['GET /subjects'] = () => ({ status: 200, data: { items: [] } });
    mockResponses['GET /catalog/schools'] = () => ({ status: 200, data: { items: [] } });
    mockResponses['GET /catalog/courses'] = () => ({ status: 200, data: { items: [national(9, '333', '教育综合', '教育学'), national(8, '311', '教育学专业基础', '教育学')] } });
    await wrap(<RouteSubject />);
    await fireEvent.changeText(await screen.findByLabelText('搜索院校'), '某冷门师范学院');
    await fireEvent.press(await screen.findByText('就用「某冷门师范学院」'));
    expect(await screen.findByText('全国统考科目（各院校同一套卷子）')).toBeTruthy();
    expect(screen.getByText('311 教育学专业基础')).toBeTruthy();
    await fireEvent.press(screen.getByText('列表里没有？手动添加专业课'));
    await fireEvent.changeText(screen.getByLabelText('专业课代码和名称'), '333 教育综合');
    await fireEvent.press(await screen.findByText('是全国统考的「333 教育综合」吗？'));
    expect(screen.getByRole('checkbox', { name: /333 教育综合/ }).props.accessibilityState).toEqual({ checked: true });
    expect(screen.queryByText('手动添加')).toBeNull();
  });
});

describe('1.10 示例题', () => {
  it('还没有示例题时给出口，不卡在这一页', async () => {
    mockParams = { from: 'onboarding' };
    mockResponses['GET /sample-questions'] = () => ({ status: 200, data: { items: [] } });
    await wrap(<Samples />);
    expect(await screen.findByText('示例题还在准备')).toBeTruthy();
    await fireEvent.press(screen.getByText('用我手里的一道题试试'));
    expect(mockReplace).toHaveBeenCalledWith({ pathname: '/trial/input', params: { subjectId: undefined } });
    await fireEvent.press(screen.getByText('导入我的资料'));
    await waitFor(() => expect(mockReplace).toHaveBeenCalledWith('/(onboarding)/import'));
  });

  it('示例题开关关闭（接口 404）时同样给出口', async () => {
    mockResponses['GET /feature-flags'] = flags([]);
    mockResponses['GET /sample-questions'] = () => ({ status: 404, error: { code: 'NOT_FOUND', message: '不存在' } });
    await wrap(<Samples />);
    expect(await screen.findByText('示例题还在准备')).toBeTruthy();
    expect(screen.queryByText('用我手里的一道题试试')).toBeNull();
    expect(screen.getByText('先进入 App')).toBeTruthy();
  });
});

describe('1.11 申请收录', () => {
  it('从 1.1 带入院校专业课，提交后去 1.11a 显示人数', async () => {
    mockParams = { entry: 'onboarding', subjectId: '5' };
    mockResponses['POST /course-applications'] = () => ({ status: 200, data: { applicant_count: 3 } });
    await wrap(<Apply />);
    expect(await screen.findByDisplayValue('四川大学')).toBeTruthy();
    expect(screen.getByDisplayValue('西方哲学史')).toBeTruthy();
    await fireEvent.press(screen.getByText('提交申请'));
    await waitFor(() => expect(mockReplace).toHaveBeenCalled());
    const post = mockCalls.find((c) => c.method === 'POST')!;
    expect(post.init).toEqual({ body: { school: '四川大学', major: '哲学', subject_name: '西方哲学史', subject_code: '812', exam_year: 2027, entry: 'onboarding' } });
    expect(mockReplace).toHaveBeenCalledWith({ pathname: '/apply/done', params: expect.objectContaining({ count: '3' }) });
  });

  it('重复申请提示你已申请过', async () => {
    mockParams = { entry: 'bank', subjectId: '5' };
    mockResponses['POST /course-applications'] = () => ({ status: 409, error: { code: 'CONFLICT', message: '你已申请过这门课', detail: { reason: 'already_applied', applicant_count: 4 } } });
    await wrap(<Apply />);
    await fireEvent.press(await screen.findByText('提交申请'));
    await waitFor(() => expect(mockReplace).toHaveBeenCalledWith({ pathname: '/apply/done', params: expect.objectContaining({ again: '1', count: '4' }) }));
  });
});

describe('1.9b 体验结果', () => {
  const graded = {
    id: 7, kind: 'own', qtype: 'short_answer', score: 15, stem: '简述康德的「哥白尼式革命」', answer_source: 'ai', point_count: 2, status: 'graded', remaining: 0, total: 8,
    points: [
      { seq: 1, content: '对象符合认识', score: 8, got: 8, verdict: 'hit', reason: '写到了' },
      { seq: 2, content: '认识限于现象', score: 7, got: 0, verdict: 'miss' },
    ],
    suggestions: [],
  };

  it('标按 AI 参考答案批改；只加入这道题后先走 1.2', async () => {
    mockParams = { id: '7', subjectId: '5' };
    mockResponses['GET /trials/{trialId}'] = () => ({ status: 200, data: graded });
    mockResponses['POST /trials/{trialId}/save'] = () => ({ status: 200, data: { question_id: 99 } });
    await wrap(<TrialPage />);
    expect(await screen.findByText('按 AI 参考答案批改 · 仅供参考')).toBeTruthy();
    expect(screen.getByText('命中 1 · 部分 0 · 遗漏 1')).toBeTruthy();
    expect(screen.getByText(/会存进「西方哲学史」/)).toBeTruthy();
    await fireEvent.press(screen.getByText('只加入这道题'));
    await waitFor(() => expect(mockReplace).toHaveBeenCalledWith('/(onboarding)/target'));
    const save = mockCalls.find((c) => c.path === '/trials/{trialId}/save')!;
    expect(save.init).toEqual({ params: { path: { trialId: 7 } }, body: { action: 'only', subject_id: 5 } });
  });

  it('提交前只显示采分点个数', async () => {
    mockParams = { id: '7' };
    mockResponses['GET /trials/{trialId}'] = () => ({ status: 200, data: { ...graded, status: 'ready', points: undefined, total: undefined } });
    await wrap(<TrialPage />);
    expect(await screen.findByText('提交批改（不扣额度）')).toBeTruthy();
    expect(screen.queryByText('对象符合认识')).toBeNull();
  });
});
