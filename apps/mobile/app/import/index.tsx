// 1.4 选择导入方式（从首页、题库页进入）。可带 subjectId 预选专业课。
import { useLocalSearchParams } from 'expo-router';
import { ModeStep } from '@/features/import/ModeStep';

export default function ImportIndex() {
  const { subjectId } = useLocalSearchParams<{ subjectId?: string }>();
  return <ModeStep onboarding={false} subjectId={subjectId ? Number(subjectId) : undefined} />;
}
