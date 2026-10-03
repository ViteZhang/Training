// 知识图谱布局（3.9）：每个板块一团，团内用简化的力导向迭代排开节点，结果确定、可测试。
// 200 个节点以内计算量 O(n² × 迭代次数)，在手机上一次计算完成，之后只做缩放平移，不再重算。

export interface LayoutNode {
  id: number;
  sectionId: number;
  examCount: number;
}

export interface LayoutEdge {
  source: number;
  target: number;
}

export interface Placed {
  x: number;
  y: number;
  r: number;
}

/** 节点半径：圆越大真题考得越多。 */
export function radiusOf(examCount: number) {
  return 10 + Math.min(examCount, 6) * 3;
}

export function layoutGraph(nodes: LayoutNode[], edges: LayoutEdge[], size = 800, iterations = 120): Map<number, Placed> {
  const out = new Map<number, Placed>();
  if (nodes.length === 0) return out;
  const sections = [...new Set(nodes.map((n) => n.sectionId))];
  const center = size / 2;
  const ring = sections.length > 1 ? size * 0.3 : 0;
  const anchor = new Map<number, { x: number; y: number }>();
  sections.forEach((s, i) => {
    const a = (2 * Math.PI * i) / sections.length;
    anchor.set(s, { x: center + ring * Math.cos(a), y: center + ring * Math.sin(a) });
  });
  // 初始位置：在板块中心附近按黄金角螺旋排开（确定性，不用随机数）。
  const idx = new Map<number, number>();
  const pos = nodes.map((n, i) => {
    idx.set(n.id, i);
    const a = anchor.get(n.sectionId)!;
    const k = nodes.slice(0, i).filter((m) => m.sectionId === n.sectionId).length;
    const angle = k * 2.399963;
    const dist = 18 * Math.sqrt(k + 1);
    return { x: a.x + dist * Math.cos(angle), y: a.y + dist * Math.sin(angle), vx: 0, vy: 0 };
  });
  const links = edges.map((e) => [idx.get(e.source), idx.get(e.target)] as const).filter((l): l is readonly [number, number] => l[0] !== undefined && l[1] !== undefined);
  for (let it = 0; it < iterations; it++) {
    const cool = 1 - it / iterations;
    // 互斥：节点之间推开。
    for (let i = 0; i < pos.length; i++) {
      for (let j = i + 1; j < pos.length; j++) {
        const dx = pos[i]!.x - pos[j]!.x;
        const dy = pos[i]!.y - pos[j]!.y;
        const d2 = Math.max(dx * dx + dy * dy, 1);
        const f = 900 / d2;
        pos[i]!.vx += dx * f;
        pos[i]!.vy += dy * f;
        pos[j]!.vx -= dx * f;
        pos[j]!.vy -= dy * f;
      }
    }
    // 关联：拉近。
    for (const [a, b] of links) {
      const dx = pos[b]!.x - pos[a]!.x;
      const dy = pos[b]!.y - pos[a]!.y;
      const d = Math.sqrt(dx * dx + dy * dy) || 1;
      const f = (d - 70) * 0.02;
      pos[a]!.vx += (dx / d) * f * d * 0.05;
      pos[a]!.vy += (dy / d) * f * d * 0.05;
      pos[b]!.vx -= (dx / d) * f * d * 0.05;
      pos[b]!.vy -= (dy / d) * f * d * 0.05;
    }
    // 回到板块中心。
    nodes.forEach((n, i) => {
      const a = anchor.get(n.sectionId)!;
      pos[i]!.vx += (a.x - pos[i]!.x) * 0.01;
      pos[i]!.vy += (a.y - pos[i]!.y) * 0.01;
    });
    for (const p of pos) {
      const step = Math.min(Math.sqrt(p.vx * p.vx + p.vy * p.vy), 12 * cool + 1);
      const len = Math.sqrt(p.vx * p.vx + p.vy * p.vy) || 1;
      p.x += (p.vx / len) * step;
      p.y += (p.vy / len) * step;
      p.vx = 0;
      p.vy = 0;
    }
  }
  // 平移缩放到画布内，四周留出节点半径的余量。
  const pad = 30;
  const xs = pos.map((p) => p.x);
  const ys = pos.map((p) => p.y);
  const minX = Math.min(...xs);
  const minY = Math.min(...ys);
  const span = Math.max(Math.max(...xs) - minX, Math.max(...ys) - minY, 1);
  const scale = (size - pad * 2) / span;
  nodes.forEach((n, i) => {
    out.set(n.id, { x: pad + (pos[i]!.x - minX) * scale, y: pad + (pos[i]!.y - minY) * scale, r: radiusOf(n.examCount) });
  });
  return out;
}
