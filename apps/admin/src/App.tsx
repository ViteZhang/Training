import { useState } from 'react';
import { Navigate, Route, Routes } from 'react-router';
import { AppLayout } from './components/AppLayout';
import { allRoles, menu } from './lib/menu';
import { Login } from './pages/Login';
import { Placeholder } from './pages/Placeholder';

/** T02：登录壳与布局。登录态与账号角色在 T28 接后端，这里登录后先以管理员身份进入。 */
export function App() {
  const [account, setAccount] = useState<{ name: string; roles: typeof allRoles } | undefined>();
  if (!account) return <Login onLoggedIn={() => setAccount({ name: '管理员', roles: ['admin'] })} />;
  const pages = menu.flatMap((g) => g.pages);
  return (
    <AppLayout account={account}>
      <Routes>
        <Route index element={<Navigate to="/overview" replace />} />
        {pages.map((p) => (
          <Route key={p.path} path={p.path} element={<Placeholder page={p} />} />
        ))}
        <Route path="*" element={<Navigate to="/overview" replace />} />
      </Routes>
    </AppLayout>
  );
}
