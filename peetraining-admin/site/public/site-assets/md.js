// 协议正文用的最小 Markdown 渲染：标题、段落、加粗、链接、有序 / 无序列表（可嵌套）、表格、分割线。
// 正文来自后端 /agreements/{kind}，一律先转义再渲染，不输出原始 HTML。

const ESC = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' };
export const escapeHtml = (s) => String(s).replace(/[&<>"']/g, (c) => ESC[c]);

// 行内：加粗、链接（只放行 http、https、mailto 和站内路径）。
export function inline(text) {
  return escapeHtml(text)
    .replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>')
    .replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, (m, label, href) =>
      /^(https?:\/\/|mailto:|\/)/.test(href) ? `<a href="${href}" rel="noopener">${label}</a>` : m,
    );
}

const LIST = /^(\s*)([-*]|\d+[.、)])\s+(.*)$/;
const isTableRow = (line) => /^\s*\|.*\|\s*$/.test(line);
const isTableRule = (line) => /^\s*\|[\s:|-]+\|\s*$/.test(line) && line.includes('-');
const cells = (line) => line.trim().replace(/^\||\|$/g, '').split('|').map((c) => c.trim());

function renderList(items) {
  // items: [{ indent, ordered, text }]；按缩进生成嵌套列表。
  let html = '';
  const stack = [];
  for (const item of items) {
    while (stack.length && item.indent < stack[stack.length - 1].indent) {
      html += `</li></${stack.pop().tag}>`;
    }
    const top = stack[stack.length - 1];
    if (!top || item.indent > top.indent) {
      const tag = item.ordered ? 'ol' : 'ul';
      html += `<${tag}>`;
      stack.push({ indent: item.indent, tag });
    } else {
      html += '</li>';
    }
    html += `<li>${inline(item.text)}`;
  }
  while (stack.length) html += `</li></${stack.pop().tag}>`;
  return html;
}

export function renderMarkdown(source) {
  const lines = String(source ?? '').replace(/\r\n?/g, '\n').split('\n');
  const out = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    if (!line.trim()) {
      i += 1;
      continue;
    }
    const heading = line.match(/^(#{1,4})\s+(.*)$/);
    if (heading) {
      // 页面标题已占用 h1，正文标题从 h2 起。
      const level = Math.min(heading[1].length + 1, 4);
      out.push(`<h${level}>${inline(heading[2])}</h${level}>`);
      i += 1;
      continue;
    }
    if (/^\s*(-{3,}|\*{3,})\s*$/.test(line)) {
      out.push('<hr>');
      i += 1;
      continue;
    }
    if (isTableRow(line) && i + 1 < lines.length && isTableRule(lines[i + 1])) {
      const head = cells(line);
      i += 2;
      const rows = [];
      while (i < lines.length && isTableRow(lines[i])) {
        rows.push(cells(lines[i]));
        i += 1;
      }
      out.push(
        '<div class="tablebox"><table><thead><tr>' +
          head.map((c) => `<th scope="col">${inline(c)}</th>`).join('') +
          '</tr></thead><tbody>' +
          rows.map((r) => `<tr>${r.map((c) => `<td>${inline(c)}</td>`).join('')}</tr>`).join('') +
          '</tbody></table></div>',
      );
      continue;
    }
    if (LIST.test(line)) {
      const items = [];
      while (i < lines.length) {
        const m = lines[i].match(LIST);
        if (m) {
          items.push({ indent: m[1].length, ordered: /\d/.test(m[2]), text: m[3] });
          i += 1;
        } else if (lines[i].trim() && /^\s+/.test(lines[i]) && items.length) {
          // 列表项的续行
          items[items.length - 1].text += ` ${lines[i].trim()}`;
          i += 1;
        } else {
          break;
        }
      }
      out.push(renderList(items));
      continue;
    }
    const para = [];
    while (
      i < lines.length &&
      lines[i].trim() &&
      !/^#{1,4}\s/.test(lines[i]) &&
      !LIST.test(lines[i]) &&
      !(isTableRow(lines[i]) && i + 1 < lines.length && isTableRule(lines[i + 1]))
    ) {
      para.push(lines[i].trim());
      i += 1;
    }
    out.push(`<p>${inline(para.join(' '))}</p>`);
  }
  return out.join('\n');
}
