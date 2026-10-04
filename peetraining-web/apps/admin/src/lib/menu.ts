/** 后台页面清单，按设计稿 7 模块分五组（PRD 10.2）。roles 只影响界面显示，安全由后端保证。 */
export type Role = 'admin' | 'support' | 'content_lead' | 'content_editor' | 'analyst';

export interface PageDef {
  code: string;
  title: string;
  path: string;
  roles: Role[];
}

export interface MenuGroup {
  key: string;
  title: string;
  pages: PageDef[];
}

const all: Role[] = ['admin', 'support', 'content_lead', 'content_editor', 'analyst'];

export const menu: MenuGroup[] = [
  {
    key: 'ops',
    title: '运营',
    pages: [
      { code: '7.1', title: '概览', path: '/overview', roles: ['admin', 'analyst'] },
      { code: '7.2', title: '用户', path: '/users', roles: ['admin', 'support'] },
      { code: '7.3', title: '会员与订单', path: '/orders', roles: ['admin', 'support'] },
      { code: '7.4', title: '兑换码', path: '/codes', roles: ['admin', 'support'] },
    ],
  },
  {
    key: 'quality',
    title: '质量',
    pages: [
      { code: '7.5', title: '资料解析监控', path: '/parse', roles: ['admin', 'support'] },
      { code: '7.6', title: '批改异议', path: '/disputes', roles: ['admin', 'support'] },
      { code: '7.7', title: '用户反馈', path: '/feedback', roles: ['admin', 'support'] },
    ],
  },
  {
    key: 'config',
    title: '配置',
    pages: [
      { code: '7.8', title: 'AI 与额度', path: '/config', roles: ['admin'] },
      { code: '7.9', title: '消息与公告', path: '/notice', roles: ['admin'] },
    ],
  },
  {
    key: 'official',
    title: '官方题库',
    pages: [
      { code: '7.10', title: '需求洞察', path: '/demand', roles: ['admin', 'content_lead', 'analyst'] },
      { code: '7.11', title: '官方题库', path: '/official', roles: ['admin', 'content_lead'] },
      { code: '7.12', title: '内容生产', path: '/produce', roles: ['admin', 'content_lead', 'content_editor'] },
      { code: '7.13', title: '审核队列', path: '/review', roles: ['admin', 'content_lead'] },
      { code: '7.14', title: '版本发布', path: '/release', roles: ['admin', 'content_lead'] },
    ],
  },
  {
    key: 'system',
    title: '系统',
    pages: [{ code: '7.15', title: '后台账号与权限', path: '/roles', roles: ['admin'] }],
  },
];

/** 某组角色能看到的菜单。 */
export function visibleMenu(roles: Role[]): MenuGroup[] {
  return menu
    .map((g) => ({ ...g, pages: g.pages.filter((p) => p.roles.some((r) => roles.includes(r))) }))
    .filter((g) => g.pages.length > 0);
}

export const allRoles = all;
