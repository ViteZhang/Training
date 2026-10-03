import { Alert, Space, Spin, Typography } from 'antd';
import type { ReactNode } from 'react';
import { ApiError } from '@training/api-client';

/** 页面标题（带页面编号）与右侧操作。 */
export function PageTitle({ code, title, extra }: { code: string; title: string; extra?: ReactNode }) {
  return (
    <Space style={{ width: '100%', justifyContent: 'space-between', marginBottom: 16 }}>
      <Typography.Title level={3} style={{ margin: 0 }}>
        {code} {title}
      </Typography.Title>
      {extra}
    </Space>
  );
}

/** 加载中与错误状态。 */
export function Loadable({ loading, error, children }: { loading: boolean; error: unknown; children: ReactNode }) {
  if (loading) return <Spin style={{ display: 'block', margin: 48 }} />;
  if (error) return <Alert type="error" message={error instanceof ApiError ? error.message : '加载失败，请刷新重试'} />;
  return <>{children}</>;
}

/** 底部隐私说明（设计稿）。 */
export function PrivacyNote() {
  return (
    <Typography.Paragraph type="secondary" style={{ marginTop: 24 }}>
      后台看不到用户资料和题目的内容，只看统计和状态。排查问题需要用户在反馈或批改异议里授权，授权 72 小时内可看，每次查看都会通知用户并留痕。
    </Typography.Paragraph>
  );
}
