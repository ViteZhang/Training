import { flush, setAnalyticsContext, track } from '../lib/analytics';
import { useSession } from '../lib/session';
import { getJSON, remove } from '../lib/storage';

const mockPost = jest.fn();
jest.mock('../lib/api', () => ({ api: { POST: (...a: unknown[]) => mockPost(...a) } }));

const session = { accessToken: 'a', accessExpiresAt: '2099-01-01T00:00:00Z', refreshToken: 'r', refreshExpiresAt: '2099-01-01T00:00:00Z' };

beforeEach(() => {
  mockPost.mockReset();
  remove('analytics_queue');
  useSession.getState().setSession(session);
});

describe('埋点（T32）', () => {
  it('攒在本地，批量上报公共字段；长文本属性不上报', async () => {
    mockPost.mockResolvedValue({ response: { status: 200 } });
    setAnalyticsContext({ subjectCode: '654', stage: 'strengthen' });
    track('answer_submit', { kind: 'typed', duration_sec: 95, note: '长'.repeat(65) });
    track('session_finish');
    expect(mockPost).not.toHaveBeenCalled();
    await flush();
    expect(mockPost).toHaveBeenCalledTimes(1);
    const [path, init] = mockPost.mock.calls[0] as [string, { body: { subject_code: string; stage: string; events: { name: string; props?: Record<string, unknown> }[] } }];
    expect(path).toBe('/events');
    expect(init.body.subject_code).toBe('654');
    expect(init.body.stage).toBe('strengthen');
    expect(init.body.events.map((e) => e.name)).toEqual(['answer_submit', 'session_finish']);
    expect(init.body.events[0]!.props).toEqual({ kind: 'typed', duration_sec: 95 });
    expect(getJSON('analytics_queue')).toEqual([]);
  });

  it('满 20 条立即上报', async () => {
    mockPost.mockResolvedValue({ response: { status: 200 } });
    for (let i = 0; i < 20; i++) track('reveal_answer');
    await Promise.resolve();
    expect(mockPost).toHaveBeenCalledTimes(1);
  });

  it('网络失败或服务端出错时留着下次再报；没登录时不报', async () => {
    mockPost.mockRejectedValueOnce(new Error('offline'));
    track('redeem');
    await flush();
    expect(getJSON<unknown[]>('analytics_queue')).toHaveLength(1);
    mockPost.mockResolvedValueOnce({ response: { status: 503 } });
    await flush();
    expect(getJSON<unknown[]>('analytics_queue')).toHaveLength(1);
    useSession.getState().clear();
    mockPost.mockClear();
    await flush();
    expect(mockPost).not.toHaveBeenCalled();
  });
});
