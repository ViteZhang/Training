// 7.7 用户反馈：待回复数、平均首次回复时长、最多的类型、回复满意度；列表显示类型、内容、用户、时间、是否授权、状态；
// 详情可查看授权资料（显示授权截止时间）和关联解析任务；操作：重新识别这份资料、回复到消息中心。
import { ApiError } from '@training/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, App as AntApp, Button, Card, Col, Descriptions, Drawer, Form, Image, Input, Modal, Row, Segmented, Space, Statistic, Table, Tag, Typography } from 'antd';
import { useState } from 'react';
import { Loadable, PageTitle, PrivacyNote } from '../components/Page';
import { api, dt, dur, unwrap } from '../lib/api';

const typeNames: Record<string, string> = { suggestion: '功能建议', recognition: '识别不准', grading: '批改不准', bug: '出现问题', infringement: '侵权投诉' };
const statusNames: Record<string, string> = { open: '待回复', replied: '已回复', closed: '已关闭' };
type Status = '' | 'open' | 'replied';

function errMsg(e: unknown) {
  return e instanceof ApiError ? e.message : '操作失败，请重试';
}

export function Feedback() {
  const [status, setStatus] = useState<Status>('open');
  const [open, setOpen] = useState<number>();
  const stats = useQuery({ queryKey: ['admin', 'feedbacks', 'stats'], queryFn: () => unwrap(api.GET('/admin/feedbacks/stats')) });
  const list = useQuery({
    queryKey: ['admin', 'feedbacks', status],
    queryFn: () => unwrap(api.GET('/admin/feedbacks', { params: { query: { status: status || undefined } } })),
  });
  const s = stats.data;
  return (
    <>
      <PageTitle code="7.7" title="用户反馈" />
      {s ? (
        <Row gutter={16}>
          <Col span={6}><Card><Statistic title="待回复" value={s.open} /></Card></Col>
          <Col span={6}><Card><Statistic title="平均首次回复" value={dur(s.avg_reply_seconds)} /></Card></Col>
          <Col span={6}><Card><Statistic title="最多的类型" value={typeNames[s.top_type] ?? '—'} /></Card></Col>
          <Col span={6}><Card><Statistic title="回复满意度" value={s.satisfaction ? s.satisfaction.toFixed(1) : '—'} /></Card></Col>
        </Row>
      ) : null}
      <Segmented
        style={{ margin: '16px 0' }}
        value={status}
        onChange={(v) => setStatus(v as Status)}
        options={[
          { label: '待回复', value: 'open' },
          { label: '已回复', value: 'replied' },
          { label: '全部', value: '' },
        ]}
      />
      <Loadable loading={list.isPending} error={list.error}>
        <Table
          rowKey="id"
          dataSource={list.data?.items ?? []}
          onRow={(f) => ({ onClick: () => setOpen(f.id), style: { cursor: 'pointer' } })}
          columns={[
            { title: '类型', render: (_, f) => typeNames[f.type] },
            { title: '内容', dataIndex: 'content', ellipsis: true },
            { title: '用户 ID', dataIndex: 'user_id' },
            { title: '时间', render: (_, f) => dt(f.created_at) },
            { title: '授权', render: (_, f) => (f.allow_access ? <Tag color="blue">已授权</Tag> : '—') },
            { title: '状态', render: (_, f) => statusNames[f.status] },
          ]}
        />
      </Loadable>
      {open ? <FeedbackDrawer id={open} onClose={() => setOpen(undefined)} /> : null}
      <PrivacyNote />
    </>
  );
}

