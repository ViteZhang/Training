// 7.13 审核队列：编辑提交的草稿（新编辑前 3 次全量审核，之后 30% 抽检）。修订时左右对比修改前后；
// 操作：通过、退回（必须填原因）、直接修改后通过。不能审核自己提交的。
import { ApiError } from '@training/api-client';
import type { components } from '@training/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { App as AntApp, Button, Col, Descriptions, Drawer, Input, Modal, Row, Space, Table, Tag, Typography } from 'antd';
import { useState } from 'react';
import { Loadable, PageTitle } from '../components/Page';
import { ProjectPicker, useProjects } from '../components/ProjectPicker';
import { api, dt, unwrap } from '../lib/api';
import { changeNames, qtypeNames } from './Produce';

type Review = components['schemas']['OfficialReview'];
const modeNames: Record<string, string> = { full: '全量审核', sampled: '抽检' };

function errMsg(e: unknown) {
  return e instanceof ApiError ? e.message : '操作失败，请重试';
}

export function ReviewQueue() {
  const [project, setProject] = useState<number>();
  const [open, setOpen] = useState<Review>();
  const projects = useProjects();
  const name = (id: number) => {
    const p = projects.data?.items.find((x) => x.id === id);
    return p ? `${p.school} ${p.subject_code}` : String(id);
  };
  const q = useQuery({
    queryKey: ['admin', 'official', 'reviews', project ?? 0],
    queryFn: () => unwrap(api.GET('/admin/official/reviews', { params: { query: { project_id: project } } })),
    // 切换项目时保留上一份列表，不闪成加载中。
    placeholderData: (prev) => prev,
  });
  return (
    <>
      <PageTitle code="7.13" title="审核队列" extra={<ProjectPicker value={project} onChange={setProject} />} />
      <Loadable loading={q.isPending} error={q.error}>
        <Table
          rowKey="id"
          dataSource={q.data?.items ?? []}
          locale={{ emptyText: '没有待审核的内容' }}
          onRow={(r) => ({ onClick: () => setOpen(r), style: { cursor: 'pointer' } })}
          columns={[
            { title: '项目', render: (_, r) => name(r.project_id) },
            { title: '变更', render: (_, r) => `${changeNames[r.change_type]}${r.entity_type === 'knowledge_point' ? '知识点' : '题目'}` },
            { title: '内容', render: (_, r) => String(r.payload.name ?? r.payload.stem ?? r.before?.stem ?? '') },
            { title: '审核方式', render: (_, r) => <Tag color={r.mode === 'full' ? 'blue' : 'default'}>{modeNames[r.mode] ?? r.mode}</Tag> },
            { title: '编辑', dataIndex: 'editor_id' },
            { title: '提交时间', render: (_, r) => dt(r.created_at) },
          ]}
        />
      </Loadable>
      {open ? <ReviewDrawer review={open} onClose={() => setOpen(undefined)} /> : null}
    </>
  );
}

/** 把草稿内容排成可读的字段列表（审核时对比用）。 */
export function PayloadView({ payload }: { payload?: Record<string, unknown> }) {
  if (!payload) return <Typography.Text type="secondary">（新增，没有修改前的内容）</Typography.Text>;
  const rubric = (payload.rubric as { content: string; score: number }[] | undefined) ?? [];
  const options = (payload.options as { key: string; text: string }[] | undefined) ?? [];
  return (
    <Descriptions column={1} size="small" bordered>
      {payload.name ? <Descriptions.Item label="知识点">{`${String(payload.section ?? '')} / ${String(payload.chapter ?? '')} / ${String(payload.name)}`}</Descriptions.Item> : null}
      {payload.original_text ? <Descriptions.Item label="原文表述">{String(payload.original_text)}</Descriptions.Item> : null}
      {payload.qtype ? <Descriptions.Item label="题型">{qtypeNames[String(payload.qtype)]}</Descriptions.Item> : null}
      {payload.stem ? <Descriptions.Item label="题干">{String(payload.stem)}</Descriptions.Item> : null}
      {options.length ? <Descriptions.Item label="选项">{options.map((o) => `${o.key}. ${o.text}`).join('；')}</Descriptions.Item> : null}
      {payload.answer ? <Descriptions.Item label="参考答案">{String(payload.answer)}</Descriptions.Item> : null}
      {Array.isArray(payload.kp_names) && payload.kp_names.length ? <Descriptions.Item label="知识点">{(payload.kp_names as string[]).join('、')}</Descriptions.Item> : null}
      <Descriptions.Item label="采分点">
        {rubric.length ? rubric.map((r, i) => <div key={i}>{`${i + 1}. ${r.content}（${r.score} 分）`}</div>) : '—'}
      </Descriptions.Item>
    </Descriptions>
  );
}

