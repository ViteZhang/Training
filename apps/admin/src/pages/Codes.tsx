// 7.4 兑换码：汇总（已生成、已使用、可用、已停用或过期）；批次列表（档位、数量、已用、有效期、渠道、状态），可看明细、导出、停用；
// 新建批次：名称、档位、数量、码有效期、渠道；单码查询，显示使用人与时间，可作废并收回会员。
// 码在库里只存哈希：完整兑换码只在生成时返回一次，生成后立即导出（D37）；之后的明细与导出只有末 3 位。
import type { Schemas } from '@training/api-client';
import { ApiError } from '@training/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, App as AntApp, Button, Card, Col, DatePicker, Descriptions, Form, Input, InputNumber, Modal, Popconfirm, Row, Select, Space, Statistic, Table, Tag } from 'antd';
import { useState } from 'react';
import { Loadable, PageTitle } from '../components/Page';
import { api, downloadCSV, dt, unwrap } from '../lib/api';
import type { Role } from '../lib/menu';

const tierNames: Record<string, string> = { sprint: '冲刺卡', season: '考季卡', monthly: '月卡', gift: '赠送天数' };
const codeStatus: Record<string, string> = { unused: '未使用', used: '已使用', void: '已作废' };

function tierLabel(b: { tier: string; days?: number }) {
  return b.tier === 'gift' ? `${b.days} 天` : tierNames[b.tier];
}

function errMsg(e: unknown) {
  return e instanceof ApiError ? e.message : '操作失败，请重试';
}

export function Codes({ roles }: { roles: Role[] }) {
  const isAdmin = roles.includes('admin');
  const qc = useQueryClient();
  const { message } = AntApp.useApp();
  const summary = useQuery({ queryKey: ['admin', 'redeem', 'summary'], queryFn: () => unwrap(api.GET('/admin/redeem/summary')) });
  const batches = useQuery({ queryKey: ['admin', 'redeem', 'batches'], queryFn: () => unwrap(api.GET('/admin/redeem/batches')) });
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<Schemas['AdminBatchCreated']>();
  const [detail, setDetail] = useState<number>();
  const refresh = () => void qc.invalidateQueries({ queryKey: ['admin', 'redeem'] });

  const exportBatch = async (id: number) => {
    const r = await unwrap(api.GET('/admin/redeem/batches/{batchId}', { params: { path: { batchId: id } } }));
    downloadCSV(`兑换码明细-${r.batch.name}.csv`, [
      ['码末 3 位', '状态', '使用人 ID', '使用时间', '档位', '有效期至'],
      ...r.codes.map((c) => [c.tail, codeStatus[c.status], c.used_by ?? '', dt(c.used_at), tierLabel(r.batch), dt(r.batch.expires_at)]),
    ]);
  };
  const disable = useMutation({
    mutationFn: (id: number) => unwrap(api.POST('/admin/redeem/batches/{batchId}/disable', { params: { path: { batchId: id } } })),
    onSuccess: () => {
      message.success('已停用');
      refresh();
    },
    onError: (e) => message.error(errMsg(e)),
  });
  const s = summary.data;
  return (
    <>
      <PageTitle code="7.4" title="兑换码" extra={isAdmin ? <Button type="primary" onClick={() => setCreating(true)}>新建批次</Button> : null} />
      {s ? (
        <Row gutter={16}>
          <Col span={6}><Card><Statistic title="已生成" value={s.generated} /></Card></Col>
          <Col span={6}><Card><Statistic title="已使用" value={s.used} /></Card></Col>
          <Col span={6}><Card><Statistic title="可用" value={s.available} /></Card></Col>
          <Col span={6}><Card><Statistic title="已停用或过期" value={s.inactive} /></Card></Col>
        </Row>
      ) : null}
      <Card title="批次" style={{ marginTop: 16 }}>
        <Loadable loading={batches.isPending} error={batches.error}>
          <Table
            rowKey="id"
            dataSource={batches.data?.items ?? []}
            columns={[
              { title: '批次', dataIndex: 'name' },
              { title: '档位', render: (_, b) => tierLabel(b) },
              { title: '数量', dataIndex: 'quantity' },
              { title: '已用', dataIndex: 'used' },
              { title: '有效期至', render: (_, b) => dt(b.expires_at).slice(0, 10) },
              { title: '渠道', dataIndex: 'channel' },
              { title: '状态', render: (_, b) => (b.status === 'active' ? <Tag color="green">启用</Tag> : <Tag>已停用</Tag>) },
              {
                title: '操作',
                render: (_, b) => (
                  <Space>
                    <Button size="small" onClick={() => setDetail(b.id)}>明细</Button>
                    <Button size="small" onClick={() => void exportBatch(b.id)}>导出</Button>
                    {isAdmin && b.status === 'active' ? (
                      <Popconfirm title="停用后未使用的码不能再兑换，已兑换的会员不受影响" onConfirm={() => disable.mutate(b.id)}>
                        <Button size="small" danger>停用</Button>
                      </Popconfirm>
                    ) : null}
                  </Space>
                ),
              },
            ]}
          />
        </Loadable>
      </Card>
      <CodeLookup canVoid={isAdmin} onChanged={refresh} />

      <Modal open={creating} title="新建批次" footer={null} onCancel={() => setCreating(false)} destroyOnHidden>
        <CreateBatch
          onCreated={(r) => {
            setCreating(false);
            setCreated(r);
            refresh();
          }}
        />
      </Modal>
      <Modal open={!!created} title="兑换码已生成" onCancel={() => setCreated(undefined)} footer={<Button onClick={() => setCreated(undefined)}>完成</Button>}>
        {created ? (
          <>
            <Alert type="warning" showIcon message="完整兑换码只显示这一次，请立即导出保存；系统只保存哈希，之后无法再查看完整码。" style={{ marginBottom: 12 }} />
            <Button
              type="primary"
              onClick={() =>
                downloadCSV(`兑换码-${created.batch.name}.csv`, [
                  ['兑换码', '档位', '有效期至', '渠道'],
                  ...created.codes.map((c) => [c, tierLabel(created.batch), dt(created.batch.expires_at).slice(0, 10), created.batch.channel]),
                ])
              }
            >
              导出 {created.codes.length} 个兑换码（CSV）
            </Button>
          </>
        ) : null}
      </Modal>
      {detail ? <BatchDetail id={detail} onClose={() => setDetail(undefined)} /> : null}
    </>
  );
}

