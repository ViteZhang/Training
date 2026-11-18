import { Empty, Typography } from 'antd';
import type { PageDef } from '../lib/menu';

/** 后台页面占位：各页面在 T28–T30 实现。 */
export function Placeholder({ page }: { page: PageDef }) {
  return (
    <>
      <Typography.Title level={3}>{`${page.code} ${page.title}`}</Typography.Title>
      <Empty description="页面开发中" />
    </>
  );
}
