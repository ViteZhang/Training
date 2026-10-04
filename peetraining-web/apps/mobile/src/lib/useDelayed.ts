import { timing } from '@training/ui-tokens';
import { useEffect, useState } from 'react';

/** 加载超过 300 毫秒才返回 true，避免骨架屏闪烁（PRD 14）。 */
export function useDelayed(active: boolean, delayMs: number = timing.skeletonDelayMs): boolean {
  const [shown, setShown] = useState(false);
  useEffect(() => {
    if (!active) return;
    const t = setTimeout(() => setShown(true), delayMs);
    return () => {
      clearTimeout(t);
      setShown(false);
    };
  }, [active, delayMs]);
  return active && shown;
}
