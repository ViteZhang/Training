// 官方题的「必答 / 限选」（T51）：按专业分组的题（「第一组：请选择文艺学专业的考生作答」，回答非报考专业不得分）是限选题，
// 记组名与适用专业（payload.group）；没有分组是必答题。用户添加官方题库时，限选题只复制与报考方向匹配的那一组。
import { Form, Input, Radio, Space, Tag } from 'antd';

export type QuestionGroup = { label: string; majors?: string };

/** 表单初始值：限选题打开「限选」并带出组名与适用专业。 */
export function groupInit(p: Record<string, unknown>) {
  const g = p.group as QuestionGroup | undefined;
  return { elective: !!g?.label, group: g };
}

/** 表单值 → payload.group：必答题不带 group。 */
export function groupOut(v: { elective?: boolean; group?: QuestionGroup }): { group?: QuestionGroup } {
  if (!v.elective || !v.group?.label?.trim()) return { group: undefined };
  return { group: { label: v.group.label.trim(), majors: v.group.majors?.trim() || undefined } };
}

/** 题目表单里的「必答 / 限选」，限选时填组名与适用专业。 */
export function GroupFields() {
  return (
    <>
      <Form.Item name="elective" label="必答 / 限选" initialValue={false}>
        <Radio.Group
          options={[
            { value: false, label: '必答' },
            { value: true, label: '限选（按专业分组）' },
          ]}
        />
      </Form.Item>
      <Form.Item noStyle shouldUpdate={(a, b) => a.elective !== b.elective}>
        {({ getFieldValue }) =>
          getFieldValue('elective') ? (
            <Space align="start">
              <Form.Item name={['group', 'label']} label="组名" rules={[{ required: true, message: '填组名，如 第一组' }]}>
                <Input maxLength={32} placeholder="如 第一组" style={{ width: 120 }} />
              </Form.Item>
              <Form.Item name={['group', 'majors']} label="适用专业" extra="只有报考这些专业的考生作答，用户按报考方向只拿到这一组">
                <Input maxLength={255} placeholder="如 文艺学、比较文学与世界文学" style={{ width: 360 }} />
              </Form.Item>
            </Space>
          ) : null
        }
      </Form.Item>
    </>
  );
}

/** 列表里的标签：限选题标「限选 · 第一组」，必答题不标。 */
export function GroupTag({ group }: { group?: QuestionGroup }) {
  if (!group?.label) return null;
  return <Tag color="blue">限选 · {group.label}</Tag>;
}
