// 登录后：记一次 app_open，设置埋点公共字段（第一门专业课的代码、备考阶段），切回前台时上报（T32）。
import { useQuery } from '@tanstack/react-query';
import { useEffect } from 'react';
import { flush, setAnalyticsContext, track } from './analytics';
import { api, unwrap } from './api';

export function useAnalytics(enabled: boolean) {
  const profile = useQuery({ queryKey: ['profile'], queryFn: () => unwrap(api.GET('/profile')), enabled });
  const subjects = useQuery({ queryKey: ['subjects'], queryFn: () => unwrap(api.GET('/subjects')), enabled });
  const code = subjects.data?.items.find((s) => !s.is_essay)?.code;
  const stage = profile.data?.stage;
  useEffect(() => {
    setAnalyticsContext({ subjectCode: code, stage });
  }, [code, stage]);
  useEffect(() => {
    if (!enabled) return;
    track('app_open');
    void flush();
  }, [enabled]);
}
