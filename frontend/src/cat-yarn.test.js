import test from 'node:test'
import assert from 'node:assert/strict'
import { YARN_CONFIG as C, YARN_MAX, createYarnEngine, yarnMergeScore } from './cat-yarn.js'

function run(e, sec) { let r = sec; while (r > 1e-9) { const st = Math.min(1 / 60, r); e.tick(st); r -= st } return e.snapshot() }
function drop(e, x) { e.dispatch({ type: 'aim', x }); return e.dispatch({ type: 'drop' }) }
// 绕开随机：直接让引擎丢出指定等级（通过 seed 搜索不可靠，用内部白盒：连丢直到 held 命中）
function dropLevel(e, lv, x) {
  for (let i = 0; i < 400; i++) {
    const s = e.snapshot()
    if (s.cooldown > 0) { run(e, 0.1); continue }
    if (s.held === lv) return drop(e, x)
    // 不是想要的等级：丢到远处角落，让它不干扰
    drop(e, i % 2 ? 14 : 346)
    run(e, 0.6)
  }
  throw new Error('取不到等级 ' + lv)
}
const valid = s => s.balls.every(b => Number.isFinite(b.x) && Number.isFinite(b.y) && Number.isFinite(b.vx) && Number.isFinite(b.vy))
const inBox = s => s.balls.every(b => b.x >= b.r - 0.01 && b.x <= C.width - b.r + 0.01 && b.y <= C.height - b.r + 0.01)
function overlap(s) {
  let worst = 0
  for (let i = 0; i < s.balls.length; i++) for (let j = i + 1; j < s.balls.length; j++) {
    const a = s.balls[i], b = s.balls[j]; worst = Math.max(worst, a.r + b.r - Math.hypot(a.x - b.x, a.y - b.y))
  }
  return worst
}

test('配置：半径递增，最大球放得进盒子，危险线在出生点下方', () => {
  for (let i = 1; i < C.radii.length; i++) assert.ok(C.radii[i] > C.radii[i - 1])
  assert.ok(C.radii[YARN_MAX] * 2 < C.width * 0.5, '最大球直径小于盒宽一半')
  assert.ok(C.spawnY < C.lineY && C.lineY < C.height)
  assert.equal(C.names.length, C.radii.length)
  assert.ok(Math.max(...C.spawnPool) <= 3, '只会丢出前几级')
})

test('丢出的球会落到盒底并静止，不穿透、不乱飘', () => {
  const e = createYarnEngine({ seed: 1 })
  drop(e, 180)
  const s = run(e, 3)
  assert.equal(s.balls.length, 1)
  const b = s.balls[0]
  assert.ok(Math.abs(b.y - (C.height - b.r)) < 0.5, `落在盒底，实际 y=${b.y}`)
  assert.ok(Math.hypot(b.vx, b.vy) < 5, '已静止')
})

test('丢球有冷却，冷却中再丢无效；球不会被丢到盒子外', () => {
  const e = createYarnEngine({ seed: 2 })
  drop(e, 10)
  assert.equal(e.snapshot().balls.length, 1)
  e.dispatch({ type: 'drop' })
  assert.equal(e.snapshot().balls.length, 1, '冷却中不能连丢')
  run(e, C.dropCooldown + 0.05)
  e.dispatch({ type: 'aim', x: -999 }); e.dispatch({ type: 'drop' })
  const s = run(e, 1)
  assert.ok(inBox(s))
  e.dispatch({ type: 'aim', x: 99999 }); assert.ok(e.snapshot().aimX <= C.width)
})

test('两个同级相碰合成高一级，得分按等级给，其余不变', () => {
  const e = createYarnEngine({ seed: 3 })
  dropLevel(e, 0, 180); run(e, 0.5)
  dropLevel(e, 0, 182)
  const s = run(e, 2.5)
  const lv1 = s.balls.filter(b => b.lv === 1)
  assert.ok(s.merges >= 1 && lv1.length >= 1, '合成出 1 级')
  assert.ok(s.score >= yarnMergeScore(1))
  assert.ok(s.best >= 1)
  assert.equal(s.event.type === 'merge' || s.event.type === 'drop', true)
})

test('不同级相碰不合成', () => {
  const e = createYarnEngine({ seed: 4, balls: [{ lv: 1, x: 180, y: 520 }, { lv: 2, x: 200, y: 480 }] })
  const s = run(e, 2.5)
  assert.equal(s.merges, 0)
  assert.deepEqual(s.balls.map(b => b.lv).sort(), [1, 2])
})

test('三个同级挤在一起：每个球只参与一次合成，不丢球、不重复 id', () => {
  const e = createYarnEngine({ seed: 5, balls: [{ lv: 0, x: 170, y: 530 }, { lv: 0, x: 195, y: 530 }, { lv: 0, x: 183, y: 502 }] })
  const s = run(e, 3)
  assert.ok(valid(s))
  assert.equal(new Set(s.balls.map(b => b.id)).size, s.balls.length)
  assert.ok(s.merges >= 1)
  // 三个 0 级共 3 份“质量”：最终等级和不会凭空多出
  const total = s.balls.reduce((n, b) => n + 2 ** b.lv, 0)
  assert.ok(total <= 3, `质量守恒：2^lv 之和应不超过 3，实际 ${total}`)
})