function FeedbackDrawer({ id, onClose }: { id: number; onClose: () => void }) {
  const qc = useQueryClient();
  const { message } = AntApp.useApp();
  const path = { params: { path: { feedbackId: id } } };
  const q = useQuery({ queryKey: ['admin', 'feedback', id], queryFn: () => unwrap(api.GET('/admin/feedbacks/{feedbackId}', path)) });
  const [material, setMaterial] = useState(false);
  const reply = useMutation({
    mutationFn: (text: string) => unwrap(api.POST('/admin/feedbacks/{feedbackId}/reply', { ...path, body: { reply: text } })),
    onSuccess: () => {
      message.success('已回复到用户的消息中心');
      void qc.invalidateQueries({ queryKey: ['admin', 'feedback'] });
      void qc.invalidateQueries({ queryKey: ['admin', 'feedbacks'] });
    },
    onError: (e) => message.error(errMsg(e)),
  });
  const reparse = useMutation({
    mutationFn: () => unwrap(api.POST('/admin/feedbacks/{feedbackId}/reparse', path)),
    onSuccess: () => message.success('已重新识别这份资料'),
    onError: (e) => message.error(errMsg(e)),
  });
  const f = q.data;
  return (
    <Drawer open width={640} title="反馈详情" onClose={onClose}>
      <Loadable loading={q.isPending} error={q.error}>
        {f ? (
          <>
            <Descriptions column={1} bordered size="small">
              <Descriptions.Item label="类型">{typeNames[f.type]} · {statusNames[f.status]}</Descriptions.Item>
              <Descriptions.Item label="用户">{f.user_id} · {dt(f.created_at)}</Descriptions.Item>
              <Descriptions.Item label="内容">{f.content}</Descriptions.Item>
              <Descriptions.Item label="截图">
                {f.screenshots.length ? <Image.PreviewGroup>{f.screenshots.map((u) => <Image key={u} src={u} width={96} />)}</Image.PreviewGroup> : '—'}
              </Descriptions.Item>
              <Descriptions.Item label="授权查看">{f.grant_expires_at ? `授权至 ${dt(f.grant_expires_at)}` : '未授权或已过期'}</Descriptions.Item>
              <Descriptions.Item label="关联解析任务">{f.related_import_id ?? '—'}</Descriptions.Item>
              {f.reply ? <Descriptions.Item label="已回复">{f.reply}</Descriptions.Item> : null}
            </Descriptions>
            <Space style={{ margin: '12px 0' }} wrap>
              {f.material_id && f.grant_expires_at ? <Button onClick={() => setMaterial(true)}>查看授权资料</Button> : null}
              {f.material_id ? <Button loading={reparse.isPending} onClick={() => reparse.mutate()}>重新识别这份资料</Button> : null}
            </Space>
            <Form layout="vertical" onFinish={(v: { reply: string }) => reply.mutate(v.reply)}>
              <Form.Item label="回复到用户消息中心" name="reply" rules={[{ required: true, min: 2, message: '请输入回复' }]}>
                <Input.TextArea rows={4} maxLength={1000} />
              </Form.Item>
              <Button type="primary" htmlType="submit" loading={reply.isPending}>发送回复</Button>
            </Form>
          </>
        ) : null}
      </Loadable>
      {material ? <GrantedMaterial id={id} onClose={() => setMaterial(false)} /> : null}
    </Drawer>
  );
}

function GrantedMaterial({ id, onClose }: { id: number; onClose: () => void }) {
  const q = useQuery({
    queryKey: ['admin', 'feedback-material', id],
    queryFn: () => unwrap(api.GET('/admin/feedbacks/{feedbackId}/material', { params: { path: { feedbackId: id } } })),
    retry: false,
    staleTime: Infinity,
  });
  const m = q.data;
  return (
    <Modal open width={760} title="授权查看 · 资料识别文字" footer={null} onCancel={onClose}>
      <Alert type="info" showIcon message="这次查看已记入日志，并已通知用户" style={{ marginBottom: 12 }} />
      <Loadable loading={q.isPending} error={q.error}>
        {m ? (
          <>
            <Typography.Paragraph type="secondary">
              {m.format} · {m.pages} 页 · {m.status} · 授权至 {dt(m.grant_expires_at)}
            </Typography.Paragraph>
            {m.page_texts.map((t, i) => (
              <Card key={i} size="small" title={`第 ${i + 1} 页`} style={{ marginBottom: 8 }}>
                <Typography.Paragraph style={{ whiteSpace: 'pre-wrap', margin: 0 }}>{t}</Typography.Paragraph>
              </Card>
            ))}
          </>
        ) : null}
      </Loadable>
    </Modal>
  );
}
