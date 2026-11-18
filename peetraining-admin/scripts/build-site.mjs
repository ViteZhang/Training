// 官网构建：把 site/pages/*.html 套进 site/layout.html，连同 site/public/ 输出到 dist-site/。
// 不依赖任何第三方包。
//
//   node scripts/build-site.mjs           构建到 dist-site/
//   node scripts/build-site.mjs --serve   构建后在 http://localhost:4173 预览（路径规则与容器里的 Nginx 一致）
//
// 预览时 /api 转发到 SITE_API_PROXY（默认 https://training.dreamerlab.cn），协议正文、客服二维码、下载地址都取自公开接口。
import { cp, mkdir, readFile, readdir, rm, writeFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { dirname, extname, join, normalize } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const src = join(root, 'site');
const out = join(root, 'dist-site');

// 三份协议共用 pages/legal.html，kind 与后端 /agreements/{kind} 一致。
const LEGAL = [
  { kind: 'user', title: '用户服务协议' },
  { kind: 'privacy', title: '隐私政策' },
  { kind: 'membership', title: '会员服务协议' },
];

const escapeHtml = (s) =>
  String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]);

// {{key}} 替换；没有对应值的占位符直接报错，避免把花括号发到线上。
function fill(template, vars) {
  return template.replace(/\{\{([\w.]+)\}\}/g, (_, key) => {
    if (!(key in vars)) throw new Error(`模板里的 {{${key}}} 没有对应的值`);
    return vars[key];
  });
}

// 页面文件第一行是 <!--{"title":…,"description":…,"nav":…}-->。
function parsePage(text, file) {
  const m = text.match(/^<!--(\{.*?\})-->\s*/s);
  if (!m) throw new Error(`${file} 第一行缺少页面信息注释`);
  return { meta: JSON.parse(m[1]), body: text.slice(m[0].length) };
}

// 页脚的备案信息：site.config.json 里没填的项不输出。
function filingHtml(config) {
  const items = [];
  if (config.icp) {
    items.push(`<a href="https://beian.miit.gov.cn/" target="_blank" rel="noopener">${escapeHtml(config.icp)}</a>`);
  }
  if (config.police) {
    const href = config.policeUrl ? escapeHtml(config.policeUrl) : 'https://beian.mps.gov.cn/';
    items.push(`<a href="${href}" target="_blank" rel="noopener">${escapeHtml(config.police)}</a>`);
  }
  if (config.modelNotice) items.push(`<span>${escapeHtml(config.modelNotice)}</span>`);
  return items.join('');
}

function downloadLink(url, fallback) {
  if (!url) return `href="${fallback}" aria-disabled="true"`;
  if (!/^https:\/\//.test(url)) throw new Error(`下载地址必须以 https:// 开头：${url}`);
  return `href="${escapeHtml(url)}"`;
}

async function build() {
  const config = JSON.parse(await readFile(join(src, 'site.config.json'), 'utf8'));
  const layout = await readFile(join(src, 'layout.html'), 'utf8');
  await rm(out, { recursive: true, force: true });
  await mkdir(join(out, 'legal'), { recursive: true });
  await cp(join(src, 'public'), out, { recursive: true });

  const shared = {
    filing: filingHtml(config),
    modelNotice: config.modelNotice
      ? escapeHtml(config.modelNotice)
      : 'AI 能力由阿里云百炼大模型服务提供，所有模型调用只在服务端进行。',
    // 下载按钮：site.config.json 里填了地址就直接用；没填的先置灰，由 site.js 从 /bootstrap 取后台配置的地址。
    androidLink: downloadLink(config.androidUrl, '#android'),
    iosLink: downloadLink(config.iosUrl, '#ios'),
    siteUrl: config.siteUrl.replace(/\/$/, ''),
    year: String(new Date().getFullYear()),
  };

  const render = (meta, body, path, extra = {}) => {
    const vars = { ...shared, ...extra };
    const nav = Object.fromEntries(
      ['home', 'pricing', 'help', 'about', 'download'].map((k) => [`nav.${k}`, meta.nav === k ? ' aria-current="page"' : '']),
    );
    return fill(layout, {
      ...vars,
      ...nav,
      title: escapeHtml(fill(meta.title, vars)),
      description: escapeHtml(fill(meta.description, vars)),
      path,
      content: fill(body, vars),
    });
  };

  const files = (await readdir(join(src, 'pages'))).filter((f) => f.endsWith('.html'));
  for (const file of files) {
    const name = file.replace(/\.html$/, '');
    const { meta, body } = parsePage(await readFile(join(src, 'pages', file), 'utf8'), file);
    if (name === 'legal') {
      for (const doc of LEGAL) {
        const html = render(meta, body, `/legal/${doc.kind}`, { kind: doc.kind, legalTitle: doc.title });
        await writeFile(join(out, 'legal', `${doc.kind}.html`), html);
      }
      continue;
    }
    const path = name === 'index' ? '/' : `/${name}`;
    await writeFile(join(out, file), render(meta, body, path));
  }
  console.log(`官网已构建到 dist-site/（${files.length} 个页面模板）`);
}

const TYPES = {
  '.html': 'text/html; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.svg': 'image/svg+xml',
  '.ttf': 'font/ttf',
  '.txt': 'text/plain; charset=utf-8',
  '.json': 'application/json',
};

function serve(port) {
  const api = (process.env.SITE_API_PROXY ?? 'https://training.dreamerlab.cn').replace(/\/$/, '');
  createServer(async (req, res) => {
    const url = new URL(req.url, 'http://localhost');
    if (url.pathname.startsWith('/api/')) {
      try {
        const r = await fetch(api + url.pathname + url.search);
        res.writeHead(r.status, { 'content-type': r.headers.get('content-type') ?? 'application/json' });
        res.end(Buffer.from(await r.arrayBuffer()));
      } catch {
        res.writeHead(502).end();
      }
      return;
    }
    // 与 deploy/nginx.conf 的 try_files $uri $uri.html $uri/index.html 一致。
    const base = normalize(decodeURIComponent(url.pathname)).replace(/^(\.\.[/\\])+/, '');
    for (const candidate of [base, `${base}.html`, join(base, 'index.html')]) {
      try {
        const data = await readFile(join(out, candidate));
        res.writeHead(200, { 'content-type': TYPES[extname(candidate)] ?? 'application/octet-stream' });
        res.end(data);
        return;
      } catch {
        // 试下一个候选路径
      }
    }
    res.writeHead(404, { 'content-type': TYPES['.html'] });
    res.end(await readFile(join(out, '404.html')));
  }).listen(port, () => console.log(`官网预览：http://localhost:${port}（/api → ${api}）`));
}

await build();
if (process.argv.includes('--serve')) serve(Number(process.env.SITE_PORT ?? 4173));
