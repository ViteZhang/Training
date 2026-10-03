// 规则参数编辑器：把 JSON 展开成「路径 → 值」逐项编辑，只能改数值、开关、文字，结构由后端校验（与原来一致）。
// 额度（quota.*）的数字可以设为「不限」（null）。
import type { Schemas } from '@training/api-client';
import { Button, Checkbox, Input, InputNumber, Space, Switch, Table, Typography } from 'antd';
import { useState } from 'react';

type Leaf = { path: string; value: unknown };

export function flatten(v: unknown, prefix = ''): Leaf[] {
  if (v !== null && typeof v === 'object' && !Array.isArray(v)) {
    return Object.entries(v as Record<string, unknown>).flatMap(([k, x]) => flatten(x, prefix ? `${prefix}.${k}` : k));
  }
  return [{ path: prefix, value: v }];
}

export function setPath(obj: Record<string, unknown>, path: string, value: unknown): Record<string, unknown> {
  const copy = structuredClone(obj);
  const parts = path.split('.');
  let cur = copy as Record<string, unknown>;
  for (const p of parts.slice(0, -1)) cur = cur[p] as Record<string, unknown>;
  cur[parts[parts.length - 1]!] = value;
  return copy;
}

export function ParamEditor({ param, onSave, saving }: { param: Schemas['AdminParam']; onSave: (v: Record<string, unknown>) => void; saving?: boolean }) {
  const [value, setValue] = useState<Record<string, unknown>>(param.value);
  const leaves = flatten(value);
  const nullable = param.key === 'quota';
  const dirty = JSON.stringify(value) !== JSON.stringify(param.value);
  return (
    <>
      <Typography.Paragraph type="secondary">{param.description}</Typography.Paragraph>
      <Table
        size="small"
        pagination={false}
        rowKey="path"
        dataSource={leaves}
        columns={[
          { title: '参数', dataIndex: 'path', width: '50%' },
          {
            title: '值',
            render: (_, l: Leaf) => {
              const set = (v: unknown) => setValue(setPath(value, l.path, v));
              if (typeof l.value === 'boolean') return <Switch aria-label={l.path} checked={l.value} onChange={set} />;
              if (typeof l.value === 'string') return <Input aria-label={l.path} value={l.value} onChange={(e) => set(e.target.value)} />;
              if (Array.isArray(l.value)) return <Input aria-label={l.path} value={JSON.stringify(l.value)} onChange={(e) => { try { set(JSON.parse(e.target.value)); } catch { /* 输入中 */ } }} />;
              return (
                <Space>
                  <InputNumber aria-label={l.path} min={0} value={l.value as number | null} disabled={l.value === null} onChange={(v) => set(v ?? 0)} />
                  {nullable ? (
                    <Checkbox checked={l.value === null} onChange={(e) => set(e.target.checked ? null : 0)}>
                      不限
                    </Checkbox>
                  ) : null}
                </Space>
              );
            },
          },
        ]}
      />
      <Space style={{ marginTop: 12 }}>
        <Button type="primary" disabled={!dirty} loading={saving} onClick={() => onSave(value)}>
          保存
        </Button>
        <Button disabled={!dirty} onClick={() => setValue(param.value)}>
          还原
        </Button>
        <Typography.Text type="secondary">保存后立即生效；改额度只影响之后的使用，改价格只影响新订单</Typography.Text>
      </Space>
    </>
  );
}
