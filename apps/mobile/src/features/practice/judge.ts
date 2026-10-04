// 客观题离线判分（CLAUDE.md 必须遵守第 3 条的唯一例外）：与服务端 practice.Judge 规则一致，联网后以服务端复核结果为准。
type Option = { key: string; text: string };

const trueWords = new Set(['对', '正确', '√', 'T', 'TRUE', '是', '✓']);
const falseWords = new Set(['错', '错误', '×', 'F', 'FALSE', '否', '✗']);

function truthOf(s: string): string {
  const v = s.trim().toUpperCase();
  if (trueWords.has(v)) return 'T';
  if (falseWords.has(v)) return 'F';
  return '';
}

export function keysOf(s: string): string[] {
  const out = new Set<string>();
  for (const ch of s.toUpperCase()) if (ch >= 'A' && ch <= 'H') out.add(ch);
  return [...out].sort();
}

function normText(s: string): string {
  return s.toLowerCase().replace(/[\s\p{P}\p{S}]/gu, '');
}

/** 返回 null 表示不能判（没有答案）。 */
export function judge(qtype: string, options: Option[] | undefined, answer: string | undefined, selected: string[], text: string): boolean | null {
  const ans = (answer ?? '').trim();
  if (!ans) return null;
  switch (qtype) {
    case 'single_choice':
    case 'multi_choice': {
      const want = keysOf(ans);
      if (want.length === 0) return null;
      return want.join('') === keysOf(selected.join('')).join('');
    }
    case 'true_false': {
      const textOf = new Map((options ?? []).map((o) => [o.key.toUpperCase(), o.text]));
      const norm = (v: string) => truthOf(v) || truthOf(textOf.get(v.trim().toUpperCase()) ?? '');
      const want = norm(ans);
      if (!want) return null;
      return selected.length === 1 && norm(selected[0]!) === want;
    }
    case 'fill_blank': {
      const split = (s: string) => s.split(/[；;|]/).filter((x) => x.trim() !== '').map(normText);
      const want = split(ans);
      const got = split(text);
      if (want.length === 0) return null;
      return want.length === got.length && want.every((w, i) => w === got[i]);
    }
  }
  return null;
}

export const isObjective = (qtype: string) => ['single_choice', 'multi_choice', 'true_false', 'fill_blank'].includes(qtype);
