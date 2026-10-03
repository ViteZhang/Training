// 7.3 会员与订单：本月收入、付费用户、付费转化（以导入过资料的用户为分母）、待处理退款；订单列表与筛选；各档位销量；
// 退款处理显示用户使用情况（PRD 13.3：开通 7 天内、主要功能因我们的问题无法使用时全额退款），可先帮用户重新解析、拒绝或同意。
import type { Schemas } from '@training/api-client';
import { ApiError } from '@training/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { App as AntApp, Button, Card, Col, Descriptions, Form, Input, Modal, Row, Segmented, Space, Statistic, Table, Typography } from 'antd';
import { useState } from 'react';
import { Loadable, PageTitle } from '../components/Page';
import { api, dt, pct, unwrap, yuan } from '../lib/api';
import type { Role } from '../lib/menu';

const tierNames: Record<string, string> = { sprint: '冲刺卡', season: '考季卡', monthly: '月卡' };
const channelNames: Record<string, string> = { wechat: '微信支付', alipay: '支付宝', apple_iap: 'App Store' };
const statusNames: Record<string, string> = { created: '待支付', paid: '已支付', closed: '已关闭', refunding: '退款中', refunded: '已退款' };
type Status = '' | 'paid' | 'refunded' | 'closed';

export function Orders({ roles }: { roles: Role[] }) {
  const [status, setStatus] = useState<Status>('paid');
  const [refund, setRefund] = useState<Schemas['AdminOrder']>();
  const summary = useQuery({ queryKey: ['admin', 'orders', 'summary'], queryFn: () => unwrap(api.GET('/admin/orders/summary')) });
  const list = useQuery({
    queryKey: ['admin', 'orders', status],
    queryFn: () => unwrap(api.GET('/admin/orders', { params: { query: { status: status || undefined } } })),
  });
  const s = summary.data;
  return (
    <>
      <PageTitle code="7.3" title="会员与订单" />
      <Loadable loading={summary.isPending} error={summary.error}>
        {s ? (
          <Row gutter={16}>
            <Col span={6}><Card><Statistic title="本月收入" value={yuan(s.month_revenue_cents)} /></Card></Col>
            <Col span={6}><Card><Statistic title="付费用户" value={s.paid_users} /></Card></Col>
            <Col span={6}><Card><Statistic title="付费转化（导入过资料的用户）" value={pct(s.conversion)} /></Card></Col>
            <Col span={6}><Card><Statistic title="待处理退款" value={s.pending_refunds} /></Card></Col>
          </Row>
        ) : null}
        {s && s.tiers.length ? (
          <Typography.Paragraph style={{ marginTop: 12 }}>
            本月各档位销量：{s.tiers.map((t) => `${tierNames[t.tier]} ${t.orders} 单 · ${yuan(t.revenue_cents)}`).join('；')}
          </Typography.Paragraph>
        ) : null}
      </Loadable>
      <Segmented
        style={{ margin: '16px 0' }}
        value={status}
        onChange={(v) => setStatus(v as Status)}
        options={[
          { label: '已支付', value: 'paid' },
          { label: '退款', value: 'refunded' },
          { label: '已关闭', value: 'closed' },
          { label: '全部', value: '' },
        ]}
      />
      <Loadable loading={list.isPending} error={list.error}>
        <Table
          rowKey="order_no"
          dataSource={list.data?.items ?? []}
          columns={[
            { title: '订单号', dataIndex: 'order_no' },
            { title: '用户 ID', dataIndex: 'user_id' },
            { title: '档位', render: (_, o) => tierNames[o.tier] },
            { title: '渠道', render: (_, o) => channelNames[o.channel] },
            { title: '金额', render: (_, o) => yuan(o.amount_cents) },
            { title: '状态', render: (_, o) => statusNames[o.status] ?? o.status },
            { title: '支付时间', render: (_, o) => dt(o.paid_at) },
            {
              title: '操作',
              render: (_, o) =>
                o.status === 'paid' ? (
                  <Button size="small" onClick={() => setRefund(o)}>
                    退款处理
                  </Button>
                ) : null,
            },
          ]}
        />
      </Loadable>
      {refund ? <RefundModal order={refund} canRefund={roles.includes('admin')} onClose={() => setRefund(undefined)} /> : null}
    </>
  );
}

function RefundModal({ order, canRefund, onClose }: { order: Schemas['AdminOrder']; canRefund: boolean; onClose: () => void }) {
  const qc = useQueryClient();
  const { message } = AntApp.useApp();
  const usage = useQuery({
    queryKey: ['admin', 'order-usage', order.order_no],
    queryFn: () => unwrap(api.GET('/admin/orders/{orderNo}/usage', { params: { path: { orderNo: order.order_no } } })),
  });
  const reparse = useMutation({
    mutationFn: () => unwrap(api.POST('/admin/users/{userId}/reparse', { params: { path: { userId: order.user_id } } })),
    onSuccess: (r) => message.success(`已重新排队 ${r.count} 个文件`),
    onError: (e) => message.error(e instanceof ApiError ? e.message : '操作失败'),
  });
  const refund = useMutation({
    mutationFn: (reason: string) => unwrap(api.POST('/admin/orders/{orderNo}/refund', { params: { path: { orderNo: order.order_no } }, body: { reason } })),
    onSuccess: () => {
      message.success('已退款，本单会员已收回');
      void qc.invalidateQueries({ queryKey: ['admin', 'orders'] });
      onClose();
    },
    onError: (e) => message.error(e instanceof ApiError ? e.message : '退款失败'),
  });
  const u = usage.data;
  return (
    <Modal open title={`退款处理 · ${order.order_no}`} footer={null} onCancel={onClose}>
      <Loadable loading={usage.isPending} error={usage.error}>
        {u ? (
          <Descriptions column={1} size="small" bordered>
            <Descriptions.Item label="开通至今">{u.days_since_paid} 天{u.days_since_paid <= 7 ? '（7 天内）' : ''}</Descriptions.Item>
            <Descriptions.Item label="开通后使用">解析 {u.pages} 页 · 批改 {u.gradings} 次 · 作答 {u.answers} 题</Descriptions.Item>
            <Descriptions.Item label="解析失败的资料">{u.failed_materials} 份</Descriptions.Item>
          </Descriptions>
        ) : null}
      </Loadable>
      <Typography.Paragraph type="secondary" style={{ marginTop: 12 }}>
        规则：开通 7 天内、且主要功能因我们的问题无法使用时全额退款，退款后会员立即失效；其他情况由管理员判断。可先帮用户重新解析。
      </Typography.Paragraph>
      <Space direction="vertical" style={{ width: '100%' }}>
        <Button loading={reparse.isPending} onClick={() => reparse.mutate()}>先帮用户重新解析失败文件</Button>
        {canRefund ? (
          <Form layout="vertical" onFinish={(v: { reason: string }) => refund.mutate(v.reason)}>
            <Form.Item label="退款原因" name="reason" rules={[{ required: true, min: 2, message: '请填写退款原因' }]}>
              <Input maxLength={200} />
            </Form.Item>
            <Space>
              <Button type="primary" danger htmlType="submit" loading={refund.isPending}>同意退款</Button>
              <Button onClick={onClose}>拒绝</Button>
            </Space>
          </Form>
        ) : (
          <Typography.Text type="secondary">退款由管理员操作</Typography.Text>
        )}
      </Space>
    </Modal>
  );
}
