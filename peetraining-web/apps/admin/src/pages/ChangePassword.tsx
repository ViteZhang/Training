// 首次登录必须修改初始密码（后端在改密码前拒绝其他后台接口）。
import { ApiError } from '@training/api-client';
import { Alert, Button, Card, Form, Input, Typography } from 'antd';
import { useState } from 'react';
import { api, unwrap } from '../lib/api';
import { clearSession } from '../lib/session';

export function ChangePassword({ onDone }: { onDone: () => void }) {
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  return (
    <div style={{ minHeight: '100vh', display: 'grid', placeItems: 'center', background: '#FAF8F3' }}>
      <Card style={{ width: 400 }}>
        <Typography.Title level={4}>修改初始密码</Typography.Title>
        <Typography.Paragraph type="secondary">首次登录请先设置自己的密码：至少 10 位，同时包含字母和数字</Typography.Paragraph>
        {error ? <Alert type="error" message={error} style={{ marginBottom: 16 }} /> : null}
        <Form
          layout="vertical"
          onFinish={async (v: { old_password: string; new_password: string }) => {
            setBusy(true);
            setError(undefined);
            try {
              await unwrap(api.POST('/admin/me/password', { body: v }));
              onDone();
            } catch (e) {
              setError(e instanceof ApiError ? e.message : '修改失败，请重试');
            } finally {
              setBusy(false);
            }
          }}
        >
          <Form.Item label="原密码" name="old_password" rules={[{ required: true, message: '请输入原密码' }]}>
            <Input.Password autoComplete="current-password" />
          </Form.Item>
          <Form.Item
            label="新密码"
            name="new_password"
            rules={[{ required: true, pattern: /^(?=.*[A-Za-z])(?=.*\d).{10,}$/, message: '至少 10 位，同时包含字母和数字' }]}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
          <Button type="primary" htmlType="submit" block loading={busy}>
            保存
          </Button>
          <Button type="link" block onClick={clearSession}>
            退出
          </Button>
        </Form>
      </Card>
    </div>
  );
}
