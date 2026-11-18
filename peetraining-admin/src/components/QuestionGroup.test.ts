import { describe, expect, it } from 'vitest';
import { groupInit, groupOut } from './QuestionGroup';

// T51：限选题写 payload.group，必答题不带。
describe('必答 / 限选', () => {
  it('限选题带出组名与适用专业', () => {
    expect(groupInit({ group: { label: '第二组', majors: '古代文学' } })).toEqual({ elective: true, group: { label: '第二组', majors: '古代文学' } });
    expect(groupInit({})).toEqual({ elective: false, group: undefined });
  });

  it('切回必答或没填组名时去掉 group；空的适用专业不带', () => {
    expect(groupOut({ elective: false, group: { label: '第一组' } })).toEqual({ group: undefined });
    expect(groupOut({ elective: true, group: { label: '  ' } })).toEqual({ group: undefined });
    expect(groupOut({ elective: true, group: { label: ' 第一组 ', majors: ' ' } })).toEqual({ group: { label: '第一组', majors: undefined } });
  });
});
