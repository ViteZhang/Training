// 1.4 选择导入方式（引导第 4 步）。导入流程（1.5–1.8）在 app/import 下，与题库页「导入」共用。
import { ModeStep } from '@/features/import/ModeStep';

export default function ImportStep() {
  return <ModeStep onboarding />;
}
