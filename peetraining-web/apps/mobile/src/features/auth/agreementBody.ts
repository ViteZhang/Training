// 0.4 协议正文的排版解析：后台粘贴的是 Markdown 的一个子集（标题、段落、列表、表格、加粗），
// App 不引入 Markdown 库，只认这几种写法，其余原样当段落显示。

/** 一段文字里的片段：bold 为加粗。 */
export interface Span {
  text: string;
  bold?: boolean;
}

export type Block =
  | { type: 'heading'; spans: Span[] }
  | { type: 'paragraph'; spans: Span[] }
  | { type: 'list'; items: { marker: string; spans: Span[] }[] }
  | { type: 'table'; header: Span[][]; rows: Span[][][] };

/** 解析行内加粗 **…**；导出工具转义的 \*\* 也按加粗处理。 */
export function parseInline(text: string): Span[] {
  const src = text.replace(/\\\*/g, '*');
  const spans: Span[] = [];
  const re = /\*\*(.+?)\*\*/g;
  let last = 0;
  for (const m of src.matchAll(re)) {
    const at = m.index ?? 0;
    if (at > last) spans.push({ text: src.slice(last, at) });
    spans.push({ text: m[1] ?? '', bold: true });
    last = at + m[0].length;
  }
  if (last < src.length) spans.push({ text: src.slice(last) });
  return spans.length ? spans : [{ text: '' }];
}

const listRe = /^(\d+\.|[-*•])\s+(.*)$/;

function tableCells(line: string): string[] {
  return line.replace(/^\|/, '').replace(/\|$/, '').split('|').map((c) => c.trim());
}

const isTableRule = (line: string) => /^\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?$/.test(line);
const isTableRow = (line: string) => line.startsWith('|');
const isHeading = (line: string) => line.startsWith('#');

/** 把正文切成块：空行分段；列表行、表格行各自成块。 */
export function parseAgreementBody(body: string): Block[] {
  const lines = body.replace(/\r\n?/g, '\n').split('\n').map((l) => l.trim());
  const at = (k: number) => lines[k] ?? '';
  const blocks: Block[] = [];
  let i = 0;
  while (i < lines.length) {
    const line = at(i);
    if (!line) {
      i++;
    } else if (isHeading(line)) {
      blocks.push({ type: 'heading', spans: parseInline(line.replace(/^#+\s*/, '')) });
      i++;
    } else if (isTableRow(line)) {
      const rows: string[] = [];
      while (i < lines.length && isTableRow(at(i))) rows.push(at(i++));
      const hasHeader = rows.length > 1 && isTableRule(rows[1] ?? '');
      const cells = rows.filter((r) => !isTableRule(r)).map((r) => tableCells(r).map(parseInline));
      blocks.push({ type: 'table', header: hasHeader ? (cells[0] ?? []) : [], rows: hasHeader ? cells.slice(1) : cells });
    } else if (listRe.test(line)) {
      const items: { marker: string; spans: Span[] }[] = [];
      for (let m = at(i).match(listRe); m; m = at(i).match(listRe)) {
        const marker = m[1] ?? '';
        items.push({ marker: /\d/.test(marker) ? marker : '•', spans: parseInline(m[2] ?? '') });
        i++;
      }
      blocks.push({ type: 'list', items });
    } else {
      // 普通段落：连续的非空行合成一段，遇到标题、表格、列表另起
      const para: string[] = [];
      for (let l = at(i); l && !isHeading(l) && !isTableRow(l) && !listRe.test(l); l = at(i)) {
        para.push(l);
        i++;
      }
      blocks.push({ type: 'paragraph', spans: parseInline(para.join('\n')) });
    }
  }
  return blocks;
}
