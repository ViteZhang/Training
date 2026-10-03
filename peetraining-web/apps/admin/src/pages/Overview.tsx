// 7.1 概览：注册与本周新增、本周活跃、本周解析页数与成功率、付费用户与转化、本月收入；新用户漏斗；导入方式占比；
// 每日作答题数；热门专业课（按代码聚合）；「需要处理」提醒。只读 Worker 每小时汇总的聚合表。
import { useQuery } from '@tanstack/react-query';
import { Alert, Card, Col, List, Progress, Row, Statistic, Table, Typography } from 'antd';
import { Loadable, PageTitle, PrivacyNote } from '../components/Page';
import { api, dt, pct, unwrap, yuan } from '../lib/api';

const funnelNames: Record<string, string> = { registered: '注册', imported: '导入第一份资料', trained: '完成首次训练', retained: '7 日后仍在练', paid: '付费' };
const modeNames: Record<string, string> = { question: '题目类', reference: '参考类', essay: '作文类' };

export function Overview() {
  const q = useQuery({ queryKey: ['admin', 'overview'], queryFn: () => unwrap(api.GET('/admin/overview')) });
  const o = q.data;
  const reg = o?.funnel.find((f) => f.name === 'registered')?.value ?? 0;
  const modeTotal = o?.import_modes.reduce((s, m) => s + m.value, 0) ?? 0;
  const maxAnswers = Math.max(1, ...(o?.days.map((d) => d.answers) ?? [1]));
  return (
    <>
      <PageTitle code="7.1" title="概览" extra={<Typography.Text type="secondary">数据更新于 {dt(o?.updated_at)}（每小时汇总）</Typography.Text>} />
      <Loadable loading={q.isPending} error={q.error}>
        {o ? (
          <>
            {o.alerts.map((a) => (
              <Alert key={a} type="warning" showIcon message={a} style={{ marginBottom: 12 }} />
            ))}
            <Row gutter={16}>
              <Col span={6}>
                <Card><Statistic title="注册用户" value={o.users_total} suffix={<Typography.Text type="secondary"> 本周 +{o.week_new_users}</Typography.Text>} /></Card>
              </Col>
              <Col span={6}>
                <Card><Statistic title="本周活跃" value={o.week_active_users} /></Card>
              </Col>
              <Col span={6}>
                <Card><Statistic title="本周解析页数" value={o.week_parse_pages} suffix={<Typography.Text type="secondary"> 成功率 {pct(o.week_parse_rate)}</Typography.Text>} /></Card>
              </Col>
              <Col span={6}>
                <Card>
                  <Statistic title="付费用户" value={o.paid_users} suffix={<Typography.Text type="secondary"> 转化 {pct(o.conversion)} · 本月 {yuan(o.month_revenue_cents)}</Typography.Text>} />
                </Card>
              </Col>
            </Row>
            <Row gutter={16} style={{ marginTop: 16 }}>
              <Col span={12}>
                <Card title="新用户漏斗">
                  {o.funnel.map((f) => (
                    <div key={f.name} style={{ marginBottom: 8 }}>
                      <Typography.Text>{funnelNames[f.name] ?? f.name} · {f.value}</Typography.Text>
                      <Progress percent={reg ? Math.round((f.value / reg) * 1000) / 10 : 0} />
                    </div>
                  ))}
                </Card>
              </Col>
              <Col span={12}>
                <Card title="导入方式（本周）">
                  {o.import_modes.map((m) => (
                    <div key={m.name} style={{ marginBottom: 8 }}>
                      <Typography.Text>{modeNames[m.name] ?? m.name}</Typography.Text>
                      <Progress percent={modeTotal ? Math.round((m.value / modeTotal) * 100) : 0} />
                    </div>
                  ))}
                </Card>
                <Card title="每日作答题数 · 近 7 天" style={{ marginTop: 16 }}>
                  <div style={{ display: 'flex', alignItems: 'flex-end', gap: 8, height: 120 }} aria-label="每日作答题数">
                    {o.days.map((d) => (
                      <div key={d.day} style={{ flex: 1, textAlign: 'center' }}>
                        <div style={{ height: (d.answers / maxAnswers) * 90, background: '#231F55', borderRadius: 4 }} />
                        <Typography.Text type="secondary" style={{ fontSize: 12 }}>{d.day.slice(5)}</Typography.Text>
                      </div>
                    ))}
                  </div>
                </Card>
              </Col>
            </Row>
            <Card title="热门专业课 · 按用户填的代码聚合" style={{ marginTop: 16 }}>
              <Table
                size="small"
                pagination={false}
                rowKey="name"
                dataSource={o.hot_subjects}
                columns={[
                  { title: '专业课代码', dataIndex: 'name' },
                  { title: '用户数', dataIndex: 'value' },
                ]}
              />
            </Card>
            {o.alerts.length === 0 ? (
              <Card title="需要处理" style={{ marginTop: 16 }}>
                <List dataSource={['暂无需要处理的事项']} renderItem={(t) => <List.Item>{t}</List.Item>} />
              </Card>
            ) : null}
          </>
        ) : null}
      </Loadable>
      <PrivacyNote />
    </>
  );
}