function CreateBatch({ onCreated }: { onCreated: (r: Schemas['AdminBatchCreated']) => void }) {
  const { message } = AntApp.useApp();
  const [tier, setTier] = useState('gift');
  const create = useMutation({
    mutationFn: (body: { name: string; tier: 'sprint' | 'season' | 'monthly' | 'gift'; days?: number; quantity: number; expires_at: string; channel?: string }) =>
      unwrap(api.POST('/admin/redeem/batches', { body })),
    onSuccess: onCreated,
    onError: (e) => message.error(errMsg(e)),
  });
  return (
    <Form
      layout="vertical"
      initialValues={{ tier: 'gift', days: 7, quantity: 100 }}
      onFinish={(v: { name: string; tier: 'sprint' | 'season' | 'monthly' | 'gift'; days?: number; quantity: number; expires: { toDate: () => Date }; channel?: string }) => {
        const end = v.expires.toDate();
        end.setHours(23, 59, 59, 0);
        create.mutate({ name: v.name, tier: v.tier, days: v.tier === 'gift' ? v.days : undefined, quantity: v.quantity, expires_at: end.toISOString(), channel: v.channel });
      }}
    >
      <Form.Item label="批次名称" name="name" rules={[{ required: true, message: '请填写批次名称' }]}>
        <Input maxLength={64} placeholder="种子用户 · 第二批" />
      </Form.Item>
      <Form.Item label="档位" name="tier">
        <Select
          onChange={setTier}
          options={[
            { label: '赠送天数', value: 'gift' },
            { label: '冲刺卡', value: 'sprint' },
            { label: '考季卡', value: 'season' },
            { label: '月卡', value: 'monthly' },
          ]}
        />
      </Form.Item>
      {tier === 'gift' ? (
        <Form.Item label="天数" name="days" rules={[{ required: true, message: '请填写天数' }]}>
          <InputNumber min={1} max={400} style={{ width: '100%' }} />
        </Form.Item>
      ) : null}
      <Form.Item label="数量" name="quantity" rules={[{ required: true, message: '请填写数量' }]}>
        <InputNumber min={1} max={5000} style={{ width: '100%' }} />
      </Form.Item>
      <Form.Item label="码有效期至" name="expires" rules={[{ required: true, message: '请选择有效期' }]}>
        <DatePicker style={{ width: '100%' }} />
      </Form.Item>
      <Form.Item label="渠道" name="channel">
        <Input maxLength={32} placeholder="考研群 / 小红书 / 客服" />
      </Form.Item>
      <Form.Item extra="每码限用 1 次；8 位大写，去掉 0/O、1/I 等易混字符；渠道用于统计来源">
        <Button type="primary" htmlType="submit" loading={create.isPending}>生成兑换码</Button>
      </Form.Item>
    </Form>
  );
}

