import { semantic } from '@training/ui-tokens';
import type { ColorValue } from 'react-native';
import Svg, { Circle, Path } from 'react-native-svg';

/** 线性图标，路径取自设计稿（24 网格、1.7 线宽）。 */
const paths = {
  today: <Path d="M3 10.5L12 3l9 7.5V20a1 1 0 0 1-1 1h-5v-6H9v6H4a1 1 0 0 1-1-1z" />,
  bank: <Path d="M5 4.5A1.5 1.5 0 0 1 6.5 3H19v15H6.5A1.5 1.5 0 0 0 5 19.5zM5 19.5A1.5 1.5 0 0 0 6.5 21H19v-3M9 7h6" />,
  train: <Path d="M4 20h4L19 9l-4-4L4 16zM13.5 6.5l4 4" />,
  me: (
    <>
      <Circle cx={12} cy={8} r={4} />
      <Path d="M4 21c1.5-4 4.5-6 8-6s6.5 2 8 6" />
    </>
  ),
  import: <Path d="M12 16V4M7 9l5-5 5 5M4 20h16" />,
  empty: <Path d="M4 7h16v12a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1zM4 7l2-3h12l2 3M9 12h6" />,
  offline: <Path d="M2 8.5a15 15 0 0 1 20 0M5 12a10 10 0 0 1 14 0M8.5 15.5a5 5 0 0 1 7 0M12 19h.01M3 3l18 18" />,
  sparkle: <Path d="M12 3l1.8 5.2L19 10l-5.2 1.8L12 17l-1.8-5.2L5 10l5.2-1.8zM19 16l.8 2.2L22 19l-2.2.8L19 22l-.8-2.2L16 19l2.2-.8z" />,
  lock: <Path d="M6 10h12v10H6zM8 10V7a4 4 0 0 1 8 0v3" />,
  close: <Path d="M6 6l12 12M18 6L6 18" />,
  check: <Path d="M5 12l5 5L20 7" />,
  bell: <Path d="M6 16V11a6 6 0 0 1 12 0v5l2 2H4zM10 20a2 2 0 0 0 4 0" />,
  camera: (
    <>
      <Path d="M4 8h3l1.5-2h7L17 8h3v11H4z" />
      <Circle cx={12} cy={13} r={3.5} />
    </>
  ),
  image: (
    <>
      <Path d="M4 5h16v14H4zM4 16l5-5 4 4 3-3 4 4" />
      <Circle cx={9} cy={9} r={1.5} />
    </>
  ),
  mic: <Path d="M12 3a3 3 0 0 1 3 3v6a3 3 0 0 1-6 0V6a3 3 0 0 1 3-3zM5 11a7 7 0 0 0 14 0M12 18v3" />,
  file: <Path d="M7 3h7l5 5v13H7zM14 3v5h5" />,
  clipboard: <Path d="M8 4h8v3H8zM8 5.5H6v15.5h12V5.5h-2M9 12h6M9 16h4" />,
  chevron: <Path d="M9 5l7 7-7 7" />,
  back: <Path d="M15 5l-7 7 7 7" />,
  edit: <Path d="M4 20h4L18 10l-4-4L4 16zM14 6l4 4" />,
  pen: <Path d="M4 20h4L19 9l-4-4L4 16zM13.5 6.5l4 4" />,
  wrong: (
    <>
      <Circle cx={12} cy={12} r={8.5} />
      <Path d="M9 9l6 6M15 9l-6 6" />
    </>
  ),
  paper: <Path d="M7 3h7l5 5v13H7zM14 3v5h5M10 13h6M10 17h6" />,
  book: <Path d="M5 4.5A1.5 1.5 0 0 1 6.5 3H19v15H6.5A1.5 1.5 0 0 0 5 19.5zM5 19.5A1.5 1.5 0 0 0 6.5 21H19v-3" />,
} as const;

export type IconName = keyof typeof paths;

export function Icon({ name, size = 22, color = semantic.textPrimary }: { name: IconName; size?: number; color?: ColorValue }) {
  return (
    <Svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth={1.7} strokeLinecap="round" strokeLinejoin="round">
      {paths[name]}
    </Svg>
  );
}