test('链式合成：合成出的球马上碰到同级邻居，会继续合成', () => {
  const r1 = C.radii[1], r2 = C.radii[2], y = C.height - r2
  // 两个 1 级贴在一起合成为 2 级，位置在它们中点，正好压在旁边的 2 级上
  const e = createYarnEngine({ seed: 5, balls: [
    { lv: 1, x: 150, y: C.height - r1 }, { lv: 1, x: 150 + 2 * r1 - 4, y: C.height - r1 },
    { lv: 2, x: 185, y }, // 紧挨着两个 1 级的合成点，合成出的 2 级会立刻碰到它
  ] })
  const s = run(e, 3)
  assert.ok(s.best >= 3, `应链式合成到 3 级，实际最高 ${s.best}，球 ${s.balls.map(b => b.lv)}`)
  assert.ok(s.balls.some(b => b.lv === 3))
})

test('随机连续丢 60 个球：数值有限、都在盒内、重叠很小', () => {
  const e = createYarnEngine({ seed: 6 })
  let rng = 12345
  for (let i = 0; i < 60; i++) {
    rng = (rng * 1103515245 + 12345) >>> 0
    drop(e, 20 + (rng % 320)); run(e, 0.6)
    if (e.snapshot().phase === 'over') break
  }
  const s = run(e, 3)
  assert.ok(valid(s)); assert.ok(inBox(s))
  assert.ok(overlap(s) < 6, `重叠过大 ${overlap(s)}`)
})

test('球堆稳定：停手后不抖动、不无故移动', () => {
  const e = createYarnEngine({ seed: 7 })
  for (let i = 0; i < 14; i++) { drop(e, 40 + (i * 53) % 280); run(e, 0.7); if (e.snapshot().phase === 'over') break }
  run(e, 5)
  const a = e.snapshot(); run(e, 1.5); const b = e.snapshot()
  const byId = new Map(a.balls.map(x => [x.id, x]))
  let drift = 0
  for (const q of b.balls) { const p = byId.get(q.id); if (p) drift = Math.max(drift, Math.hypot(q.x - p.x, q.y - p.y)) }
  assert.ok(drift < 2, `静止后 1.5 秒位移 ${drift}`)
})

test('堆过危险线并停留会结束；结束后不能再丢；重开恢复', () => {
  const e = createYarnEngine({ seed: 8 })
  let n = 0
  while (e.snapshot().phase === 'playing' && n < 400) { drop(e, 180 + (n % 5) * 3); run(e, 0.6); n++ }
  const s = e.snapshot()
  assert.equal(s.phase, 'over', '一直往同一处堆总会满')
  assert.match(s.message.text, /堆满/)
  const cnt = s.balls.length
  e.dispatch({ type: 'drop' }); assert.equal(e.snapshot().balls.length, cnt)
  e.dispatch({ type: 'restart', seed: 9 })
  const r = e.snapshot()
  assert.equal(r.phase, 'playing'); assert.equal(r.balls.length, 0); assert.equal(r.score, 0); assert.equal(r.best, -1)
})

test('刚丢下的球飞过危险线不算越线', () => {
  const e = createYarnEngine({ seed: 10 })
  drop(e, 180); run(e, 0.3)
  assert.equal(e.snapshot().danger, 0)
  assert.equal(e.snapshot().phase, 'playing')
})

test('两个最高级相碰：一起消失并给大奖励', () => {
  const r = C.radii[YARN_MAX]
  const e = createYarnEngine({ seed: 11, balls: [{ lv: YARN_MAX, x: r + 2, y: C.height - r }, { lv: YARN_MAX, x: r + 2 + 2 * r - 6, y: C.height - r }] })
  const s = run(e, 1.5)
  assert.equal(s.balls.length, 0)
  assert.equal(s.score, yarnMergeScore(YARN_MAX) * 2)
  assert.equal(s.event.type, 'clear')
})

test('同 seed 完全确定', () => {
  const play = seed => { const e = createYarnEngine({ seed }); for (let i = 0; i < 12; i++) { drop(e, 30 + (i * 71) % 300); run(e, 0.6) } return JSON.stringify(run(e, 2).balls.map(b => [b.lv, Math.round(b.x), Math.round(b.y)])) }
  assert.equal(play(21), play(21))
  assert.notEqual(play(21), play(22))
})

test('键盘微调：按步长移动并夹在盒内', () => {
  const e = createYarnEngine({ seed: 12 })
  const x0 = e.snapshot().aimX
  e.dispatch({ type: 'nudge', dir: 1 }); assert.equal(e.snapshot().aimX, x0 + C.aimStep)
  for (let i = 0; i < 100; i++) e.dispatch({ type: 'nudge', dir: -1 })
  assert.ok(e.snapshot().aimX >= C.radii[e.snapshot().held])
})
