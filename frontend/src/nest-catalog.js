// 15 款新猫窝确认稿参数（数据源：cat-nest-gallery-fixed/measurements.json，SHA 818a959f…）。
// 苹果窝 nest-apple 维持上一代实现，不在此表内；两套进场链路互不干扰。
// B 档（11/06）：显示尺寸整体 ×0.8（240→192）；sprite 与 opening 仍是源图像素坐标，用百分比换算，不缩放。
// 本模块只放纯数据与纯函数，方便单测直接断言 demo 的几何/时序；DOM 渲染在 KidHome.vue。
export const WORLD_W = 1280
export const CAT_W = 110
export const CAT_H = 83
export const CAT_FOOT_OFFSET = 17 // .cat-wrap margin-top:-100，脚点比猫底低 17px
export const NEST_PAD = 8 // demo .shade 的 inset:-8px
export const NEST_FLOOR_OFFSET = 60 // B 档按 0.8 等比缩放：75→60，跟随窝高 h
export const NEST_ENTRY_TIME = { walk: 900, prepare: 150, openJump: 650, doorJump: 480, land: 220 }
export const NEST_WALK_FRAME_MS = 180
export const NEST_MOTION = { anticipate: 'crouchA', takeoff: 'pounceA', entering: 'pounceB', land: 'crouchA' }

