// 7.14 版本发布：自上个版本以来已审核的变更清单（标出采分点有变化的）、自动检查、用户通知预览；发布后用户没改过的内容跟着更新，
// 改过的不覆盖；版本历史可回滚最新版本。
import { ApiError } from '@training/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, App as AntApp, Button, Card, Empty, Input, List, Popconfirm, Space, Table, Tag, Typography } from 'antd';
import { useState } from 'react';
import { Loadable, PageTitle } from '../components/Page';
import { ProjectPicker } from '../components/ProjectPicker';
import { api, dt, unwrap } from '../lib/api';
import { changeNames } from './Produce';

function errMsg(e: unknown) {
  return e instanceof ApiError ? e.message : '操作失败，请重试';
}

export function Release() {
  const [project, setProject] = useState<number>();
  return (
    <>
      <PageTitle code="7.14" title="版本发布" extra={<ProjectPicker value={project} onChange={setProject} />} />
      {project ? <ReleaseBody project={project} /> : <Empty description="先选择项目" />}
    </>
  );
}

function ReleaseBody({ project }: { project: number }) {
  const qc = useQueryClient();
  const { message } = AntApp.useApp();
  const path = { params: { path: { projectId: project } } };
  const [version, setVersion] = useState('');
  const preview = useQuery({ queryKey: ['admin', 'official', 'preview', project], queryFn: () => unwrap(api.GET('/admin/official/projects/{projectId}/release-preview', path)) });
  const versions = useQuery({ queryKey: ['admin', 'official', 'versions', project], queryFn: () => unwrap(api.GET('/admin/official/projects/{projectId}/releases', path)) });
  const done = () => void qc.invalidateQueries({ queryKey: ['admin', 'official'] });
  const publish = useMutation({
    mutationFn: () => unwrap(api.POST('/admin/official/projects/{projectId}/releases', { ...path, body: { version: version || undefined } })),
    onSuccess: (v) => {
      message.success(`已发布 ${v.version}`);
      setVersion('');
      done();
    },
    onError: (e) => message.error(errMsg(e)),
  });
  const rollback = useMutation({
    mutationFn: (id: number) => unwrap(api.POST('/admin/official/projects/{projectId}/releases/{versionId}/rollback', { params: { path: { projectId: project, versionId: id } } })),
    onSuccess: () => {
      message.success('已回滚');
      done();
    },
    onError: (e) => message.error(errMsg(e)),
  });
  const p = preview.data;
  const ok = !!p && p.checks.every((c) => c.passed);
  const latest = versions.data?.items.find((v) => !v.rolled_back_at);
  return (
    <>
      <Loadable loading={preview.isPending} error={preview.error}>
        {p ? (
          <Card title={`待发布：${p.changes.length} 处变更`} style={{ marginBottom: 16 }}>
            <Table
              rowKey="draft_id"
              size="small"
              pagination={false}
              dataSource={p.changes}
              locale={{ emptyText: '自上个版本以来没有已审核的变更' }}
              columns={[
                { title: '变更', render: (_, c) => `${changeNames[c.change_type]}${c.entity_type === 'knowledge_point' ? '知识点' : '题目'}` },
                { title: '内容', dataIndex: 'title' },
                { title: '', render: (_, c) => (c.rubric_changed ? <Tag color="orange">采分点有变化，用户的背诵会重新进入复习</Tag> : null) },
              ]}
            />
            <Typography.Title level={5} style={{ marginTop: 16 }}>自动检查</Typography.Title>
            <List
              size="small"
              dataSource={p.checks}
              renderItem={(c) => (
                <List.Item>
                  {c.passed ? <Tag color="green">通过</Tag> : <Tag color="red">未通过</Tag>}
                  {c.name}
                  {c.detail ? <Typography.Text type="secondary">：{c.detail}</Typography.Text> : null}
                </List.Item>
              )}
            />
            <Alert style={{ margin: '16px 0' }} type="info" showIcon message={`通知只发给已添加的 ${p.subscribers} 位用户`} description={p.notice} />
            <Space>
              <Input style={{ width: 160 }} placeholder={`版本号（默认 ${p.next_version}）`} value={version} onChange={(e) => setVersion(e.target.value)} />
              <Popconfirm title={`发布 ${version || p.next_version}？`} description="发布后用户没改过的内容会跟着更新" onConfirm={() => publish.mutate()} disabled={!ok}>
                <Button type="primary" disabled={!ok} loading={publish.isPending}>
                  发布
                </Button>
              </Popconfirm>
            </Space>
          </Card>
        ) : null}
      </Loadable>
      <Card title="版本历史">
        <Loadable loading={versions.isPending} error={versions.error}>
          <Table
            rowKey="id"
            size="small"
            dataSource={versions.data?.items ?? []}
            columns={[
              { title: '版本', dataIndex: 'version' },
              { title: '变更', render: (_, v) => `${v.changes.length} 处` },
              { title: '发布时间', render: (_, v) => dt(v.published_at) },
              { title: '状态', render: (_, v) => (v.rolled_back_at ? <Tag>已回滚 {dt(v.rolled_back_at)}</Tag> : v.id === latest?.id ? <Tag color="green">当前</Tag> : null) },
              {
                title: '操作',
                render: (_, v) =>
                  v.id === latest?.id ? (
                    <Popconfirm title={`回滚 ${v.version}？`} description="恢复发布前的内容；用户没改过的副本跟着恢复，掌握度保留" onConfirm={() => rollback.mutate(v.id)}>
                      <Button size="small" danger loading={rollback.isPending}>
                        回滚
                      </Button>
                    </Popconfirm>
                  ) : null,
              },
            ]}
          />
        </Loadable>
      </Card>
    </>
  );
}
