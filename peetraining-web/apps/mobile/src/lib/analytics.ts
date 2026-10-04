// 埋点（PRD 15 节，T32）：事件先存进本地队列（MMKV），攒够 20 条、隔 30 秒或 App 切到后台时批量上报 POST /events。
// 只上报事件名和短的标量属性（题型、用时、来源等），绝不带作答原文、资料内容和手机号；服务端还会再过滤一遍。
// 公共字段里的用户 ID 由服务端从登录态取；专业课代码和备考阶段由 setAnalyticsContext 设置。
import { AppState, Platform } from 'react-native';
import { api } from './api';
import { appConfig } from './config';
import { useSession } from './session';
import { getJSON, setJSON } from './storage';

export type EventName =
  | 'app_open' | 'login_success' | 'onboarding_step' | 'subject_add' | 'target_set' | 'stage_set'
  | 'import_start' | 'import_done' | 'import_fail' | 'confirm_edit' | 'confirm_submit'
  | 'plan_generate' | 'plan_complete' | 'score_estimate_view' | 'stage_change' | 'dashboard_view'
  | 'session_start' | 'session_finish' | 'answer_submit' | 'reveal_answer' | 'loss_attribution' | 'recite_finish'
  | 'paper_start' | 'time_reminder_shown' | 'paper_submit' | 'time_report_view' | 'essay_submit' | 'essay_rewrite'
  | 'grade_result' | 'grade_dispute' | 'grade_recheck' | 'rubric_edit' | 'question_report'
  | 'quota_block' | 'member_page_view' | 'pay_success' | 'redeem' | 'invite_success' | 'export'
  | 'survey_submit' | 'official_bank_add';

type Props = Record<string, string | number | boolean | undefined>;
interface Queued {
  name: EventName;
  at: string;
  props?: Record<string, string | number | boolean>;
}

const KEY = 'analytics_queue';
const BATCH = 20;
const MAX_QUEUE = 500;
const INTERVAL = 30_000;
// 测试环境不起定时器。
const isTest = typeof process !== 'undefined' && !!process.env?.JEST_WORKER_ID;

let context: { subject_code?: string; stage?: string } = {};
let timer: ReturnType<typeof setTimeout> | undefined;
let flushing = false;

/** 设置公共字段：当前专业课代码、备考阶段。 */
export function setAnalyticsContext(c: { subjectCode?: string | null; stage?: string | null }) {
  context = { subject_code: c.subjectCode ?? undefined, stage: c.stage ?? undefined };
}

function queue(): Queued[] {
  return getJSON<Queued[]>(KEY) ?? [];
}

/** 记一条事件。属性只留短的标量值。 */
export function track(name: EventName, props?: Props) {
  const clean: Record<string, string | number | boolean> = {};
  for (const [k, v] of Object.entries(props ?? {})) {
    if (v === undefined) continue;
    if (typeof v === 'string' && v.length > 64) continue;
    clean[k] = v;
  }
  const q = [...queue(), { name, at: new Date().toISOString(), props: Object.keys(clean).length ? clean : undefined }].slice(-MAX_QUEUE);
  setJSON(KEY, q);
  if (q.length >= BATCH) void flush();
  else if (!timer && !isTest) timer = setTimeout(() => void flush(), INTERVAL);
}

/** 上报队列里的事件；没登录时先留着。网络错误保留下次再报，服务端拒绝（4xx）的整批丢掉。 */
export async function flush() {
  if (timer) clearTimeout(timer);
  timer = undefined;
  if (flushing || !useSession.getState().session) return;
  const q = queue();
  if (q.length === 0) return;
  flushing = true;
  const batch = q.slice(0, 100);
  try {
    const res = await api.POST('/events', {
      body: { app_version: appConfig.version, platform: Platform.OS === 'ios' ? 'ios' : 'android', ...context, events: batch },
    });
    const status = res.response?.status ?? 0;
    if (status < 500 && status !== 0 && status !== 401) setJSON(KEY, queue().slice(batch.length));
  } catch {
    // 网络错误：留着下次再报。
  } finally {
    flushing = false;
  }
  if (queue().length > 0 && !isTest) timer = setTimeout(() => void flush(), INTERVAL);
}

if (!isTest) {
  AppState.addEventListener('change', (s) => {
    if (s === 'background') void flush();
  });
}
