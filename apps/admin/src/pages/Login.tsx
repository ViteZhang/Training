// 后台登录：账号密码 + 短信两步验证，所有后台账号强制开启（PRD 10.1）。15 分钟内错 5 次暂停登录。
import { ApiError } from '@training/api-client';
import { Alert, Button, Card, Form, Input, Space, Typography } from 'antd';
import { useState } from 'react';
import { api, unwrap } from '../lib/api';
import { setSession } from '../lib/session';

export function Login() {
  const [challenge, setChallenge] = useState<{ id: string; phone: string }>();
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    setError(undefined);
    try {
      await fn();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : '登录失败，请重试');
    } finally {
      setBusy(false);
    }
  };
  return (
    <div style={{ minHeight: '100vh', display: 'grid', placeItems: 'center', background: '#FAF8F3' }}>
      <Card style={{ width: 380 }}>
        <Typography.Title level={4}>考研Training 运营后台</Typography.Title>
        <Typography.Paragraph type="secondary">仅限内部账号；登录需短信两步验证</Typography.Paragraph>
        {error ? <Alert type="error" message={error} style={{ marginBottom: 16 }} /> : null}
        {!challenge ? (
          <Form
            layout="vertical"
            onFinish={(v: { username: string; password: string }) =>
              run(async () => {
                const r = await unwrap(api.POST('/admin/auth/login', { body: v }));
                setChallenge({ id: r.challenge_id, phone: r.phone_masked });
              })
            }
          >
            <Form.Item label="账号" name="username" rules={[{ required: true, message: '请输入账号' }]}>
              <Input autoComplete="username" />
            </Form.Item>
            <Form.Item label="密码" name="password" rules={[{ required: true, message: '请输入密码' }]}>
              <Input.Password autoComplete="current-password" />
            </Form.Item>
            <Button type="primary" htmlType="submit" block loading={busy}>
              下一步
            </Button>
          </Form>
        ) : (
          <Form
            layout="vertical"
            onFinish={(v: { code: string }) =>
              run(async () => {
                const r = await unwrap(api.POST('/admin/auth/verify', { body: { challenge_id: challenge.id, code: v.code } }));
                setSession(r.token);
              })
            }
          >
            <Typography.Paragraph>验证码已发送到 {challenge.phone}</Typography.Paragraph>
            <Form.Item label="短信验证码" name="code" rules={[{ required: true, len: 6, message: '请输入 6 位验证码' }]}>
              <Input inputMode="numeric" maxLength={6} autoComplete="one-time-code" />
            </Form.Item>
            <Space direction="vertical" style={{ width: '100%' }}>
              <Button type="primary" htmlType="submit" block loading={busy}>
                登录
              </Button>
              <Button type="link" onClick={() => setChallenge(undefined)}>
                返回
              </Button>
            </Space>
          </Form>
        )}
      </Card>
    </div>
  );
}
