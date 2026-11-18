import { useState } from 'react';

/**
 * 弹窗打开时，缓存里可能是之前的旧数据（版本号、状态都过期）。
 * 返回 true 表示数据是本次打开后从服务端取回的，或者缓存本来就不过期；在此之前按加载中处理，避免拿旧版本号提交。
 */
export function useFreshSinceMount(q: { dataUpdatedAt: number; isFetching: boolean }) {
  const [mountedAt] = useState(() => Date.now());
  return q.dataUpdatedAt >= mountedAt || !q.isFetching;
}
