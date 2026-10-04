// 7.9 消息与公告：系统自动消息（解析和批改完成、复习到期）可开关和改时间；手动公告：标题、内容、发送对象、
// 渠道（消息中心）、定时发送、预览。短信只用于验证码。
import { ApiError } from '@training/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { App as AntApp, Button, Card, Checkbox, DatePicker, Form, Input, Popconfirm, Radio, Space, Table, Tag, Typography } from 'antd';
import type dayjs from 'dayjs';
import { useState } from 'react';
import { Loadable, PageTitle } from '../components/Page';
import { ParamsPanel } from './Config';
import { api, dt, unwrap } from '../lib/api';

function errMsg(e: unknown) {
  return e instanceof ApiError ? e.message : '操作失败，请重试';
}

export function Notice() {
  const qc = useQueryClient();
  const { message } = AntApp.useApp();
  const list = useQuery({ queryKey: ['admin', 'announcements'], queryFn: () => unwrap(api.GET('/admin/announcements')) });
  const [preview, setPreview] = useState<{ title: string; body: string }>();
  const [form] = Form.useForm();
  const create = useMutation({
    mutationFn: (v: { title: string; body: string; target: 'all' | 'users'; user_ids?: string; with_popup?: boolean; scheduled?: dayjs.Dayjs }) =>
      unwrap(
        api.POST('/admin/announcements', {
          body: {
            title: v.title,
            body: v.body,
            all: v.target === 'all',
            user_ids: v.target === 'users' ? (v.user_ids ?? '').split(/[,，\s]+/).map(Number).filter((x) => Number.isInteger(x) && x > 0) : undefined,
            with_popup: v.with_popup,
            scheduled_at: v.scheduled?.toISOString(),
          },
        }),
      ),
    onSuccess: () => {
      message.success('公告已创建，到点后 5 分钟内发出');
      form.resetFields();
      void qc.invalidateQueries({ queryKey: ['admin', 'announcements'] });
    },
    onError: (e) => message.error(errMsg(e)),
  });
  const cancel = useMutation({
    mutationFn: (id: number) => unwrap(api.DELETE('/admin/announcements/{announcementId}', { params: { path: { announcementId: id } } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['admin', 'announcements'] }),
    onError: (e) => message.error(errMsg(e)),
  });
  return (
    <>
      <PageTitle code="7.9" title="消息与公告" />
      <Card title="系统自动消息" size="small" style={{ marginBottom: 16 }}>
        <Typography.Paragraph type="secondary">review_due_hour 是复习到期提醒的北京时间整点（0–23）；task_done 控制解析完成、整卷与作文批改完成、导出完成的消息。</Typography.Paragraph>
        <ParamsPanel keys={['system_messages']} />
      </Card>
      <Card title="手动公告" size="small" style={{ marginBottom: 16 }}>
        <Form form={form} layout="vertical" initialValues={{ target: 'all' }} onFinish={create.mutate}>
          <Form.Item label="标题" name="title" rules={[{ required: true, min: 2, message: '请输入标题' }]}><Input maxLength={128} /></Form.Item>
          <Form.Item label="内容" name="body" rules={[{ required: true, min: 2, message: '请输入内容' }]}><Input.TextArea rows={4} maxLength={2000} /></Form.Item>
          <Form.Item label="发送对象" name="target">
            <Radio.Group options={[{ label: '全部用户', value: 'all' }, { label: '指定用户', value: 'users' }]} />
          </Form.Item>
          <Form.Item noStyle shouldUpdate>
            {({ getFieldValue }) =>
              getFieldValue('target') === 'users' ? (
                <Form.Item label="用户 ID（逗号分隔）" name="user_ids" rules={[{ required: true, message: '请填写用户 ID' }]}><Input /></Form.Item>
              ) : null
            }
          </Form.Item>
          <Space>
            <Form.Item label="定时发送（不填立即发）" name="scheduled"><DatePicker showTime /></Form.Item>
            <Form.Item label=" " name="with_popup" valuePropName="checked"><Checkbox>协议更新时同时弹 0.4b</Checkbox></Form.Item>
          </Space>
          <Typography.Paragraph type="secondary">渠道：消息中心。短信只用于验证码，不发营销短信。</Typography.Paragraph>
          <Space>
            <Button onClick={() => setPreview({ title: form.getFieldValue('title') ?? '', body: form.getFieldValue('body') ?? '' })}>预览</Button>
            <Button type="primary" htmlType="submit" loading={create.isPending}>创建公告</Button>
          </Space>
        </Form>
        {preview ? (
          <Card size="small" style={{ marginTop: 12, maxWidth: 390 }} title="消息中心预览">
            <Typography.Text strong>{preview.title}</Typography.Text>
            <Typography.Paragraph type="secondary" style={{ margin: 0 }}>{preview.body}</Typography.Paragraph>
          </Card>
        ) : null}
      </Card>
      <Card title="公告记录" size="small">
        <Loadable loading={list.isPending} error={list.error}>
          <Table
            rowKey="id"
            dataSource={list.data?.items ?? []}
            columns={[
              { title: '标题', dataIndex: 'title' },
              { title: '对象', render: (_, a) => (a.all ? '全部用户' : `${a.user_ids.length} 个用户`) },
              { title: '定时', render: (_, a) => dt(a.scheduled_at) },
              { title: '状态', render: (_, a) => (a.sent_at ? <Tag color="green">已发 {a.sent_count} 人 · {dt(a.sent_at)}</Tag> : <Tag>待发送</Tag>) },
              {
                title: '操作',
                render: (_, a) =>
                  a.sent_at ? null : (
                    <Popconfirm title="取消这条公告？" onConfirm={() => cancel.mutate(a.id)}>
                      <Button size="small" danger>取消</Button>
                    </Popconfirm>
                  ),
              },
            ]}
          />
        </Loadable>
      </Card>
    </>
  );
}
