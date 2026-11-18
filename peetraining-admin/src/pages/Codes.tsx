// 7.4 兑换码：汇总（已生成、已使用、可用、已停用或过期）；批次列表（档位、数量、已用、有效期、渠道、状态），可看明细、导出、停用；
// 新建批次：名称、档位、数量、码有效期、渠道；查找兑换码（按码或备注），显示使用人与时间，可作废并收回会员。
// 单个发码 · 手动收款（PRD v3 7.4、13.3）：客服在企业微信收款后填手机号、档位、金额、收款方式、备注，生成一个码复制发给用户，
// 这笔收入记入 7.3（渠道：企业微信客服），码归入「企业微信客服 · 单个发码」批次。
// 明文与发放备注（D54）：明细里直接显示、一键复制兑换码；每个码可写发放备注（发给了谁），按「未发放 / 已发放未激活 / 已激活 / 已作废」筛选，
// 避免重复发放；「复制一个未发放的码」复制后马上要求填备注。旧码库里没有明文，可粘贴当初导出的码补录（服务端核对哈希）。
import type { Schemas } from '@/api-client';
import { ApiError } from '@/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, App as AntApp, Button, Card, Col, DatePicker, Descriptions, Form, Input, InputNumber, Modal, Popconfirm, Row, Segmented, Select, Space, Statistic, Table, Tag, Typography } from 'antd';
import { useState } from 'react';
import { Loadable, PageTitle } from '../components/Page';
import { api, downloadCSV, dt, unwrap } from '../lib/api';
import { bjWallToISO } from '../lib/time';
import type { Role } from '../lib/menu';

const tierNames: Record<string, string> = { sprint: '冲刺卡', season: '考季卡', monthly: '月卡', gift: '赠送天数' };
type Code = Schemas['AdminCode'];
type IssueState = 'available' | 'issued' | 'used' | 'void';
const issueNames: Record<IssueState, string> = { available: '未发放', issued: '已发放未激活', used: '已激活', void: '已作废' };
const issueColors: Record<IssueState, string> = { available: 'default', issued: 'orange', used: 'green', void: 'red' };

