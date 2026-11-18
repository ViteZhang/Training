// 官网脚本：导航、下载地址、客服企业微信、协议正文。
// 数据都来自后端的公开接口（不需要登录），和 App 用的是同一份配置：
//   /bootstrap       后台配置的最新版本与下载地址（安卓安装包、iOS TestFlight）
//   /purchase-entry  客服企业微信名称、二维码、回复时间
//   /agreements/:kind 协议当前生效版本的正文
import { renderMarkdown } from './md.js';

const API = '/api/v1';

async function getJson(path) {
  const res = await fetch(API + path, { headers: { accept: 'application/json' } });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}

const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => [...root.querySelectorAll(sel)];
const ua = navigator.userAgent;
const inWeChat = /MicroMessenger/i.test(ua);

// 只接受 http(s) 地址，避免把接口返回的异常值写进 href / src。
const safeUrl = (value) => (typeof value === 'string' && /^https?:\/\//.test(value) ? value : '');

function initNav() {
  const toggle = $('.nav__toggle');
  const links = $('#nav-links');
  if (!toggle || !links) return;
  toggle.addEventListener('click', () => {
    const open = links.classList.toggle('is-open');
    toggle.setAttribute('aria-expanded', String(open));
    toggle.setAttribute('aria-label', open ? '收起菜单' : '打开菜单');
  });
}

// 客服企业微信：页面里放 <div data-wecom>，接口没有返回联系方式时显示 [data-wecom-empty]。
async function initWecom() {
  const boxes = $$('[data-wecom]');
  if (!boxes.length) return;
  let contact = null;
  try {
    const entry = await getJson('/purchase-entry?platform=android');
    if (entry.enabled && entry.contact) contact = entry.contact;
  } catch {
    contact = null;
  }
  for (const box of boxes) {
    const qr = contact && safeUrl(contact.qr_url);
    if (!contact || !qr) {
      $('[data-wecom-empty]', box)?.removeAttribute('hidden');
      continue;
    }
    const card = $('[data-wecom-card]', box);
    $('img', card).src = qr;
    $('[data-wecom-name]', card).textContent = contact.name || '考研Training 客服';
    const time = (contact.reply_time || '').trim().replace(/\s*-{1,2}\s*/g, '–');
    const timeEl = $('[data-wecom-time]', card);
    if (time) timeEl.textContent = `服务时间 ${time}`;
    else timeEl.hidden = true;
    const id = (contact.wecom_id || '').trim();
    const idEl = $('[data-wecom-id]', card);
    if (id) idEl.textContent = `企业微信 ID：${id}`;
    else idEl.hidden = true;
    card.hidden = false;
  }
}

// 下载页：有下载地址就点亮按钮，没有就保留「添加客服获取」的说明。
async function initDownload() {
  const cards = $$('[data-download]');
  if (!cards.length) return;
  await Promise.all(
    cards.map(async (card) => {
      const platform = card.dataset.download;
      let update = null;
      try {
        const boot = await getJson(`/bootstrap?platform=${platform}&app_version=0.0.0`);
        update = boot.update ?? null;
      } catch {
        update = null;
      }
      const btn = $('[data-download-btn]', card);
      // 构建时已写好地址的按钮直接可用；置灰的才用后台配置的地址，取不到就显示「添加客服获取」。
      if (btn.getAttribute('aria-disabled') === 'true') {
        const url = update && safeUrl(update.download_url);
        if (!url) {
          $('[data-download-empty]', card)?.removeAttribute('hidden');
          return;
        }
        btn.href = url;
        btn.removeAttribute('aria-disabled');
      }
      if (update?.latest_version) {
        const ver = $('[data-download-ver]', card);
        ver.textContent = `最新版本 ${update.latest_version}`;
        ver.hidden = false;
      }
      // 微信内置浏览器不能直接下载安装包，点按钮时先提示换浏览器。
      if (inWeChat && platform === 'android') {
        btn.addEventListener('click', (e) => {
          e.preventDefault();
          $('#wx-tip')?.removeAttribute('hidden');
        });
      }
    }),
  );
  $('#wx-tip button')?.addEventListener('click', () => $('#wx-tip').setAttribute('hidden', ''));
  // 从首页带着 #android / #ios 过来、或按系统自动把对应的卡片排在前面。
  const prefer = location.hash === '#ios' || /iPhone|iPad|iPod/i.test(ua) ? 'ios' : 'android';
  const first = $(`[data-download="${prefer}"]`);
  if (first && first.parentElement.firstElementChild !== first) first.parentElement.prepend(first);
}

async function initLegal() {
  const box = $('[data-legal]');
  if (!box) return;
  const body = $('[data-legal-body]', box);
  const fail = $('[data-legal-fail]', box);
  const load = async () => {
    fail.hidden = true;
    try {
      const doc = await getJson(`/agreements/${encodeURIComponent(box.dataset.legal)}`);
      body.innerHTML = renderMarkdown(doc.body);
      const meta = $('[data-legal-meta]', box);
      const date = doc.effective_at ? new Date(doc.effective_at) : null;
      const parts = [];
      if (doc.version) parts.push(`版本 ${doc.version}`);
      if (date && !Number.isNaN(date.getTime())) {
        parts.push(`生效日期 ${date.getFullYear()} 年 ${date.getMonth() + 1} 月 ${date.getDate()} 日`);
      }
      meta.textContent = parts.join(' · ');
      meta.hidden = parts.length === 0;
    } catch {
      body.innerHTML = '';
      fail.hidden = false;
    }
  };
  $('button', fail)?.addEventListener('click', load);
  await load();
}

initNav();
initWecom();
initDownload();
initLegal();
