// 官网协议正文渲染（site/public/site-assets/md.js）
import { describe, expect, it } from 'vitest';
import { inline, renderMarkdown } from './public/site-assets/md.js';

describe('官网 Markdown 渲染', () => {
  it('原始 HTML 一律转义', () => {
    expect(renderMarkdown('<script>alert(1)</script>')).toBe('<p>&lt;script&gt;alert(1)&lt;/script&gt;</p>');
    expect(inline('[x](javascript:alert(1))')).not.toContain('<a');
  });

  it('标题从 h2 起，加粗与段落', () => {
    const html = renderMarkdown('### 一、适用对象\n\n**请仔细阅读**本协议。\n续行。');
    expect(html).toContain('<h4>一、适用对象</h4>');
    expect(html).toContain('<p><strong>请仔细阅读</strong>本协议。 续行。</p>');
  });

  it('有序列表里嵌套无序列表', () => {
    const html = renderMarkdown('1. 第一条\n   - 子项 A\n   - 子项 B\n2. 第二条');
    expect(html).toBe('<ol><li>第一条<ul><li>子项 A</li><li>子项 B</li></ul></li><li>第二条</li></ol>');
  });

  it('表格', () => {
    const html = renderMarkdown('| 档位 | 有效期 |\n| --- | --- |\n| 月卡 | 30 日 |');
    expect(html).toContain('<th scope="col">档位</th>');
    expect(html).toContain('<td>月卡</td><td>30 日</td>');
  });

  it('站内与 https 链接放行', () => {
    expect(inline('见[隐私政策](/legal/privacy)')).toContain('<a href="/legal/privacy"');
  });
});
