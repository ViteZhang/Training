import { parseAgreementBody, parseInline } from '@/features/auth/agreementBody';

describe('0.4 协议正文排版', () => {
  it('解析段落中间的加粗，也认导出转义的 \\*\\*', () => {
    expect(parseInline('前文**重点**后文')).toEqual([{ text: '前文' }, { text: '重点', bold: true }, { text: '后文' }]);
    expect(parseInline('a \\*\\*b\\*\\* c')).toEqual([{ text: 'a ' }, { text: 'b', bold: true }, { text: ' c' }]);
  });

  it('切出标题、列表、表格、段落', () => {
    const body = [
      '### 一、适用对象',
      '',
      '1. 第一条',
      '2. **第二条**',
      '',
      '| 权限 | 用途 |',
      '| --- | --- |',
      '| 相机 | 拍照 |',
      '',
      '本服务主要包括：',
      '- 自建题库',
    ].join('\n');
    const blocks = parseAgreementBody(body);
    expect(blocks.map((b) => b.type)).toEqual(['heading', 'list', 'table', 'paragraph', 'list']);
    expect(blocks[1]).toMatchObject({ type: 'list', items: [{ marker: '1.' }, { marker: '2.', spans: [{ text: '第二条', bold: true }] }] });
    expect(blocks[2]).toEqual({ type: 'table', header: [[{ text: '权限' }], [{ text: '用途' }]], rows: [[[{ text: '相机' }], [{ text: '拍照' }]]] });
    expect(blocks[4]).toMatchObject({ items: [{ marker: '•' }] });
  });
});
