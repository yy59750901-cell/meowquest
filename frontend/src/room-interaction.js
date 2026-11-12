// 房间道具互动的纯逻辑：无向节点图最短路径 + 可复用的会话编排参数。
// 不读写 DOM、存储、奖励；随机函数可注入，便于单测确定性。

export function buildAdjacency(edges) {
  const adj = new Map()
  for (const [a, b] of edges) {
    if (!adj.has(a)) adj.set(a, [])
    if (!adj.has(b)) adj.set(b, [])
    if (!adj.get(a).includes(b)) adj.get(a).push(b)
    if (!adj.get(b).includes(a)) adj.get(b).push(a)
  }
  return adj
}

// 返回不含起点、含终点的节点序列；同点返回 []，不可达返回 null。
// 任何有多个互动点的道具只要给一份 edges 就能复用，不必改调度逻辑。
export function shortestNodePath(edges, from, to) {
  if (!from || !to) return null
  if (from === to) return []
  const adj = buildAdjacency(edges)
  if (!adj.has(from) || !adj.has(to)) return null
  const queue = [[from]]
  const seen = new Set([from])
  while (queue.length) {
    const path = queue.shift()
    for (const next of adj.get(path.at(-1)) || []) {
      if (seen.has(next)) continue
      const candidate = path.concat(next)
      if (next === to) return candidate.slice(1)
      seen.add(next)
      queue.push(candidate)
    }
  }
  return null
}

const unit = (random) => Math.max(0, Math.min(0.999999, Number(random()) || 0))
const bounded = (range, random) => {
  const [lo, hi] = range
  return hi <= lo ? lo : lo + Math.floor(unit(random) * (hi - lo + 1))
}

// 各道具的互动点；单节点道具也能接入同一套会话调度（只是没有路径可走）。
export const ROOM_INTERACTION_NODES = Object.freeze({
  'cat-tree': Object.freeze(['grip', 'mid', 'top', 'nest']),
  nest: Object.freeze(['nest']),
  yarn: Object.freeze(['yarn']),
  plant: Object.freeze(['plant']),
})

// 次数与停留时长都有上下界，避免孩子觉得猫卡住不动或点不动。
// 睡觉类用 longDwell：一次长停留而不是连续蹦几次。
export const ROOM_INTERACTION_PROFILES = Object.freeze({
  'cat-tree': Object.freeze({ repeats: [2, 4], dwell: [2500, 5000], longDwell: [6000, 12000], maxDuration: 25000 }),
  nest: Object.freeze({ repeats: [1, 1], dwell: [6000, 12000], longDwell: [6000, 12000], maxDuration: 25000 }),
  yarn: Object.freeze({ repeats: [2, 3], dwell: [1500, 3000], longDwell: [1500, 3000], maxDuration: 25000 }),
  plant: Object.freeze({ repeats: [1, 1], dwell: [2200, 3200], longDwell: [2200, 3200], maxDuration: 25000 }),
})

export function planInteractionSession(key, random = Math.random) {
  const profile = ROOM_INTERACTION_PROFILES[key]
  if (!profile) return null
  return Object.freeze({
    repeats: bounded(profile.repeats, random),
    dwell: bounded(profile.dwell, random),
    longDwell: bounded(profile.longDwell, random),
    maxDuration: profile.maxDuration,
  })
}

// 自动连玩时随机挑一个不同的互动点；单节点道具没有下一个目标。
export function pickInteractionNode(key, current, random = Math.random) {
  const pool = (ROOM_INTERACTION_NODES[key] || []).filter(node => node !== current)
  if (!pool.length) return null
  return pool[Math.floor(unit(random) * pool.length)]
}

export function dwellForNode(plan, node) {
  return node === 'nest' ? plan.longDwell : plan.dwell
}

