import { useQuery } from '@tanstack/react-query';
import { Platform } from 'react-native';
import { api, unwrap } from './api';
import { getJSON, setJSON } from './storage';

/** 功能开关名（PRD 3.4，默认关闭）。 */
export type FlagKey =
  | 'online_payment' | 'official_bank' | 'oral_recite' | 'voice_answer' | 'scanned_pdf' | 'invite'
  // 官方题库先行（PRD v3 增补）
  | 'onboarding_route' | 'trial' | 'sample_trial' | 'ai_live_question' | 'course_apply';

const CACHE_KEY = 'feature_flags';

/**
 * H5 版暂不支持的功能，不管服务端开没开都按关闭处理（入口不显示）：
 * 录音（口述背诵、语音作答）在浏览器里格式与权限不一致；在线支付要另接网页支付；邀请海报要截图和系统分享。
 */
const WEB_UNSUPPORTED: ReadonlySet<FlagKey> = new Set(['oral_recite', 'voice_answer', 'online_payment', 'invite']);

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
  if (Platform.OS === 'web' && WEB_UNSUPPORTED.has(key)) return false;
  return data?.[key] === true;
}
