/** 把「院校 · 专业」拆开；分隔符前后的空格、中点写法不统一都兼容。 */
export function splitTarget(v?: string | null): { school: string; major: string } {
  const [school = '', ...rest] = (v ?? '').split(/\s*[·•]\s*/);
  return { school: school.trim(), major: rest.join(' · ').trim() };
}
