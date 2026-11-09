// 猫砂盆的纯逻辑：小图四档状态、覆层砂块位置、心情分档。
// 不读写 DOM / 存储 / 奖励，便于单测；SVG 字符串与已确认的独立原型一致。
export const LITTER_KEY = 'litter-box'
export const LITTER_MAX_STATE = 3
export const LITTER_STATE_LABELS = Object.freeze(['干干净净', '有 1 团啦', '有 2 团啦', '满啦 · 有得玩'])

// 覆层里的砂块是固定预设点，不持久化坐标；后端只需存 0-3 的档位。
export const LITTER_CLUMP_POSITIONS = Object.freeze([
  Object.freeze({ x: 152, y: 268 }),
  Object.freeze({ x: 252, y: 292 }),
  Object.freeze({ x: 342, y: 258 }),
])
export const LITTER_SCOOP_HOME = Object.freeze({ x: 300, y: 132 })
export const LITTER_BIN_ZONE = Object.freeze({ left: 534, right: 656, top: 190, bottom: 296 })
export const LITTER_BIN_TARGET = Object.freeze({ x: 590, y: 232 })
export const LITTER_PICKUP_RADIUS = 46

export function clampLitterState(n) {
  const v = Number(n)
  if (!Number.isFinite(v) || v < 0) return 0
  return Math.min(LITTER_MAX_STATE, Math.floor(v))
}

export function litterClumpPositions(n) {
  return LITTER_CLUMP_POSITIONS.slice(0, clampLitterState(n))
}

// 心情只改变猫的表现（姿势、距离、台词），不扣数值、不催促、不制造义务。
export const LITTER_MOOD_TIERS = Object.freeze({
  // 桶左沿在 540，猫宽 110：catX 必须 <= 424 才不会压住桶（窄屏缩放后同样成立）。
  good: Object.freeze({
    anim: 'auto', catX: 416, catY: 148, grayscale: false,
    start: '我帮你看着！', pick: '沙沙沙～好舒服', drop: '干净啦！喵～',
    done: '盆盆好舒服，谢谢你！', back: '还差一点点～',
  }),
  ok: Object.freeze({
    anim: 'sit', catX: 412, catY: 156, grayscale: false,
    start: '喵…慢慢来', pick: '嗯…', drop: '谢谢',
    done: '嗯，舒服多了，喵。', back: '放回砂盆里啦～',
  }),
  low: Object.freeze({
    anim: 'sleep', catX: 408, catY: 158, grayscale: true,
    start: '……（不太想动）', pick: '……', drop: '…喵',
    done: '…谢谢你，我过来啦。', back: '慢慢来就好',
  }),
})

export function litterMoodTier(mood) {
  const v = Number(mood)
  if (!Number.isFinite(v) || v < 40) return 'low'
  if (v < 70) return 'ok'
  return 'good'
}

function miniClump(cx, cy) {
  return '<path d="M' + (cx - 5) + ' ' + (cy + 2) + 'h2v-2h3v-2h3v2h2v5h-10z" fill="#B98F63" stroke="#8B6345" stroke-width="0.8"/>'
    + '<rect x="' + (cx - 2) + '" y="' + (cy - 2) + '" width="3" height="2" fill="#E0C79C"/>'
}

// 小图按档位换整张图：结团数量递增，3 团时加气味线，干净时加亮晶晶。
// 不用数字角标或红点，避免「有得玩」被读成「欠了作业」。
export function litterMiniMarkup(n) {
  const count = clampLitterState(n)
  let s = '<svg viewBox="0 0 48 40" shape-rendering="crispEdges">'
  if (count >= 3) {
    s += '<g fill="none" stroke="#C7B283" stroke-width="1.6" stroke-linecap="round">'
      + '<path d="M13 7 q3 -4 6 0"/><path d="M28 4 q3 -4 6 0"/></g>'
  }
  s += '<rect x="2" y="12" width="44" height="24" fill="#8B6345"/>'
    + '<rect x="4" y="14" width="40" height="20" fill="#C9A276"/>'
    + '<rect x="2" y="9" width="44" height="6" fill="#E0BC90"/>'
    + '<rect x="2" y="9" width="44" height="2" fill="#F2D9B0"/>'
    + '<rect x="6" y="15" width="36" height="13" fill="#EFE0BE"/>'
    + '<rect x="6" y="15" width="36" height="2" fill="#E4D0A6"/>'
    + '<g fill="#D8BF95"><rect x="9" y="20" width="2" height="2"/><rect x="19" y="25" width="2" height="2"/>'
    + '<rect x="29" y="19" width="2" height="2"/><rect x="38" y="24" width="2" height="2"/></g>'
  if (count === 0) {
    s += '<path d="M39 2h2v3h3v2h-3v3h-2V7h-3V5h3z" fill="#FFFFFF" opacity=".95"/>'
  } else if (count === 1) {
    s += miniClump(24, 22)
  } else if (count === 2) {
    s += miniClump(15, 22) + miniClump(32, 22)
  } else {
    s += miniClump(12, 21) + miniClump(24, 24) + miniClump(35, 20)
  }
  s += '<rect x="2" y="29" width="44" height="7" fill="#A47B55"/>'
    + '<rect x="4" y="30" width="40" height="2" fill="#D4A875"/>'
    + '</svg>'
  return s
}

export function litterClumpMarkup() {
  return '<svg viewBox="0 0 44 40" shape-rendering="crispEdges">'
    + '<ellipse cx="22" cy="34" rx="16" ry="3" fill="rgba(90,62,38,.16)"/>'
    + '<path d="M8 12h6V8h8v4h8V6h6v6h4v14h-4v6H12v-6H6V16h2z" fill="#C9A87A" stroke="#A8815A" stroke-width="2"/>'
    + '<rect x="12" y="14" width="5" height="4" fill="#DCBE94"/><rect x="26" y="18" width="4" height="4" fill="#B38F63"/>'
    + '<rect x="18" y="24" width="5" height="4" fill="#B38F63"/><rect x="30" y="10" width="4" height="3" fill="#DCBE94"/>'
    + '</svg>'
}

export function inLitterBin(p) {
  const z = LITTER_BIN_ZONE
  return p.x >= z.left && p.x <= z.right && p.y >= z.top && p.y <= z.bottom
}
