import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import { AnswerEvidence } from '../features/answers/Evidence';
import { AnswerUpdate } from '../features/answers/AnswerUpdate';
import { TextbookSheet } from '../features/profile/textbooks';
import type { Schemas } from '@training/api-client';

jest.mock('expo-router', () => ({ router: { push: jest.fn() } }));
let mockUUID = 0;
jest.mock('expo-crypto', () => ({ randomUUID: () => `request-key-${++mockUUID}` }));
const mockCalls: { method: string; path: string; body?: unknown }[] = [];
let mockResponses: Record<string, () => { status: number; data?: unknown; error?: unknown }> = {};
jest.mock('../lib/api', () => {
  const actual = jest.requireActual('@training/api-client');
  const call = (method: string) => (path: string, init?: { body?: unknown }) => {
    mockCalls.push({ method, path, body: init?.body });
    const r = mockResponses[`${method} ${path}`]?.() ?? { status: 200, data: {} };
    return Promise.resolve({ data: r.data, error: r.error, response: new Response(null, { status: r.status }) });
  };
  return { api: { GET: call('GET'), POST: call('POST'), PATCH: call('PATCH') }, unwrap: actual.unwrap };
});
function wrap(ui: React.ReactElement) {
  return render(<SafeAreaProvider initialMetrics={{ frame: { x: 0, y: 0, width: 390, height: 844 }, insets: { top: 0, bottom: 0, left: 0, right: 0 } }}><QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })}>{ui}</QueryClientProvider></SafeAreaProvider>);
}
const snapshot: Schemas['AnswerEvidenceSnapshot'] = { version: 'v1', generated_at: '2026-10-30T10:00:00Z', model: 'model', prompt_version: 'v3', coverage: 'partial', uses_general_knowledge: true, sources: [{ id: 'src1', kind: 'official_textbook', title: '历史版本教材', edition: '第五版', heading: '文学形象 → 意境', quote: '情景交融与虚实相生', claim: '意境是情景交融的艺术境界' }] };
const question = { id: 1, answer: '原有答案', rubric_points: [{ id: 1, seq: 1, content: '原采分点', score: 5, origin: 'user_confirmed' }], rubric_version: 1 } as Schemas['QuestionDetail'];
const candidate = { id: 8, question_id: 1, status: 'ready', answer: '新的答案', points: [{ content: '新采分点', score: 5 }], snapshot, failure_message: '', created_at: '2026-10-30T10:00:00Z', updated_at: '2026-10-30T10:00:00Z' };
beforeEach(() => { mockCalls.length = 0; mockResponses = {}; mockUUID = 0; });

