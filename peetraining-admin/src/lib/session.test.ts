import { afterEach, describe, expect, it } from 'vitest';
import { clearSession, getToken, setSession } from './session';

afterEach(() => clearSession());

describe('后台会话可多开标签页', () => {
  it('令牌存在 localStorage，新标签页能读到', () => {
    setSession('t1');
    expect(localStorage.getItem('admin_token')).toBe('t1');
    clearSession();
    expect(localStorage.getItem('admin_token')).toBeNull();
  });

  it('别的标签页登录、退出时本页跟着变', () => {
    localStorage.setItem('admin_token', 't2');
    window.dispatchEvent(new StorageEvent('storage', { key: 'admin_token', newValue: 't2' }));
    expect(getToken()).toBe('t2');
    localStorage.removeItem('admin_token');
    window.dispatchEvent(new StorageEvent('storage', { key: 'admin_token', newValue: null }));
    expect(getToken()).toBeUndefined();
  });
});