export const NEST_CATALOG = {
  'nest-lace-square': { demoKey: 'lace-square', name: '花边方窝', price: 30, kind: 'open', display: [192.0, 141.8571], sprite: [896, 662], catCenter: [105.0, 75.8571], rim: 91.288, opening: [] },
  'nest-tent': { demoKey: 'tent', name: '三角帐篷', price: 42, kind: 'cave', display: [192.0, 152.2693], sprite: [1010, 801], catCenter: [98.0911, 107.9762], opening: [[142, 650], [178, 493], [256, 364], [349, 318], [410, 278], [455, 264], [510, 266], [584, 298], [649, 369], [684, 422], [713, 519], [729, 604], [733, 671], [369, 671], [281, 654]] },
  'nest-petal-cylinder': { demoKey: 'petal-cylinder', name: '粉色花瓣圆筒窝', price: 45, kind: 'cave', display: [178.4, 199.9671], sprite: [852, 955], catCenter: [60.031, 145.9446], opening: [[49, 512], [74, 493], [114, 488], [132, 458], [158, 446], [181, 460], [229, 511], [250, 525], [301, 541], [331, 573], [348, 604], [351, 647], [338, 686], [352, 773], [335, 809], [266, 821], [209, 859], [160, 841], [128, 809], [84, 776], [59, 722], [50, 657]] },
  'nest-stripe-handle': { demoKey: 'stripe-handle', name: '条纹提手窝', price: 32, kind: 'open', display: [192.0, 129.9114], sprite: [971, 657], catCenter: [94.7147, 67.6251], rim: 91.752, opening: [] },
  'nest-knit-drop': { demoKey: 'knit-drop', name: '针织水滴窝', price: 40, kind: 'cave', display: [192.0, 196.9336], sprite: [934, 958], catCenter: [148.5019, 101.7559], opening: [[525, 404], [548, 374], [579, 352], [620, 346], [660, 361], [690, 397], [715, 439], [730, 484], [731, 586], [717, 628], [697, 647], [662, 632], [621, 606], [579, 564], [554, 526], [531, 479]] },
  'nest-dot-u': { demoKey: 'dot-U', name: '波点U形窝', price: 30, kind: 'open', display: [192.0, 115.2423], sprite: [908, 545], catCenter: [104.0353, 51.5947], rim: 58.152, opening: [] },
  'nest-quilt-u': { demoKey: 'quilt-U', name: '绗缝U形垫', price: 30, kind: 'open', display: [192.0, 128.589], sprite: [978, 655], catCenter: [88.1472, 69.6933], rim: 100.12, opening: [] },
  'nest-greyblue-round': { demoKey: 'greyblue-round', name: '灰蓝椭圆围合窝', price: 32, kind: 'open', display: [192.0, 126.8066], sprite: [1019, 673], catCenter: [97.0363, 63.3091], rim: 90.256, opening: [] },
  'nest-leaf-apple': { demoKey: 'leaf-apple', name: '带叶圆苹果', price: 42, kind: 'cave', display: [192.0, 187.0505], sprite: [931, 907], catCenter: [89.5038, 98.7841], opening: [[222, 554], [242, 470], [269, 415], [304, 373], [343, 344], [388, 319], [425, 309], [463, 323], [519, 354], [570, 395], [604, 445], [628, 499], [637, 560], [602, 575], [537, 589], [473, 593], [383, 588], [304, 573]] },
  'nest-banana': { demoKey: 'banana', name: '香蕉船', price: 34, kind: 'open', display: [192.0, 183.6912], sprite: [855, 818], catCenter: [95.6631, 110.2597], rim: 129.8, opening: [] },
  'nest-catear': { demoKey: 'catear', name: '猫耳拱形窝', price: 42, kind: 'cave', display: [192.0, 145.3628], sprite: [951, 720], catCenter: [115.6846, 93.2745], opening: [[396, 580], [391, 451], [417, 392], [448, 337], [486, 293], [529, 261], [562, 249], [599, 253], [635, 287], [672, 339], [705, 397], [725, 446], [735, 481], [637, 501], [530, 526], [448, 557]] },
  'nest-house': { demoKey: 'house', name: '绿色方屋', price: 40, kind: 'half', display: [192.0, 171.849], sprite: [848, 759], catCenter: [91.4717, 115.6981], opening: [[40, 317], [105, 289], [215, 277], [396, 279], [574, 305], [689, 343], [686, 443], [671, 510], [663, 576], [596, 615], [517, 632], [424, 620], [348, 602], [264, 591], [190, 577], [127, 562], [86, 522], [40, 512]] },
  'nest-blue-yellow': { demoKey: 'blue-yellow', name: '蓝壳黄内衬半开放窝', price: 40, kind: 'half', display: [192.0, 153.1803], sprite: [915, 730], catCenter: [96.7344, 90.859], opening: [[167, 190], [223, 107], [306, 57], [389, 30], [492, 17], [605, 53], [693, 108], [763, 185], [786, 235], [760, 318], [706, 384], [649, 453], [590, 515], [520, 579], [447, 616], [376, 566], [314, 527], [255, 461], [209, 404], [177, 339], [159, 264]] },
  'nest-red-dots': { demoKey: 'red-dots', name: '红口波点椭圆洞', price: 44, kind: 'cave', display: [192.0, 176.0518], sprite: [927, 850], catCenter: [60.479, 99.2103], opening: [[101, 565], [133, 495], [187, 433], [243, 376], [299, 321], [360, 272], [393, 263], [421, 304], [437, 357], [445, 404], [443, 455], [422, 508], [372, 551], [305, 589], [248, 609], [175, 606], [123, 590]] },
  'nest-green-monster': { demoKey: 'green-monster', name: '绿色小怪兽窝', price: 42, kind: 'cave', display: [192.0, 179.579], sprite: [912, 853], catCenter: [83.1158, 110.1137], opening: [[313, 297], [268, 303], [250, 317], [222, 347], [203, 377], [184, 417], [171, 457], [151, 497], [140, 547], [132, 587], [134, 607], [166, 623], [218, 635], [298, 640], [368, 640], [420, 637], [428, 617], [432, 559], [432, 459], [432, 379], [422, 349], [404, 319], [374, 305], [343, 297]] },
}

export const NEST_KEYS = Object.keys(NEST_CATALOG)
export const isJumpNest = key => Object.prototype.hasOwnProperty.call(NEST_CATALOG, key)
export const nestProfile = key => NEST_CATALOG[key] || null

const mix = (a, b, t) => a + (b - a) * t
const smooth = t => t * t * (3 - 2 * t)
const clamp = (v, lo, hi) => Math.max(lo, Math.min(hi, v))