it('只显示快照中的教材版本、原文及实际支持内容，不编造纸质页码', async () => {
  await wrap(<AnswerEvidence snapshot={snapshot} />);
  expect(screen.getByText('教材依据 + AI 补充')).toBeTruthy();
  await fireEvent.press(screen.getByText('查看依据摘录 ›'));
  expect(screen.getByText('情景交融与虚实相生')).toBeTruthy();
  expect(screen.queryByText(/教材第.*页/)).toBeNull();
});
it('授权限制只隐藏摘录，保留历史身份信息', async () => {
  await wrap(<AnswerEvidence snapshot={{ ...snapshot, sources: snapshot.sources.map((s) => ({ ...s, quote: '', claim: '', unavailable: true })) }} />);
  await fireEvent.press(screen.getByText('查看依据摘录 ›'));
  expect(screen.getByText('依据摘录暂不可查看')).toBeTruthy();
  expect(screen.queryByText('情景交融与虚实相生')).toBeNull();
});
it('未匹配和匹配接口故障不影响自由书名保存，不显示缺口提示', async () => {
  mockResponses['POST /subjects/{subjectId}/textbook-matches'] = () => ({ status: 503, error: { code: 'INTERNAL', message: '服务不可用' } });
  mockResponses['PATCH /subjects/{subjectId}'] = () => ({ status: 200, data: {} });
  const saved = jest.fn();
  await wrap(<TextbookSheet subject={{ id: 1, name: '文学', textbooks: [] } as unknown as Schemas['Subject']} onClose={jest.fn()} onSaved={saved} />);
  await fireEvent.changeText(screen.getByLabelText('教材名称'), '未收录的讲义');
  await waitFor(() => expect(mockCalls.some((c) => c.path.endsWith('textbook-matches'))).toBe(true));
  expect(screen.queryByText('服务不可用')).toBeNull();
  expect(screen.queryByText('未匹配')).toBeNull();
  await fireEvent.press(screen.getByText('保存'));
  await waitFor(() => expect(saved).toHaveBeenCalled());
  expect(mockCalls.find((c) => c.method === 'PATCH')?.body).toEqual({ textbooks: ['未收录的讲义'], textbook_selections: [] });
});
it('生成前说明扣次，成功候选不自动覆盖；退出后恢复并允许采用', async () => {
  mockResponses['GET /questions/{questionId}/answer-evidence'] = () => ({ status: 200, data: { snapshot: null } });
  let created = false;
  mockResponses['GET /questions/{questionId}/answer-candidates'] = () => ({ status: 200, data: { items: created ? [candidate] : [] } });
  mockResponses['POST /questions/{questionId}/answer-candidates'] = () => { created = true; return { status: 200, data: candidate }; };
  mockResponses['POST /questions/{questionId}/answer-candidates/{candidateId}/adopt'] = () => ({ status: 200, data: { ...candidate, status: 'adopted' } });
  await wrap(<AnswerUpdate question={question} />);
  await fireEvent.press(screen.getByText('按当前教材重新生成'));
  expect(screen.getByText(/放弃候选不退次/)).toBeTruthy();
  expect(mockCalls.filter((c) => c.method === 'POST')).toHaveLength(0);
  await fireEvent.press(screen.getByText('生成新候选'));
  expect(await screen.findByText('新的答案')).toBeTruthy();
  expect(screen.getByText('原有答案')).toBeTruthy();
  expect(screen.getByText('新采分点')).toBeTruthy();
  expect(mockCalls.some((c) => c.path.endsWith('/adopt'))).toBe(false);
  await fireEvent.press(screen.getByText('稍后查看'));
  await fireEvent.press(screen.getByText('查看新答案候选'));
  await fireEvent.press(screen.getByText('采用新答案'));
  await waitFor(() => expect(mockCalls.some((c) => c.path.endsWith('/adopt'))).toBe(true));
});

it('创建候选遇到额度不足时沿用额度弹层并保留原答案', async () => {
  mockResponses['GET /questions/{questionId}/answer-candidates'] = () => ({ status: 200, data: { items: [] } });
  mockResponses['POST /questions/{questionId}/answer-candidates'] = () => ({ status: 429, error: { code: 'QUOTA_EXCEEDED', message: '次数用完了' } });
  await wrap(<AnswerUpdate question={question} />);
  await fireEvent.press(screen.getByText('按当前教材重新生成'));
  await fireEvent.press(screen.getByText('生成新候选'));
  expect(await screen.findByText('今天的 AI 出题次数用完了')).toBeTruthy();
  expect(screen.getByText('继续使用原答案')).toBeTruthy();
  expect(mockCalls.some((c) => c.path.endsWith('/adopt'))).toBe(false);
});
it('创建响应丢失后重试复用原幂等键', async () => {
  mockResponses['GET /questions/{questionId}/answer-candidates'] = () => ({ status: 200, data: { items: [] } });
  let calls = 0;
  mockResponses['POST /questions/{questionId}/answer-candidates'] = () => ++calls === 1 ? { status: 503, error: { code: 'INTERNAL', message: '服务繁忙' } } : { status: 200, data: candidate };
  await wrap(<AnswerUpdate question={question} />);
  await fireEvent.press(screen.getByText('按当前教材重新生成'));
  await fireEvent.press(screen.getByText('生成新候选'));
  await fireEvent.press(await screen.findByText('重试'));
  expect(await screen.findByText('新的答案')).toBeTruthy();
  const creates = mockCalls.filter((c) => c.method === 'POST' && c.path.endsWith('/answer-candidates'));
  expect(creates).toHaveLength(2);
  expect(creates[0]!.body).toEqual(creates[1]!.body);
});
