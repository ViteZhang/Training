// 官方题库项目只关联共享教材的明确出版版本，原文在独立教材库维护。
import { ApiError } from '@training/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, App as AntApp, Button, Card, Input, List, Modal, Select, Space, Tag, Typography } from 'antd';
import { useRef, useState } from 'react';
import { Link } from 'react-router';
import { Loadable } from '../components/Page';
import { api, unwrap } from '../lib/api';

export function ProjectTextbooks({ projectId }: { projectId: number }) {
  const qc = useQueryClient();
  const { message } = AntApp.useApp();
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState('');
  const [selected, setSelected] = useState<number[]>([]);
  const [baseVersion, setBaseVersion] = useState(1);
  const requestKey = useRef<string | undefined>(undefined);
  const key = ['admin', 'project-textbooks', projectId];
  const list = useQuery({ queryKey: key, queryFn: () => unwrap(api.GET('/admin/official/projects/{projectId}/textbooks', { params: { path: { projectId } } })) });
  const catalog = useQuery({ queryKey: ['admin', 'textbooks', 'associate', search], queryFn: () => unwrap(api.GET('/admin/textbooks', { params: { query: { q: search, page: 1, page_size: 50 } } })), enabled: open });
  const save = useMutation({
    mutationFn: () => {
      requestKey.current ??= crypto.randomUUID();
      return unwrap(api.PUT('/admin/official/projects/{projectId}/textbooks', { params: { path: { projectId } }, body: { edition_ids: selected, expected_version: baseVersion, idempotency_key: requestKey.current } }));
    },
    onSuccess: () => { requestKey.current = undefined; setOpen(false); void qc.invalidateQueries({ queryKey: key }); message.success('已保存教材关联'); },
    onError: (e) => { message.error(e instanceof ApiError ? e.message : '关联失败，请重试'); if (e instanceof ApiError && e.status === 409) void qc.invalidateQueries({ queryKey: key }); },
  });
  const choices = new Map((list.data?.items ?? []).map((e) => [e.edition_id, { value: e.edition_id, label: `${e.title} · ${e.author} · ${e.edition}` }]));
  for (const book of catalog.data?.items ?? []) for (const e of book.editions) if (e.status === 'published') choices.set(e.id, { value: e.id, label: `${book.title} · ${book.author} · ${e.edition}` });
  return (
    <Card title="关联教材" size="small" style={{ marginTop: 24 }} extra={<Button onClick={() => { setBaseVersion(list.data?.version ?? 1); setSelected(list.data?.items.map((e) => e.edition_id) ?? []); requestKey.current = undefined; setOpen(true); }}>关联已有教材</Button>}>
      <Loadable loading={list.isPending} error={list.error}>
        <List dataSource={list.data?.items ?? []} locale={{ emptyText: '还没有关联教材' }} renderItem={(e) => <List.Item><List.Item.Meta title={e.title} description={<Space><Typography.Text type="secondary">{e.author} · {e.edition}</Typography.Text><Tag color={e.available ? 'green' : 'default'}>{e.available ? '已发布' : '暂不可用于新答案'}</Tag></Space>} /></List.Item>} />
      </Loadable>
      <Typography.Paragraph type="secondary">关联不复制原文；解除关联不会删除公共教材。<Link to="/textbooks">前往官方教材库维护</Link></Typography.Paragraph>
      <Modal open={open} title="关联已有教材" okText="保存关联" onCancel={() => setOpen(false)} onOk={() => save.mutate()} confirmLoading={save.isPending}>
        <Space orientation="vertical" style={{ width: '100%' }}>
          <Alert type="info" message="仅选择已发布的明确出版版本，删除选项即可解除关联。" />
          <Input.Search placeholder="搜索教材名称、作者" onSearch={setSearch} />
          <Loadable loading={catalog.isPending} error={catalog.error}>
            <Select aria-label="选择关联教材版本" mode="multiple" style={{ width: '100%' }} value={selected} options={[...choices.values()]} onChange={(ids) => { requestKey.current = undefined; setSelected(ids); }} optionFilterProp="label" />
          </Loadable>
        </Space>
      </Modal>
    </Card>
  );
}
