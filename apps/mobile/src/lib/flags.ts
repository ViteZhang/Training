import { useQuery } from '@tanstack/react-query';
import { api, unwrap } from './api';
import { getJSON, setJSON } from './storage';

/** 功能开关名（PRD 3.4，默认关闭）。 */
export type FlagKey = 'online_payment' | 'official_bank' | 'oral_recite' | 'voice_answer' | 'scanned_pdf' | 'invite';

const CACHE_KEY = 'feature_flags';

/**
 * 功能开关从服务端读取，关闭的功能不显示入口（CLAUDE.md 必须遵守第 8 条）。
 * 离线时用上次缓存；从没拉到过时一律视为关闭。
 */
export function useFeatureFlag(key: FlagKey): boolean {
  const { data } = useQuery({
    queryKey: ['feature-flags'],
    queryFn: async () => {
      const res = await unwrap(api.GET('/feature-flags'));
      setJSON(CACHE_KEY, res.flags);
      return res.flags;
    },
    placeholderData: () => getJSON<Record<string, boolean>>(CACHE_KEY),
    staleTime: 5 * 60_000,
  });
  return data?.[key] === true;
}
