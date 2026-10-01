import { QueryClient } from '@tanstack/react-query';
import { ApiError } from '@training/api-client';

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // 4xx 不重试（404 是越权或开关关闭，402 是额度不足），网络错误与 5xx 重试 2 次。
      retry: (count, err) => !(err instanceof ApiError && err.status >= 400 && err.status < 500) && count < 2,
      staleTime: 30_000,
    },
  },
});
