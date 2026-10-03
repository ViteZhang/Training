import { useQuery, useQueryClient } from '@tanstack/react-query';
import { App as AntApp, Spin } from 'antd';
import type { ReactNode } from 'react';
import { Navigate, Route, Routes } from 'react-router';
import { AppLayout } from './components/AppLayout';
import { api, unwrap } from './lib/api';
import { menu, type Role } from './lib/menu';
import { clearSession, useToken } from './lib/session';
import { ChangePassword } from './pages/ChangePassword';
import { Codes } from './pages/Codes';
import { Disputes } from './pages/Disputes';
import { Feedback } from './pages/Feedback';
import { Login } from './pages/Login';
import { Orders } from './pages/Orders';
import { Overview } from './pages/Overview';
import { Parse } from './pages/Parse';
import { Placeholder } from './pages/Placeholder';
import { Users } from './pages/Users';

/** 登录（两步验证）→ 首次登录改密码 → 按角色显示菜单与页面。权限只影响界面，安全由后端按契约的 x-roles 保证。 */
export function App() {
  const token = useToken();
  const qc = useQueryClient();
  const me = useQuery({ queryKey: ['admin', 'me', token], queryFn: () => unwrap(api.GET('/admin/me')), enabled: !!token, retry: false });
  if (!token) return <Login />;
  if (me.isPending) return <Spin style={{ display: 'block', margin: 96 }} />;
  if (me.isError || !me.data) {
    clearSession();
    return <Login />;
  }
  if (me.data.must_change_password) return <ChangePassword onDone={() => void qc.invalidateQueries({ queryKey: ['admin', 'me'] })} />;
  const roles = me.data.roles as Role[];
  const pages: Record<string, ReactNode> = {
    '/overview': <Overview />,
    '/users': <Users roles={roles} />,
    '/orders': <Orders roles={roles} />,
    '/codes': <Codes roles={roles} />,
    '/parse': <Parse />,
    '/disputes': <Disputes />,
    '/feedback': <Feedback />,
  };
  const visible = menu.flatMap((g) => g.pages).filter((p) => p.roles.some((r) => roles.includes(r)));
  const home = visible[0]?.path ?? '/overview';
  const logout = async () => {
    await api.POST('/admin/auth/logout').catch(() => undefined);
    clearSession();
    qc.clear();
  };
  return (
    <AntApp>
      <AppLayout account={{ name: me.data.display_name, roles }} onLogout={() => void logout()}>
        <Routes>
          <Route index element={<Navigate to={home} replace />} />
          {visible.map((p) => (
            <Route key={p.path} path={p.path} element={pages[p.path] ?? <Placeholder page={p} />} />
          ))}
          <Route path="*" element={<Navigate to={home} replace />} />
        </Routes>
      </AppLayout>
    </AntApp>
  );
}
