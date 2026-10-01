import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { describe, expect, it } from 'vitest';
import { App } from './App';
import { AppLayout } from './components/AppLayout';
import { maskPhone } from './lib/mask';
import { visibleMenu } from './lib/menu';

describe('后台壳', () => {
  it('账号密码 → 短信验证码 → 进入概览', async () => {
    render(
      <MemoryRouter>
        <App />
      </MemoryRouter>,
    );
    fireEvent.change(screen.getByLabelText('账号'), { target: { value: 'ops' } });
    fireEvent.change(screen.getByLabelText('密码'), { target: { value: 'pw' } });
    fireEvent.click(screen.getByRole('button', { name: '下一步' }));
    const code = await screen.findByLabelText('短信验证码');
    fireEvent.change(code, { target: { value: '123456' } });
    fireEvent.click(screen.getByRole('button', { name: '登 录' }));
    await waitFor(() => expect(screen.getByRole('heading', { name: '7.1 概览' })).toBeTruthy());
  });

  it('菜单按角色显示五组', () => {
    expect(visibleMenu(['admin']).map((g) => g.title)).toEqual(['运营', '质量', '配置', '官方题库', '系统']);
    expect(visibleMenu(['content_editor']).flatMap((g) => g.pages.map((p) => p.code))).toEqual(['7.12']);
    expect(visibleMenu(['analyst']).flatMap((g) => g.pages.map((p) => p.code))).toEqual(['7.1', '7.10']);
  });

  it('布局显示账号名', () => {
    render(
      <MemoryRouter>
        <AppLayout account={{ name: '客服小王', roles: ['support'] }} now={new Date(2026, 9, 1, 9, 5)}>
          <div>内容</div>
        </AppLayout>
      </MemoryRouter>,
    );
    expect(screen.getByText('客服小王')).toBeTruthy();
    expect(screen.getByText('内容')).toBeTruthy();
  });

  it('手机号脱敏', () => {
    expect(maskPhone('13812345678')).toBe('138****5678');
    expect(maskPhone('abc')).toBe('abc');
  });
});
