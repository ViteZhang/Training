// 离线作答（T17）：会话（含客观题答案）缓存在本地，飞行模式下照常作答、本地判分；
// 作答先进待提交队列，联网后按顺序补交，服务端复核（offline=true，answered_at 为作答时间）。
import type { Schemas } from '@training/api-client';
import { ApiError } from '@training/api-client';
import { useEffect, useState } from 'react';
import { AppState } from 'react-native';
import { create } from 'zustand';
import { api, unwrap } from '@/lib/api';
import { getJSON, setJSON } from '@/lib/storage';

type Body = Schemas['SubmitAttemptRequest'];
export interface Pending {
  sessionId: number;
  body: Body;
}

const QUEUE = 'practice:pending';
const sessionKey = (id: number) => `practice:session:${id}`;

export const usePending = create<{ count: number; set: (n: number) => void }>((set) => ({
  count: getJSON<Pending[]>(QUEUE)?.length ?? 0,
  set: (count) => set({ count }),
}));

function read(): Pending[] {
  return getJSON<Pending[]>(QUEUE) ?? [];
}
function write(list: Pending[]) {
  setJSON(QUEUE, list);
  usePending.getState().set(list.length);
}

export function cacheSession(s: Schemas['PracticeSession']) {
  setJSON(sessionKey(s.id), s);
}
export function cachedSession(id: number) {
  return getJSON<Schemas['PracticeSession']>(sessionKey(id));
}

function post(sessionId: number, body: Body) {
  return unwrap(api.POST('/practice-sessions/{sessionId}/attempts', { params: { path: { sessionId } }, body }));
}

/** 提交作答：联网直接提交；网络不通时进队列，返回 null，等联网后补交。 */
export async function submitAttempt(sessionId: number, body: Body): Promise<Schemas['AttemptResult'] | null> {
  // 队列里还有没交的，先按顺序补交，保证掌握分按作答顺序计算。
  if (read().length > 0) await flush();
  try {
    if (read().length > 0) throw new ApiError(0, undefined);
    return await post(sessionId, body);
  } catch (e) {
    if (e instanceof ApiError && e.isNetwork) {
      write([...read(), { sessionId, body: { ...body, offline: true, answered_at: body.answered_at ?? new Date().toISOString() } }]);
      return null;
    }
    throw e;
  }
}

let flushing: Promise<number> | null = null;

/** 按顺序补交离线作答，遇到网络错误就停；服务端拒绝的（如题目已删除）丢弃，避免卡住队列。返回补交成功的条数。 */
export function flush(): Promise<number> {
  if (flushing) return flushing;
  flushing = (async () => {
    let sent = 0;
    try {
      for (;;) {
        const list = read();
        const next = list[0];
        if (!next) break;
        try {
          await post(next.sessionId, next.body);
          sent++;
        } catch (e) {
          if (e instanceof ApiError && e.isNetwork) break;
        }
        write(read().slice(1));
      }
    } finally {
      flushing = null;
    }
    return sent;
  })();
  return flushing;
}

/** App 回到前台时补交离线作答（TabsLayout 里调用一次）。 */
export function useFlushOnForeground() {
  useEffect(() => {
    const sub = AppState.addEventListener('change', (s) => {
      if (s === 'active' && read().length > 0) void flush();
    });
    if (read().length > 0) void flush();
    return () => sub.remove();
  }, []);
}

const AI_FILL = 'practice:daily_ai_fill';

/** 今日训练「题量不够时 AI 补题」的开关，记在本机（默认关闭，打开才会生成变式题、计 AI 出题额度）。 */
export function useAIFillPref(): [boolean, (v: boolean) => void] {
  const [v, setV] = useState(() => getJSON<boolean>(AI_FILL) ?? false);
  return [
    v,
    (next: boolean) => {
      setJSON(AI_FILL, next);
      setV(next);
    },
  ];
}