function BatchDetail({ id, onClose }: { id: number; onClose: () => void }) {
  const q = useQuery({ queryKey: ['admin', 'redeem', 'batch', id], queryFn: () => unwrap(api.GET('/admin/redeem/batches/{batchId}', { params: { path: { batchId: id } } })) });
  return (
    <Modal open width={720} title={q.data ? `明细 · ${q.data.batch.name}` : '明细'} footer={null} onCancel={onClose}>
      <Loadable loading={q.isPending} error={q.error}>
        <Table
          size="small"
          rowKey="id"
          dataSource={q.data?.codes ?? []}
          columns={[
            { title: '码（末 3 位）', render: (_, c) => `*****${c.tail}` },
            { title: '状态', render: (_, c) => codeStatus[c.status] },
            { title: '使用人 ID', render: (_, c) => c.used_by ?? '—' },
            { title: '使用时间', render: (_, c) => dt(c.used_at) },
          ]}
        />
      </Loadable>
    </Modal>
  );
}

function CodeLookup({ canVoid, onChanged }: { canVoid: boolean; onChanged: () => void }) {
  const { message } = AntApp.useApp();
  const [code, setCode] = useState('');
  const q = useQuery({
    queryKey: ['admin', 'redeem', 'code', code],
    queryFn: () => unwrap(api.GET('/admin/redeem/codes', { params: { query: { code } } })),
    enabled: code.length > 0,
    retry: false,
  });
  const voidCode = useMutation({
    mutationFn: (id: number) => unwrap(api.POST('/admin/redeem/codes/{codeId}/void', { params: { path: { codeId: id } } })),
    onSuccess: () => {
      message.success('已作废，会员已收回');
      void q.refetch();
      onChanged();
    },
    onError: (e) => message.error(errMsg(e)),
  });
  const c = q.data;
  return (
    <Card title="单码查询" style={{ marginTop: 16 }}>
      <Input.Search placeholder="输入兑换码" style={{ width: 320 }} onSearch={(v) => setCode(v.trim().toUpperCase())} enterButton="查询" />
      {code && q.isError ? <Alert type="info" message="没有找到这个兑换码" style={{ marginTop: 12 }} /> : null}
      {c ? (
        <Descriptions column={1} size="small" style={{ marginTop: 12 }}>
          <Descriptions.Item label="状态">{codeStatus[c.status]} · {c.batch_name}</Descriptions.Item>
          <Descriptions.Item label="档位">{tierLabel({ tier: c.tier, days: c.days })} · 码有效期至 {dt(c.expires_at).slice(0, 10)}</Descriptions.Item>
          <Descriptions.Item label="使用人">{c.used_by ? `用户 ${c.used_by} · ${dt(c.used_at)}` : '—'}</Descriptions.Item>
          {canVoid && c.status !== 'void' ? (
            <Descriptions.Item label="操作">
              <Popconfirm title="作废后已兑换的会员会被收回，确定吗？" onConfirm={() => voidCode.mutate(c.id)}>
                <Button danger size="small">作废并收回会员</Button>
              </Popconfirm>
            </Descriptions.Item>
          ) : null}
        </Descriptions>
      ) : null}
    </Card>
  );
}
