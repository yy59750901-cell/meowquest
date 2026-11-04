// 房间脚点坐标，不读写 DOM、存储或奖励数据。
export const CAT_TREE_SIZE = 192
export const TREE_CAT_SIZE = 64
export const TREE_SPRITE_UNITS = 48
export const GROUND_CAT_FOOT_GAP = 100 - Math.round(110 * .75)
// 48x48 像素画布上的四个互动点；世界坐标由底锚点 (24,48) 换算。
const SPRITE_NODES = Object.freeze({ grip: [11, 43], nest: [33, 37], mid: [14, 23], top: [33, 10] })
export const TREE_NODES = Object.freeze(Object.fromEntries(Object.entries(SPRITE_NODES)
  .map(([key, [x, y]]) => [key, Object.freeze({ x: (x - 24) * 4, y: (y - 48) * 4 })])))
const routes = { grip: ['grip'], nest: ['grip', 'nest'], mid: ['grip', 'mid'], top: ['grip', 'mid', 'top'] }
// 点击爬架的位置分区：上半平台 / 左侧中层 / 左下麻绳柱 / 右下小窝。
export const TREE_HOT_ZONES = Object.freeze([
  Object.freeze({ node: 'top', x1: 20, y1: 0, x2: 48, y2: 18 }),
  Object.freeze({ node: 'mid', x1: 0, y1: 18, x2: 26, y2: 30 }),
  Object.freeze({ node: 'grip', x1: 0, y1: 30, x2: 24, y2: 48 }),
  Object.freeze({ node: 'nest', x1: 24, y1: 30, x2: 48, y2: 48 }),
].map(zone => Object.freeze(zone)))
const clamp = (v, lo, hi) => Math.max(lo, Math.min(hi, v))

export function treeRoute(mode = 'top') { return [...(routes[mode] || routes.top)] }
export function treeDownRoute(node) {
  if (node === 'top') return ['mid', 'grip']
  if (node === 'mid' || node === 'nest') return ['grip']
  return []
}
export function treePoint(anchor, node) {
  const offset = TREE_NODES[node]
  return { x: anchor.x + offset.x, y: anchor.y + offset.y }
}
export function treeGroundExit(anchor, side = -1) {
  return { x: clamp(anchor.x + (side < 0 ? -110 : 110), 70, 1190), y: clamp(anchor.y, 720 * .62, 700) }
}
export function treeEntryReason({ adopted, owned, placed, busy }) {
  if (!adopted) return '先领养一只小猫吧'
  if (busy) return '先完成正在进行的操作，再和猫玩吧'
  if (!owned) return '先在商店解锁猫爬架吧'
  if (!placed) return '先把猫爬架摆进房间，再点它和小猫互动吧'
  return ''
}
// 全屏爬高游戏只认购买，不要求房间里摆着爬架；房间互动仍走 treeEntryReason。
export function gameEntryReason({ adopted, owned, busy }) {
  if (!adopted) return '先领养一只小猫吧'
  if (busy) return '先完成正在进行的操作，再和猫玩吧'
  if (!owned) return '先在商店解锁猫爬架吧'
  return ''
}
// 输入是 48x48 画布的局部坐标；越界先夹取，未命中分区时归到最近的互动点。
export function treeHitNode(x, y) {
  const px = clamp(Number.isFinite(x) ? x : 24, 0, TREE_SPRITE_UNITS)
  const py = clamp(Number.isFinite(y) ? y : 24, 0, TREE_SPRITE_UNITS)
  const zone = TREE_HOT_ZONES.find(z => px >= z.x1 && px < z.x2 && py >= z.y1 && py < z.y2)
  if (zone) return zone.node
  let nearest = 'top', best = Infinity
  for (const [node, [nx, ny]] of Object.entries(SPRITE_NODES)) {
    const dist = (px - nx) ** 2 + (py - ny) ** 2
    if (dist < best) { best = dist; nearest = node }
  }
  return nearest
}
export function treeAnchorMatches(anchor, items) {
  return !!anchor && items.some(it => it.id === anchor.id && it.item_key === 'cat-tree' && it.x === anchor.x && it.y === anchor.y)
}
export function treeVisibleBase({ left, top, width, height }) {
  const x = clamp(left + width / 2, 96, 1184)
  const low = Math.max(720 * .62, top + CAT_TREE_SIZE)
  const high = Math.min(700, top + height)
  return { x, y: low <= high ? clamp(top + height * .78, low, high) : clamp(top + height / 2 + 96, 720 * .62, 700) }
}
export function treeHopPoint(from, to, progress, lift = 14) {
  const t = clamp(progress, 0, 1)
  return { x: from.x + (to.x - from.x) * t, y: from.y + (to.y - from.y) * t - Math.sin(Math.PI * t) * lift }
}