function ReviewDrawer({ review, onClose }: { review: Review; onClose: () => void }) {
  const qc = useQueryClient();
  const { message } = AntApp.useApp();
  const [rejecting, setRejecting] = useState(false);
  const [reason, setReason] = useState('');
  const [editing, setEditing] = useState(false);
  const [edited, setEdited] = useState(JSON.stringify(review.payload, null, 2));
  const decide = useMutation({
    mutationFn: (v: { decision: 'approved' | 'rejected' | 'edited'; reason?: string; edited_payload?: Record<string, unknown> }) =>
      unwrap(api.POST('/admin/official/reviews/{reviewId}', { params: { path: { reviewId: review.id } }, body: v })),
    onSuccess: () => {
      message.success('已处理');
      void qc.invalidateQueries({ queryKey: ['admin', 'official'] });
      onClose();
    },
    onError: (e) => message.error(errMsg(e)),
  });
  const saveEdited = () => {
    try {
      decide.mutate({ decision: 'edited', edited_payload: JSON.parse(edited) as Record<string, unknown> });
    } catch {
      message.error('内容格式不对');
    }
  };
  return (
    <Drawer
      open
      width={960}
      title={`${changeNames[review.change_type]}${review.entity_type === 'knowledge_point' ? '知识点' : '题目'}`}
      onClose={onClose}
      extra={
        <Space>
          <Button danger onClick={() => setRejecting(true)}>退回</Button>
          <Button onClick={() => setEditing(true)}>修改后通过</Button>
          <Button type="primary" loading={decide.isPending} onClick={() => decide.mutate({ decision: 'approved' })}>
            通过
          </Button>
        </Space>
      }
    >
      {review.change_type === 'offline' ? (
        <>
          <Typography.Title level={5}>要下线的题目</Typography.Title>
          <PayloadView payload={review.before} />
        </>
      ) : (
        <Row gutter={16}>
          <Col span={12}>
            <Typography.Title level={5}>修改前</Typography.Title>
            <PayloadView payload={review.before} />
          </Col>
          <Col span={12}>
            <Typography.Title level={5}>提交的内容</Typography.Title>
            <PayloadView payload={review.payload} />
          </Col>
        </Row>
      )}
      <Modal open={rejecting} title="退回" okText="退回" okButtonProps={{ danger: true, disabled: !reason.trim() }} onCancel={() => setRejecting(false)}
        onOk={() => decide.mutate({ decision: 'rejected', reason })}>
        <Input.TextArea rows={3} value={reason} onChange={(e) => setReason(e.target.value)} placeholder="退回原因（必填，编辑会看到）" maxLength={500} />
      </Modal>
      <Modal open={editing} width={720} title="修改后通过" okText="保存并通过" onCancel={() => setEditing(false)} onOk={saveEdited}>
        <Input.TextArea rows={18} value={edited} onChange={(e) => setEdited(e.target.value)} style={{ fontFamily: 'monospace' }} />
      </Modal>
    </Drawer>
  );
}
