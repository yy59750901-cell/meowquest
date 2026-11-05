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
