import { useQuery } from '@tanstack/react-query';
import * as Notifications from 'expo-notifications';
import { useEffect } from 'react';
import { api, unwrap } from './api';
import { appConfig } from './config';

/** 本地学习提醒的通知 ID 前缀：重排时只取消自己排的，不动别的通知。 */
const PREFIX = 'study-reminder:';

export type ReminderSettings = { reminder_times?: string[] | null; notify_daily: boolean };

/**
 * 按备考设置排本地学习提醒（T27）：每个提醒时间每天一条；关闭「每日训练提醒」或没有通知权限时全部取消。
 * 本地通知不经服务器，不依赖厂商推送通道（CLAUDE.md 必须遵守第 10 条）。返回排了几条。
 */
export async function syncReminders(s: ReminderSettings): Promise<number> {
  const scheduled = await Notifications.getAllScheduledNotificationsAsync();
  await Promise.all(scheduled.filter((n) => n.identifier.startsWith(PREFIX)).map((n) => Notifications.cancelScheduledNotificationAsync(n.identifier)));
  const times = s.notify_daily ? (s.reminder_times ?? []) : [];
  if (times.length === 0) return 0;
  const perm = await Notifications.getPermissionsAsync();
  if (!perm.granted) return 0;
  let n = 0;
  for (const t of times) {
    const m = /^(\d{1,2}):(\d{2})$/.exec(t);
    if (!m) continue;
    await Notifications.scheduleNotificationAsync({
      identifier: PREFIX + t,
      content: { title: appConfig.appName, body: '今天的训练准备好了，花几分钟把到期的内容过一遍', data: { page: 'today' } },
      trigger: { type: Notifications.SchedulableTriggerInputTypes.DAILY, hour: Number(m[1]), minute: Number(m[2]) },
    });
    n++;
  }
  return n;
}

/** 在登录后的主界面挂载：备考设置里的提醒时间或开关变了就重排（6.10 改设置后 profile 缓存会更新）。 */
export function useReminderSync(enabled: boolean) {
  const profile = useQuery({ queryKey: ['profile'], queryFn: () => unwrap(api.GET('/profile')), enabled });
  const times = profile.data?.reminder_times?.join(',');
  const on = profile.data?.notify_daily;
  useEffect(() => {
    if (on === undefined) return;
    syncReminders({ reminder_times: times ? times.split(',') : [], notify_daily: on }).catch(() => {
      // 模拟器或没有通知能力的设备上排不了，忽略。
    });
  }, [times, on]);
}
