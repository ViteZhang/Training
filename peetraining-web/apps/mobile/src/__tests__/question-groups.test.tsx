import { render, screen } from '@testing-library/react-native';
import { GroupNote, GroupTag } from '../components/Passage';

// T51：限选题标「限选 · 第几组」；卷子里有限选题时，其余题标「必答」。
describe('必答 / 限选标签', () => {
  it('限选题带组名，详情带适用专业', async () => {
    await render(<GroupTag group={{ label: '第二组', majors: '古代文学、中国古典文献学' }} />);
    expect(screen.getByText('限选 · 第二组')).toBeTruthy();
    await render(<GroupTag group={{ label: '第二组', majors: '古代文学、中国古典文献学' }} full />);
    expect(screen.getByText('限选 · 第二组 · 古代文学、中国古典文献学')).toBeTruthy();
  });

  it('没有分组：卷子里有限选题时标必答，否则不标', async () => {
    await render(<GroupTag group={undefined} />);
    expect(screen.queryByText('必答')).toBeNull();
    await render(<GroupTag group={undefined} required />);
    expect(screen.getByText('必答')).toBeTruthy();
  });

  it('详情说明写明回答非报考专业不得分', async () => {
    await render(<GroupNote group={{ label: '第一组', majors: '文艺学、比较文学与世界文学' }} />);
    expect(screen.getByText('限选题（第一组）：文艺学、比较文学与世界文学专业的考生作答，回答非报考专业不得分')).toBeTruthy();
  });
});
