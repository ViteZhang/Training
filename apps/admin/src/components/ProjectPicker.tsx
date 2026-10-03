import { useQuery } from '@tanstack/react-query';
import { Select } from 'antd';
import { useEffect } from 'react';
import { api, unwrap } from '../lib/api';

export const stageNames: Record<string, string> = {
  initiated: '立项',
  materials: '授权资料入库',
  framework: '知识框架',
  producing: '内容生产',
  reviewing: '审核',
  published: '发布上线',
};

export function useProjects() {
  return useQuery({ queryKey: ['admin', 'official', 'projects'], queryFn: () => unwrap(api.GET('/admin/official/projects')) });
}

/** 选择官方题库项目（内容编辑只看到分配给自己的）；只有一个时自动选中。 */
export function ProjectPicker({ value, onChange }: { value?: number; onChange: (id: number) => void }) {
  const q = useProjects();
  const items = q.data?.items ?? [];
  const only = items.length === 1 ? items[0]?.id : undefined;
  useEffect(() => {
    if (value === undefined && only !== undefined) onChange(only);
  }, [value, only, onChange]);
  return (
    <Select
      style={{ minWidth: 320 }}
      placeholder="选择项目"
      loading={q.isPending}
      value={value}
      onChange={onChange}
      options={items.map((p) => ({ value: p.id, label: `${p.school} ${p.subject_code} ${p.subject_name}` }))}
    />
  );
}
