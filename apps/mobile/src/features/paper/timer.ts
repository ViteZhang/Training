/** 按题累计本机作答时长；同步成功才确认增量，断网期间保留未同步秒数。 */
export class PaperTimer {
  private elapsed: Record<number, number>;
  private sent: Record<number, number>;
  private at: number;
  constructor(items: { seq: number; time_spent_seconds: number }[], private seq: number, private running = true, private now = Date.now) {
    this.elapsed = Object.fromEntries(items.map((it) => [it.seq, it.time_spent_seconds * 1000]));
    this.sent = Object.fromEntries(items.map((it) => [it.seq, it.time_spent_seconds]));
    this.at = now();
  }
  private collect() {
    const at = this.now();
    if (this.running) this.elapsed[this.seq] = (this.elapsed[this.seq] ?? 0) + Math.max(0, at - this.at);
    this.at = at;
  }
  select(seq: number) { this.collect(); this.seq = seq; }
  setRunning(running: boolean) { this.collect(); this.running = running; }
  seconds(seq: number) { this.collect(); return Math.floor((this.elapsed[seq] ?? 0) / 1000); }
  pending(seq: number, throughSeconds = this.seconds(seq)) { return Math.max(0, throughSeconds - (this.sent[seq] ?? 0)); }
  acknowledge(seq: number, delta: number) { this.sent[seq] = (this.sent[seq] ?? 0) + delta; }
}
