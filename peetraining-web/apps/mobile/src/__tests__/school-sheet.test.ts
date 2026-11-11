import { splitTarget } from '../features/profile/target';

describe('6.9 目标院校', () => {
  it('把「院校 · 专业」拆开，兼容不同的中点和空格', () => {
    expect(splitTarget('海南大学 · 中国语言文学')).toEqual({ school: '海南大学', major: '中国语言文学' });
    expect(splitTarget('海南大学•中国语言文学')).toEqual({ school: '海南大学', major: '中国语言文学' });
    expect(splitTarget('海南大学')).toEqual({ school: '海南大学', major: '' });
    expect(splitTarget(null)).toEqual({ school: '', major: '' });
  });
});