/** 发放状态：作废、已兑换以服务端状态为准；未使用的码有备注即视为已发放。 */
export function issueState(c: Code): IssueState {
  if (c.status === 'void') return 'void';
  if (c.status === 'used') return 'used';
  return c.note ? 'issued' : 'available';
}

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
      ['兑换码', '发放状态', '发放备注', '备注人', '备注时间', '使用人 ID', '使用时间', '档位', '有效期至'],
      ...r.codes.map((c) => [
        c.code ?? `*****${c.tail}`, issueNames[issueState(c)], c.note ?? '', c.noted_by_name ?? '', dt(c.noted_at), c.used_by ?? '', dt(c.used_at), tierLabel(r.batch), dt(r.batch.expires_at),
      ]),
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
      <ManualIssue onIssued={refresh} />
      <CodeLookup canVoid={isAdmin} />

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
            <Alert type="info" showIcon message="兑换码已保存，之后可在批次「明细」里随时查看、复制，并给每个码写发放备注。" style={{ marginBottom: 12 }} />
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
      {detail ? <BatchDetail id={detail} canVoid={isAdmin} onClose={() => setDetail(undefined)} /> : null}
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
      onFinish={(v: { name: string; tier: 'sprint' | 'season' | 'monthly' | 'gift'; days?: number; quantity: number; expires: { format: (f: string) => string }; channel?: string }) => {
        // 有效期到所选日期的北京时间 23:59:59
        const end = bjWallToISO(`${v.expires.format('YYYY-MM-DD')}T23:59:59`);
        create.mutate({ name: v.name, tier: v.tier, days: v.tier === 'gift' ? v.days : undefined, quantity: v.quantity, expires_at: end, channel: v.channel });
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
      <Form.Item label="码有效期至（北京时间当天 23:59）" name="expires" rules={[{ required: true, message: '请选择有效期' }]}>
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

const payMethods = ['企业微信转账', '企业微信收款码', '微信转账', '支付宝转账', '其他'];

async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}

export function ManualIssue({ onIssued }: { onIssued: () => void }) {
  const { message } = AntApp.useApp();
  const [form] = Form.useForm();
  const [issued, setIssued] = useState<{ code: string; order_no: string; copied: boolean }>();
  const issue = useMutation({
    mutationFn: (body: { phone: string; tier: 'sprint' | 'season' | 'monthly'; amount_cents: number; pay_method: string; remark?: string }) =>
      unwrap(api.POST('/admin/redeem/single-codes', { body })),
    onSuccess: async (r) => {
      const copied = await copyText(r.code);
      setIssued({ code: r.code, order_no: r.order_no, copied });
      form.resetFields();
      onIssued();
    },
    onError: (e) => message.error(errMsg(e)),
  });
  return (
    <Card title="单个发码 · 手动收款" extra={<Tag>单个发码</Tag>} style={{ marginTop: 16 }}>
      <Form
        form={form}
        layout="inline"
        initialValues={{ tier: 'season', pay_method: payMethods[0] }}
        onFinish={(v: { phone: string; tier: 'sprint' | 'season' | 'monthly'; amount: number; pay_method: string; remark?: string }) =>
          issue.mutate({ phone: v.phone.replace(/\s/g, ''), tier: v.tier, amount_cents: Math.round(v.amount * 100), pay_method: v.pay_method, remark: v.remark || undefined })
        }
      >
        <Form.Item label="用户手机号" name="phone" rules={[{ required: true, pattern: /^1[3-9]\d{9}$/, transform: (v: string) => v?.replace(/\s/g, ''), message: '请输入正确的手机号' }]}>
          <Input maxLength={13} placeholder="186 2210 7731" style={{ width: 160 }} />
        </Form.Item>
        <Form.Item label="档位" name="tier">
          <Select style={{ width: 110 }} options={[{ label: '冲刺卡', value: 'sprint' }, { label: '考季卡', value: 'season' }, { label: '月卡', value: 'monthly' }]} />
        </Form.Item>
        <Form.Item label="收款金额" name="amount" rules={[{ required: true, message: '请填写收款金额' }]}>
          <InputNumber min={0.01} max={100000} precision={2} prefix="¥" style={{ width: 120 }} />
        </Form.Item>
        <Form.Item label="收款方式" name="pay_method">
          <Select style={{ width: 150 }} options={payMethods.map((m) => ({ label: m, value: m }))} />
        </Form.Item>
        <Form.Item label="备注" name="remark">
          <Input maxLength={200} placeholder="小红书私信来的" style={{ width: 180 }} />
        </Form.Item>
        <Form.Item>
          <Button type="primary" htmlType="submit" loading={issue.isPending}>生成并复制兑换码</Button>
        </Form.Item>
      </Form>
      <div style={{ marginTop: 8, color: '#6E6E6E', fontSize: 12 }}>生成后复制发给用户；这笔收入记入「会员与订单」，渠道记为企业微信客服。码不绑定手机号，手机号只用于对账。</div>
      <Modal open={!!issued} title="兑换码已生成" onCancel={() => setIssued(undefined)} footer={<Button onClick={() => setIssued(undefined)}>完成</Button>}>
        {issued ? (
          <>
            <Alert type={issued.copied ? 'success' : 'warning'} showIcon message={issued.copied ? '已复制到剪贴板，发给用户即可' : '没能自动复制，请手动复制'} style={{ marginBottom: 12 }} />
            <Descriptions column={1} size="small">
              <Descriptions.Item label="兑换码">
                <span style={{ fontFamily: 'DM Sans, monospace', fontSize: 20, letterSpacing: 2 }}>{issued.code}</span>
              </Descriptions.Item>
              <Descriptions.Item label="订单号">{issued.order_no}</Descriptions.Item>
            </Descriptions>
            <Alert type="info" message="之后也能在「查找兑换码」或批次明细里看到这个码；发放备注已自动记为「单个发码 · 手机号」。" style={{ marginTop: 12 }} />
          </>
        ) : null}
      </Modal>
    </Card>
  );
}

function BatchDetail({ id, canVoid, onClose }: { id: number; canVoid: boolean; onClose: () => void }) {
  const { message } = AntApp.useApp();
  const [filter, setFilter] = useState<IssueState | 'all'>('all');
  const [backfill, setBackfill] = useState(false);
  const [noting, setNoting] = useState<{ code: Code; justCopied?: boolean }>();
  const q = useQuery({ queryKey: ['admin', 'redeem', 'batch', id], queryFn: () => unwrap(api.GET('/admin/redeem/batches/{batchId}', { params: { path: { batchId: id } } })) });
  const codes = q.data?.codes ?? [];
  const counts = codes.reduce<Record<string, number>>((m, c) => ({ ...m, [issueState(c)]: (m[issueState(c)] ?? 0) + 1 }), {});
  const missingPlain = codes.filter((c) => !c.code).length;
  const shown = filter === 'all' ? codes : codes.filter((c) => issueState(c) === filter);
  const active = q.data?.batch.status === 'active';
  const takeOne = async () => {
    const next = codes.find((c) => issueState(c) === 'available' && c.code);
    if (!next?.code) {
      message.warning(missingPlain ? '没有可复制的未发放码：旧码需要先补录明文' : '这个批次没有未发放的码了');
      return;
    }
    const copied = await copyText(next.code);
    if (!copied) message.warning('没能自动复制，请在弹窗里手动复制');
    setNoting({ code: next, justCopied: copied });
  };
  return (
    <Modal open width={1040} title={q.data ? `明细 · ${q.data.batch.name}` : '明细'} footer={null} onCancel={onClose}>
      <Loadable loading={q.isPending} error={q.error}>
        <Space wrap style={{ marginBottom: 12, width: '100%', justifyContent: 'space-between' }}>
          <Segmented
            value={filter}
            onChange={(v) => setFilter(v as IssueState | 'all')}
            options={[
              { label: `全部 ${codes.length}`, value: 'all' },
              ...(['available', 'issued', 'used', 'void'] as IssueState[]).map((k) => ({ label: `${issueNames[k]} ${counts[k] ?? 0}`, value: k })),
            ]}
          />
          <Space>
            {active ? <Button type="primary" onClick={() => void takeOne()}>复制一个未发放的码</Button> : null}
            {missingPlain ? <Button onClick={() => setBackfill(true)}>补录明文（{missingPlain} 个缺）</Button> : null}
          </Space>
        </Space>
        {missingPlain ? (
          <Alert
            type="info"
            showIcon
            style={{ marginBottom: 12 }}
            message={`有 ${missingPlain} 个旧码库里没有明文，只显示末 3 位。把当初生成时导出的 CSV 里的码粘贴到「补录明文」，核对无误后就能直接看到、复制。`}
          />
        ) : null}
        <CodeTable codes={shown} canVoid={canVoid} onNote={(c, justCopied) => setNoting({ code: c, justCopied })} />
      </Loadable>
      {noting ? <NoteModal code={noting.code} justCopied={noting.justCopied} onClose={() => setNoting(undefined)} /> : null}
      {backfill ? <BackfillModal batchId={id} onClose={() => setBackfill(false)} /> : null}
    </Modal>
  );
}

/** 兑换码列表：明细与查找共用。点码旁的复制图标复制明文；未发放的码复制后提示填备注。 */
function CodeTable({ codes, canVoid, showBatch, onNote }: { codes: Code[]; canVoid: boolean; showBatch?: boolean; onNote: (c: Code, justCopied?: boolean) => void }) {
  const qc = useQueryClient();
  const { message } = AntApp.useApp();
  const voidCode = useMutation({
    mutationFn: (id: number) => unwrap(api.POST('/admin/redeem/codes/{codeId}/void', { params: { path: { codeId: id } } })),
    onSuccess: () => {
      message.success('已作废，会员已收回');
      void qc.invalidateQueries({ queryKey: ['admin', 'redeem'] });
    },
    onError: (e) => message.error(errMsg(e)),
  });
  return (
    <Table
      size="small"
      rowKey="id"
      dataSource={codes}
      pagination={{ pageSize: 50, showSizeChanger: false }}
      columns={[
        {
          title: '兑换码',
          width: 150,
          render: (_, c) =>
            c.code ? (
              <Typography.Text
                style={{ fontFamily: 'DM Sans, monospace', letterSpacing: 1 }}
                copyable={{ text: c.code, onCopy: () => (issueState(c) === 'available' ? onNote(c, true) : undefined) }}
              >
                {c.code}
              </Typography.Text>
            ) : (
              <Typography.Text type="secondary">*****{c.tail}</Typography.Text>
            ),
        },
        ...(showBatch ? [{ title: '批次', render: (_: unknown, c: Code) => `${c.batch_name} · ${tierLabel({ tier: c.tier, days: c.days })}` }] : []),
        { title: '发放状态', width: 110, render: (_, c) => <Tag color={issueColors[issueState(c)]}>{issueNames[issueState(c)]}</Tag> },
        {
          title: '发放备注',
          render: (_, c) =>
            c.status === 'void' && !c.note ? (
              '—'
            ) : (
              <Space size={4} direction="vertical">
                <Button type="link" size="small" style={{ padding: 0, height: 'auto', whiteSpace: 'normal', textAlign: 'left' }} onClick={() => onNote(c)}>
                  {c.note || '添加备注'}
                </Button>
                {c.note ? <Typography.Text type="secondary" style={{ fontSize: 12 }}>{c.noted_by_name} · {dt(c.noted_at)}</Typography.Text> : null}
              </Space>
            ),
        },
        { title: '使用人', width: 90, render: (_, c) => (c.used_by ? `用户 ${c.used_by}` : '—') },
        { title: '使用时间', width: 140, render: (_, c) => dt(c.used_at) },
        ...(canVoid
          ? [
              {
                title: '操作',
                width: 80,
                render: (_: unknown, c: Code) =>
                  c.status === 'void' ? null : (
                    <Popconfirm title={c.status === 'used' ? '作废后已兑换的会员会被收回，确定吗？' : '作废后这个码不能再兑换，确定吗？'} onConfirm={() => voidCode.mutate(c.id)}>
                      <Button danger size="small">作废</Button>
                    </Popconfirm>
                  ),
              },
            ]
          : []),
      ]}
    />
  );
}

/** 写发放备注：发给了谁；清空即回到未发放。 */
function NoteModal({ code, justCopied, onClose }: { code: Code; justCopied?: boolean; onClose: () => void }) {
  const qc = useQueryClient();
  const { message } = AntApp.useApp();
  const save = useMutation({
    mutationFn: (note: string) => unwrap(api.PUT('/admin/redeem/codes/{codeId}/note', { params: { path: { codeId: code.id } }, body: { note } })),
    onSuccess: () => {
      message.success('备注已保存');
      void qc.invalidateQueries({ queryKey: ['admin', 'redeem'] });
      onClose();
    },
    onError: (e) => message.error(errMsg(e)),
  });
  return (
    <Modal open title="发放备注" footer={null} onCancel={onClose} destroyOnHidden>
      {justCopied ? <Alert type="success" showIcon style={{ marginBottom: 12 }} message="已复制兑换码。发给谁了？记一下，避免重复发放。" /> : null}
      <Descriptions column={1} size="small" style={{ marginBottom: 12 }}>
        <Descriptions.Item label="兑换码">
          {code.code ? <Typography.Text copyable style={{ fontFamily: 'DM Sans, monospace', letterSpacing: 1 }}>{code.code}</Typography.Text> : `*****${code.tail}`}
        </Descriptions.Item>
        <Descriptions.Item label="状态">{issueNames[issueState(code)]}</Descriptions.Item>
      </Descriptions>
      <Form layout="vertical" initialValues={{ note: code.note ?? '' }} onFinish={(v: { note: string }) => save.mutate(v.note.trim())}>
        <Form.Item label="发给了谁" name="note" extra="如「小红书 @阿青」「考研群 3 群 王同学」；清空后回到未发放">
          <Input maxLength={100} showCount autoFocus placeholder="渠道 + 对方昵称或手机号后 4 位" />
        </Form.Item>
        <Button type="primary" htmlType="submit" loading={save.isPending}>保存</Button>
      </Form>
    </Modal>
  );
}

/** 旧码补录明文：粘贴当初导出的码（每行一个，或整段 CSV），服务端核对哈希，只写入属于本批次的码。 */
function BackfillModal({ batchId, onClose }: { batchId: number; onClose: () => void }) {
  const qc = useQueryClient();
  const { message } = AntApp.useApp();
  const [text, setText] = useState('');
  const codes = Array.from(new Set(text.toUpperCase().match(/\b[23456789A-HJ-NP-Z]{8}\b/g) ?? []));
  const fill = useMutation({
    mutationFn: () => unwrap(api.POST('/admin/redeem/batches/{batchId}/plain-codes', { params: { path: { batchId } }, body: { codes } })),
    onSuccess: (r) => {
      message.success(`补上 ${r.filled} 个${r.already ? `，${r.already} 个之前已有` : ''}${r.unmatched ? `，${r.unmatched} 个不属于这个批次已忽略` : ''}`);
      void qc.invalidateQueries({ queryKey: ['admin', 'redeem'] });
      onClose();
    },
    onError: (e) => message.error(errMsg(e)),
  });
  return (
    <Modal open title="补录明文" footer={null} onCancel={onClose} destroyOnHidden>
      <Typography.Paragraph type="secondary">打开当初生成时导出的 CSV，把兑换码那一列（或整个文件内容）粘贴进来。只有属于这个批次的码会被补上，其他内容会被忽略。</Typography.Paragraph>
      <Input.TextArea rows={8} value={text} onChange={(e) => setText(e.target.value)} placeholder={'ABCD2345\nWXYZ6789\n…'} />
      <Space style={{ marginTop: 12 }}>
        <Button type="primary" disabled={codes.length === 0 || codes.length > 5000} loading={fill.isPending} onClick={() => fill.mutate()}>
          补录 {codes.length} 个码
        </Button>
        {codes.length > 5000 ? <Typography.Text type="danger">一次最多 5000 个</Typography.Text> : null}
      </Space>
    </Modal>
  );
}

function CodeLookup({ canVoid }: { canVoid: boolean }) {
  const [kw, setKw] = useState('');
  const [noting, setNoting] = useState<{ code: Code; justCopied?: boolean }>();
  const q = useQuery({
    queryKey: ['admin', 'redeem', 'search', kw],
    queryFn: () => unwrap(api.GET('/admin/redeem/codes/search', { params: { query: { q: kw } } })),
    enabled: kw.length > 0,
  });
  return (
    <Card title="查找兑换码" style={{ marginTop: 16 }}>
      <Input.Search placeholder="输入兑换码，或备注里的名字（如「阿青」）" allowClear style={{ width: 380 }} onSearch={(v) => setKw(v.trim())} enterButton="查找" maxLength={100} />
      {kw ? (
        <div style={{ marginTop: 12 }}>
          <Loadable loading={q.isPending} error={q.error}>
            {q.data?.items.length ? (
              <CodeTable codes={q.data.items} canVoid={canVoid} showBatch onNote={(c, justCopied) => setNoting({ code: c, justCopied })} />
            ) : (
              <Alert type="info" message="没有找到匹配的兑换码" />
            )}
          </Loadable>
        </div>
      ) : null}
      {noting ? <NoteModal code={noting.code} justCopied={noting.justCopied} onClose={() => setNoting(undefined)} /> : null}
    </Card>
  );
}
