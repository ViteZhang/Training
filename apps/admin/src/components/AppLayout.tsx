import { useQuery } from '@tanstack/react-query';
import { Badge, Button, Layout, Menu, Space, Typography, Watermark } from 'antd';
import { useMemo, type ReactNode } from 'react';
import { Link, useLocation } from 'react-router';
import { api, unwrap } from '../lib/api';
import { visibleMenu, type Role } from '../lib/menu';

const { Sider, Header, Content } = Layout;

export interface AppLayoutProps {
  account: { name: string; roles: Role[] };
  children: ReactNode;
  /** 水印时间，测试时注入 */
  now?: Date;
  onLogout?: () => void;
}

/** 带侧边菜单的布局；每个后台页面叠加「账号名 + 时间」水印（dev-spec 第十节）。 */
export function AppLayout({ account, children, now = new Date(), onLogout }: AppLayoutProps) {
  const location = useLocation();
  const groups = useMemo(() => visibleMenu(account.roles), [account.roles]);
  // 「申请收录」菜单显示待处理数红点（PRD v3 增补「侧栏与推送」）
  const canApply = groups.some((g) => g.pages.some((p) => p.path === '/applications'));
  const applySummary = useQuery({
    queryKey: ['admin', 'applications', 'summary'],
    queryFn: () => unwrap(api.GET('/admin/course-applications/summary')),
    enabled: canApply,
    refetchInterval: 5 * 60_000,
  });
  const badge = (path: string) => (path === '/applications' ? applySummary.data?.pending ?? 0 : 0);
  const stamp = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')} ${String(now.getHours()).padStart(2, '0')}:${String(now.getMinutes()).padStart(2, '0')}`;
  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider width={220} theme="light" style={{ borderRight: '1px solid #E4DFD4' }}>
        <div style={{ padding: '20px 24px', fontWeight: 700 }}>考研Training 后台</div>
        <Menu
          mode="inline"
          selectedKeys={[location.pathname]}
          items={groups.map((g) => ({
            key: g.key,
            type: 'group',
            label: g.title,
            children: g.pages.map((p) => ({
              key: p.path,
              label: (
                <Link to={p.path}>
                  {`${p.code} ${p.title}`} {badge(p.path) > 0 ? <Badge count={badge(p.path)} size="small" style={{ marginLeft: 6 }} /> : null}
                </Link>
              ),
            })),
          }))}
        />
      </Sider>
      <Layout>
        <Header style={{ background: '#fff', borderBottom: '1px solid #E4DFD4', display: 'flex', justifyContent: 'flex-end' }}>
          <Space>
            <Typography.Text>{account.name}</Typography.Text>
            {onLogout ? (
              <Button type="link" onClick={onLogout}>
                退出
              </Button>
            ) : null}
          </Space>
        </Header>
        <Watermark content={[account.name, stamp]} font={{ color: 'rgba(35,31,85,0.08)' }}>
          <Content style={{ padding: 24, minHeight: 'calc(100vh - 64px)' }}>{children}</Content>
        </Watermark>
      </Layout>
    </Layout>
  );
}
