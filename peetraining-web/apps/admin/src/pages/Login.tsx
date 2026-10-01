// 后台登录：账号密码 + 短信两步验证，所有后台账号强制开启（PRD 10.1）。接口在 T28 实现，这里是页面壳。
import { Button, Card, Form, Input, Space, Typography } from 'antd';
import { useState } from 'react';

export function Login({ onLoggedIn }: { onLoggedIn: () => void }) {
  const [step, setStep] = useState<'password' | 'sms'>('password');
  return (
    <div style={{ minHeight: '100vh', display: 'grid', placeItems: 'center', background: '#FAF8F3' }}>
      <Card style={{ width: 380 }}>
        <Typography.Title level={4}>考研Training 运营后台</Typography.Title>
        <Typography.Paragraph type="secondary">仅限内部账号；登录需短信两步验证</Typography.Paragraph>
        {step === 'password' ? (
          <Form layout="vertical" onFinish={() => setStep('sms')}>
            <Form.Item label="账号" name="username" rules={[{ required: true, message: '请输入账号' }]}>
              <Input autoComplete="username" />
            </Form.Item>
            <Form.Item label="密码" name="password" rules={[{ required: true, message: '请输入密码' }]}>
              <Input.Password autoComplete="current-password" />
            </Form.Item>
            <Button type="primary" htmlType="submit" block>
              下一步
            </Button>
          </Form>
        ) : (
          <Form layout="vertical" onFinish={onLoggedIn}>
            <Form.Item label="短信验证码" name="code" rules={[{ required: true, len: 6, message: '请输入 6 位验证码' }]}>
              <Input inputMode="numeric" maxLength={6} autoComplete="one-time-code" />
            </Form.Item>
            <Space direction="vertical" style={{ width: '100%' }}>
              <Button type="primary" htmlType="submit" block>
                登录
              </Button>
              <Button type="link" onClick={() => setStep('password')}>
                返回
              </Button>
            </Space>
          </Form>
        )}
      </Card>
    </div>
  );
}