// demo foot()：sleep 猫元素相对窝左上角；catCenter 锚定 sleep 可见像素中心。
export function nestLocalFoot(p) {
  return { left: p.catCenter[0] - CAT_W / 2, top: p.catCenter[1] - 58.8 }
}
// 世界脚点（catPos）：猫底比脚点高 17px，等价于旧 appleNestFoot 的 490/565 换算。
export function nestSleepPos(p, it) {
  const [w, h] = p.display
  const local = nestLocalFoot(p)
  return { x: it.x - w / 2 + local.left + CAT_W / 2,
           y: it.y - h + local.top + CAT_H + CAT_FOOT_OFFSET }
}
// demo route()：游戏世界坐标版。窝锚点 it.x/it.y 即窝底中心，窝顶 = it.y - h。
export function nestRoute(p, it) {
  const [w, h] = p.display, [, sh] = p.sprite
  const nestLeft = it.x - w / 2, nestTop = it.y - h
  const local = nestLocalFoot(p)
  const landLeft = nestLeft + local.left
  const sleepFoot = local.top + CAT_H
  const open = p.kind === 'open'
  const sill = open ? p.rim : Math.max(...p.opening.map(pt => pt[1])) / sh * h
  // 洞口款站在入口前地板，不从窝体最低角升起；上不超过窝底，避免落进地面以下。
  const groundFoot = open ? h : Math.min(h, Math.max(sill + 14, h - NEST_FLOOR_OFFSET))
  const landFoot = open ? sleepFoot : sill - 4
  const apexFoot = open ? Math.min(sill - 14, landFoot - 20) : landFoot - 8
  const direction = open ? 1 : -1
  const edge = WORLD_W - 116
  const launch = Math.max(6, Math.min(edge, landLeft - direction * (open ? 46 : 28)))
  const start = direction === 1 ? Math.max(6, launch - 120) : Math.min(edge, launch + 120)
  return { w, h, nestLeft, nestTop, landLeft, sleepFoot, sill, groundFoot, landFoot, apexFoot,
    direction, launch, start, open, jumpTime: open ? NEST_ENTRY_TIME.openJump : NEST_ENTRY_TIME.doorJump,
    groundTop: nestTop + groundFoot - CAT_H,
    // walkTo 用世界脚点：元素 left + 55，猫底 + 17 = 元素 top + 100
    groundPos: { x: start + CAT_W / 2, y: nestTop + groundFoot + CAT_FOOT_OFFSET } }
}
// demo flightPoint()：真实先升后降，不是直线上抬；60% 时爪子越过前沿。
export function nestFlightPoint(r, t) {
  const foot = t <= .6 ? mix(r.groundFoot, r.apexFoot, 1 - Math.pow(1 - t / .6, 2))
    : mix(r.apexFoot, r.landFoot, Math.pow((t - .6) / .4, 2))
  return { left: mix(r.launch, r.landLeft, smooth(t)), top: r.nestTop + foot - CAT_H, foot }
}
// demo renderEntry() 的 landing：从门槛平滑落到最终 sleep 脚点，仍用 crouchA。
export function nestLandingPoint(r, t) {
  const foot = mix(r.landFoot, r.sleepFoot, smooth(t))
  return { left: r.landLeft, top: r.nestTop + foot - CAT_H, foot }
}
// demo clip()：洞口/半开放用真实 opening 多边形裁窝内猫，开放垫不裁。
export function nestOpeningClip(p) {
  if (p.kind === 'open' || p.opening.length < 3) return 'none'
  return 'polygon(' + p.opening.map(([x, y]) =>
    `${x / p.sprite[0] * 100}% ${y / p.sprite[1] * 100}%`).join(',') + ')'
}
// demo drawActor()：洞口款内外双副本补集 clip，让头先进入；开放款 .64 越沿后整只交接。
export function nestHandoff(r, progress) {
  if (r.open) {
    const crossed = progress >= .64
    return { outerHidden: crossed, innerHidden: !crossed, outerClip: 'none', innerClip: 'none' }
  }
  const entered = clamp((progress - .58) / .34, 0, 1)
  const cut = entered * CAT_W
  return { outerHidden: entered >= 1, innerHidden: entered <= 0,
    outerClip: `inset(0 0 0 ${cut}px)`, innerClip: `inset(0 ${CAT_W - cut}px 0 0)` }
}
export const nestArt = (p, part) => `/art/nests/${p.demoKey}-${part}.png`