export const SOCIAL_BOUNDS = Object.freeze({ left: 70, right: 1190, top: 446.4, bottom: 700 })
export function socialActorEligible(actor) {
  return actor.hunger >= 30 && ['sit', 'idle', 'auto'].includes(actor.anim)
    && !actor.walking && !actor.sleeping && !actor.occupied && !actor.chat && !actor.social
}
export function socialPair(ids, retained = null, counts = new Map()) {
  const available = [...new Set(ids.map(String))]
  if (available.length < 2) return []
  const first = retained && available.includes(String(retained)) ? String(retained)
    : available.slice().sort((a, b) => (counts.get(a) || 0) - (counts.get(b) || 0))[0]
  const second = available.filter(id => id !== first).sort((a, b) => (counts.get(a) || 0) - (counts.get(b) || 0))[0]
  return [first, second]
}
const socialInside = (p, r) => p.x >= r.left && p.x <= r.right && p.y >= r.top && p.y <= r.bottom
export function socialSegmentBlocked(a, b, rect) {
  let lo = 0, hi = 1
  for (const [axis, min, max] of [['x', rect.left, rect.right], ['y', rect.top, rect.bottom]]) {
    const delta = b[axis] - a[axis]
    if (Math.abs(delta) < 1e-9) { if (a[axis] < min || a[axis] > max) return false }
    else {
      const t1 = (min - a[axis]) / delta, t2 = (max - a[axis]) / delta
      lo = Math.max(lo, Math.min(t1, t2)); hi = Math.min(hi, Math.max(t1, t2))
      if (lo > hi) return false
    }
  }
  return lo <= 1 && hi >= 0
}
export function socialObstacles(items, art, excludeID) {
  return items.filter(it => it.id !== excludeID && it.item_key !== 'yarn' && art(it.item_key).surface === 'floor').map(it => {
    const size = art(it.item_key).size
    return { left: it.x - size / 2, right: it.x + size / 2, top: it.y - Math.min(44, size / 3), bottom: it.y + 8 }
  })
}
export function socialRoute(from, to, obstacles = [], bounds = SOCIAL_BOUNDS, padding = 55) {
  const rects = obstacles.map(r => ({ left: r.left - padding, right: r.right + padding, top: r.top - 18, bottom: r.bottom + 18 }))
  const valid = p => socialInside(p, bounds) && !rects.some(r => socialInside(p, r))
  if (!valid(from) || !valid(to)) return null
  const points = [from, to]
  for (const r of rects) for (const x of [r.left - 1, r.right + 1]) for (const y of [r.top - 1, r.bottom + 1]) {
    const p = { x: Math.max(bounds.left, Math.min(bounds.right, x)), y: Math.max(bounds.top, Math.min(bounds.bottom, y)) }
    if (valid(p)) points.push(p)
  }
  const costs = points.map(() => Infinity), previous = new Map(), seen = new Set()
  costs[0] = 0
  while (seen.size < points.length) {
    let index = -1
    for (let i = 0; i < points.length; i++) if (!seen.has(i) && (index < 0 || costs[i] < costs[index])) index = i
    if (index < 0 || !Number.isFinite(costs[index])) break
    if (index === 1) {
      const route = [points[1]]; let at = 1
      while (previous.get(at) !== 0) { at = previous.get(at); route.unshift(points[at]) }
      return route
    }
    seen.add(index)
    for (let i = 0; i < points.length; i++) {
      if (seen.has(i) || rects.some(r => socialSegmentBlocked(points[index], points[i], r))) continue
      const cost = costs[index] + Math.hypot(points[i].x - points[index].x, points[i].y - points[index].y)
      if (cost < costs[i]) { costs[i] = cost; previous.set(i, index) }
    }
  }
  return null
}
// 将其他猫当作静止占位，连续线段保持至少110px，不只检查到达点。
export function actorFloorRoute(from, to, others = [], obstacles = []) {
  const cats = others.map(p => ({ left: p.x - 109.99, right: p.x + 109.99, top: p.y - 91.99, bottom: p.y + 91.99 }))
  const furniture = obstacles.map(r => ({ left: r.left - 55, right: r.right + 55, top: r.top, bottom: r.bottom }))
  const blocked = [...furniture, ...cats]
  const direct = socialRoute(from, to, blocked, SOCIAL_BOUNDS, 0)
  if (direct) return direct
  const expanded = blocked.map(r => ({ ...r, top: r.top - 18, bottom: r.bottom + 18 }))
  const own = expanded.slice(0, furniture.length).filter(r => socialInside(from, r))
  if (!own.length || !socialInside(from, SOCIAL_BOUNDS) || obstacles.some(r => socialInside(from, r))
    || expanded.slice(furniture.length).some(r => socialInside(from, r))) return null
  // 只允许从家具实体外的安全间隙短撤离；不得跨实体、其他家具或任何猫占位。
  const candidates = own.flatMap(r => [{ x: r.left - 1, y: from.y }, { x: r.right + 1, y: from.y },
    { x: from.x, y: r.top - 1 }, { x: from.x, y: r.bottom + 1 }])
  let best = null, bestCost = Infinity
  for (const exit of candidates) {
    if (Math.hypot(exit.x - from.x, exit.y - from.y) > 56 || !socialInside(exit, SOCIAL_BOUNDS)
      || expanded.some(r => socialInside(exit, r)) || obstacles.some(r => socialSegmentBlocked(from, exit, r))
      || expanded.filter(r => !own.includes(r)).some(r => socialSegmentBlocked(from, exit, r))) continue
    const route = socialRoute(exit, to, blocked, SOCIAL_BOUNDS, 0)
    if (!route) continue
    let cost = Math.hypot(exit.x - from.x, exit.y - from.y), previous = exit
    for (const point of route) { cost += Math.hypot(point.x - previous.x, point.y - previous.y); previous = point }
    if (cost < bestCost) { best = [exit, ...route]; bestCost = cost }
  }
  return best
}
export function planActorSeats(actors, seats, obstacles = [], stationary = []) {
  if (actors.length !== seats.length) return null
  const preferred = actors.map((a, i) => i).sort((i, j) => actors[i].x - actors[j].x || actors[j].y - actors[i].y)
  const assignments = []
  function assign(indices, remaining, assigned) {
    if (!indices.length) { assignments.push(assigned); return }
    for (const seat of remaining.filter(i => !actors[indices[0]].allowedSeats || actors[indices[0]].allowedSeats.includes(i))) {
      assign(indices.slice(1), remaining.filter(i => i !== seat), { ...assigned, [indices[0]]: seat })
    }
  }
  assign(preferred, seats.map((_, i) => i), {})
  for (const assignment of assignments) {
    function move(remaining, positions, moves) {
      if (!remaining.length) return moves
      // 占目标位置的猫先移动；仍把全部active成员保留为障碍，避免假可达。
      for (const index of remaining) {
        const seat = assignment[index], target = seats[seat]
        const others = positions.filter((_, i) => i !== index).concat(stationary)
        const route = actorFloorRoute(positions[index], target, others, obstacles)
        if (!route) continue
        const next = positions.map((p, i) => i === index ? { ...target } : p)
        const result = move(remaining.filter(i => i !== index), next, moves.concat({ id: actors[index].id, seat, route }))
        if (result) return result
      }
      return null
    }
    const moves = move(actors.map((_, i) => i), actors.map(a => ({ x: a.x, y: a.y })), [])
    if (moves) return { moves, seats: actors.map((actor, i) => ({ id: actor.id, seat: assignment[i] })) }
  }
  return null
}
export function socialStageCandidates(preferred, halfSpan) {
  const bounds = { ...SOCIAL_BOUNDS, left: 70 + halfSpan, right: 1190 - halfSpan }
  const xs = [...new Set([preferred.x, 720, 920, 320, 1080, 200].map(x => Math.max(bounds.left, Math.min(bounds.right, x))))]
  const ys = [...new Set([preferred.y, 680, 620, 560, 480, 447.4].map(y => Math.max(bounds.top, Math.min(bounds.bottom, y))))]
  return ys.flatMap(y => xs.map(x => ({ x, y })))
}
export function planChaseEntry(actors, preferred, obstacles = [], stationary = []) {
  const resting = stationary.map(p => ({ left: p.x - 55, right: p.x + 55, top: p.y - 92, bottom: p.y + 92 }))
  const candidates = socialStageCandidates(preferred, 64).sort((a, b) => Math.hypot(a.x - preferred.x, a.y - preferred.y) - Math.hypot(b.x - preferred.x, b.y - preferred.y))
  for (const stage of candidates) {
    const directions = stage.x < 1000 ? [1, -1] : [-1, 1]
    const direction = directions.find(dir => socialFormationRoute(stage, { x: stage.x + dir * 74, y: stage.y }, 64, [...obstacles, ...resting]))
    if (!direction) continue
    const plan = planActorSeats(actors, [{ x: stage.x - 64, y: stage.y }, { x: stage.x + 64, y: stage.y }], obstacles, stationary)
    if (plan) return { ...plan, stage, direction }
  }
  return null
}
export function floorFurnitureSnapshot(items, art) {
  return items.filter(it => it.item_key !== 'yarn' && art(it.item_key).surface === 'floor')
    .map(it => `${it.id}:${it.item_key}:${it.x}:${it.y}`).sort().join('|')
}
export function socialStage(preferred, halfSpan, obstacles = [], travel = 0) {
  const bounds = { ...SOCIAL_BOUNDS, left: 70 + halfSpan, right: 1190 - halfSpan }
  const xs = [preferred.x, 720, 920, 320, 1080, 200].map(x => Math.max(bounds.left, Math.min(bounds.right, x)))
  const ys = [preferred.y, 680, 620, 560, 480, 447.4].map(y => Math.max(bounds.top, Math.min(bounds.bottom, y)))
  const valid = point => socialRoute(point, point, obstacles, bounds, halfSpan + 55)
    && (!travel || [-1, 1].some(dir => socialRoute(point, { x: point.x + dir * travel, y: point.y }, obstacles, bounds, halfSpan + 55)))
  // 追逐先找附近纵向空地，并确认一小段追逐可走，而非邀请后才发现两头堵死。
  if (travel) for (const y of ys) { const point = { x: xs[0], y }; if (valid(point)) return point }
  for (const y of ys) for (const x of xs) {
    const point = { x, y }; if (valid(point)) return point
  }
  return null
}
export function socialFormationRoute(from, to, halfSpan, obstacles = []) {
  return socialRoute(from, to, obstacles, { ...SOCIAL_BOUNDS, left: 70 + halfSpan, right: 1190 - halfSpan }, halfSpan + 55)
}
export function yarnPawContact(cat, facingRight) {
  // 原 catchB 左前爪像素 (2,10) 的实际 1.02 方块中心；球外轮廓侧下方接触爪。
  const direction = facingRight ? 1 : -1
  const paw = { x: cat.x + direction * (55 - 2.51 * 6.875), y: cat.y - 100 + .25 + 10.51 * 6.875 }
  return { paw, ball: { x: paw.x + direction * 16, y: paw.y + 12.5 } }
}
