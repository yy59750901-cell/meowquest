// 毛球合成：纯逻辑 + 简易圆形物理（不依赖 DOM/Vue，固定步长，seed 随机，便于单测）。
// 规则：猫在顶部左右移动，把毛线球丢进盒子；两个同级毛线球碰到一起合成更大一级；
// 球堆到危险线以上并停留太久，这一局结束。没有计时、不花星星、不产星星、不存成绩。

export const YARN_CONFIG = {
  width: 360,
  height: 540,
  lineY: 120, // 危险线
  spawnY: 84, // 丢球出生点（线上方，猫手里）
  radii: [15, 21, 28, 36, 45, 56, 69, 83],
  names: ['线头', '小毛球', '毛线球', '大毛球', '线团', '毛线篮', '小花猫', '大花猫'],
  spawnPool: [0, 0, 0, 1, 1, 1, 2, 2, 3], // 丢出的球只会是前几级
  gravity: 1500,
  step: 1 / 120,
  maxFrame: 0.05,
  iterations: 4,
  restitution: 0.12,
  friction: 0.45,
  floorDamp: 0.985,
  airDamp: 0.9995,
  correction: 0.8,
  restSpeed: 14, // 低于这个速度视为快停了，逐步刹住，避免球堆永远在慢慢爬
  restDamp: 0.9,
  dropCooldown: 0.45,
  graceAge: 1.0, // 新球落下前不算越线
  mergedGrace: 0.6, // 合成出的新球先给一点宽限
  dangerSpeed: 80,
  dangerLimit: 2.0, // 越线并停留这么久（秒）结束
  aimStep: 14,
}
export const YARN_MAX = YARN_CONFIG.radii.length - 1

const clamp = (v, lo, hi) => Math.max(lo, Math.min(hi, v))

function mulberry32(seed) {
  let a = seed >>> 0
  return () => {
    a = (a + 0x6d2b79f5) >>> 0
    let t = a
    t = Math.imul(t ^ (t >>> 15), t | 1)
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61)
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

// 合成得分：新球等级越高越多；两个最高级相撞（消失）给一份大奖励。
export const yarnMergeScore = lv => (lv + 1) * (lv + 2) * 5

export function createYarnEngine({ seed = Date.now(), config = YARN_CONFIG, balls = [] } = {}) {
  const C = config
  const MAXLV = C.radii.length - 1
  let rng = mulberry32(seed)
  let nextId = 1
  let acc = 0
  let s

  const pickLevel = () => C.spawnPool[Math.floor(rng() * C.spawnPool.length)]

  function fresh() {
    return {
      phase: 'playing', // playing | over
      balls: [],
      aimX: C.width / 2,
      held: pickLevel(),
      next: pickLevel(),
      cooldown: 0,
      score: 0,
      best: -1, // 本局合成出的最高等级
      merges: 0,
      dangerT: 0,
      message: { id: 0, text: '左右移动，松手丢球。两个一样的毛线球碰到一起会合成！' },
      event: { id: 0, type: '', lv: -1 },
      time: 0,
    }
  }
  s = fresh()
  let msgSeq = 0, evSeq = 0
  // 仅测试/演示用：开局直接放几个球（x,y 为球心，已落稳的球 age 给足）
  for (const b of balls) addBall(b.lv, b.x, b.y, b.vx || 0, b.vy || 0, C.graceAge + 1)
  const say = text => { s.message = { id: ++msgSeq, text } }
  const emit = (type, lv) => { s.event = { id: ++evSeq, type, lv } }
  const aimLimits = lv => [C.radii[lv], C.width - C.radii[lv]]

  function addBall(lv, x, y, vx = 0, vy = 0, age = 0) {
    const b = { id: nextId++, lv, r: C.radii[lv], x, y, vx, vy, age, m: false, dead: false }
    s.balls.push(b)
    return b
  }

  function wall(b) {
    if (b.x < b.r) { b.x = b.r; if (b.vx < 0) b.vx = -b.vx * C.restitution }
    else if (b.x > C.width - b.r) { b.x = C.width - b.r; if (b.vx > 0) b.vx = -b.vx * C.restitution }
    if (b.y > C.height - b.r) {
      b.y = C.height - b.r
      if (b.vy > 0) b.vy = -b.vy * C.restitution
      b.vx *= C.floorDamp
    }
  }

  function solvePair(a, b) {
    const dx = b.x - a.x, dy = b.y - a.y, rr = a.r + b.r
    const d2 = dx * dx + dy * dy
    if (d2 >= rr * rr) return
    const d = Math.sqrt(d2) || 1e-4
    const nx = dx / d, ny = dy / d
    const ma = a.r * a.r, mb = b.r * b.r, mt = ma + mb
    const corr = (rr - d) * C.correction
    a.x -= nx * corr * (mb / mt); a.y -= ny * corr * (mb / mt)
    b.x += nx * corr * (ma / mt); b.y += ny * corr * (ma / mt)
    const rvx = b.vx - a.vx, rvy = b.vy - a.vy
    const vn = rvx * nx + rvy * ny
    if (vn >= 0) return
    const inv = 1 / ma + 1 / mb
    const jn = -(1 + C.restitution) * vn / inv
    a.vx -= nx * jn / ma; a.vy -= ny * jn / ma
    b.vx += nx * jn / mb; b.vy += ny * jn / mb
    // 切向摩擦：球堆才不会一直滑动
    const tx = rvx - vn * nx, ty = rvy - vn * ny
    const tl = Math.hypot(tx, ty)
    if (tl > 1e-6) {
      const jt = Math.min(tl / inv, C.friction * jn)
      const ux = tx / tl, uy = ty / tl
      a.vx += ux * jt / ma; a.vy += uy * jt / ma
      b.vx -= ux * jt / mb; b.vy -= uy * jt / mb
    }
  }

  function doMerges(pairs) {
    for (const [a, b] of pairs) {
      if (a.dead || b.dead) continue
      a.dead = b.dead = true
      if (a.lv >= MAXLV) {
        s.score += yarnMergeScore(MAXLV) * 2
        s.merges++
        say('两只大花猫碰到一起，开心地跑掉了！腾出了好多空间～')
        emit('clear', MAXLV)
        continue
      }
      const lv = a.lv + 1, ma = a.r * a.r, mb = b.r * b.r, mt = ma + mb
      const nb = addBall(lv, (a.x * ma + b.x * mb) / mt, (a.y * ma + b.y * mb) / mt,
        (a.vx * ma + b.vx * mb) / mt, (a.vy * ma + b.vy * mb) / mt, C.graceAge - C.mergedGrace)
      wall(nb)
      s.score += yarnMergeScore(lv)
      s.merges++
      if (lv > s.best) {
        s.best = lv
        if (lv === MAXLV) { say('大花猫出现啦！太厉害了！'); emit('top', lv) }
        else { say(`合成了「${C.names[lv]}」！`); emit('merge', lv) }
      } else emit('merge', lv)
    }
    s.balls = s.balls.filter(b => !b.dead)
  }

  function physicsStep(h) {
    s.time += h
    if (s.cooldown > 0) s.cooldown = Math.max(0, s.cooldown - h)
    for (const b of s.balls) {
      b.vy += C.gravity * h
      b.vx *= C.airDamp; b.vy *= C.airDamp
      if (b.vx * b.vx + b.vy * b.vy < C.restSpeed * C.restSpeed) { b.vx *= C.restDamp; b.vy *= C.restDamp }
      b.x += b.vx * h; b.y += b.vy * h
      b.age += h
      wall(b)
    }
    const pairs = []
    const n = s.balls.length
    for (let it = 0; it < C.iterations; it++) {
      for (let i = 0; i < n; i++) {
        const a = s.balls[i]
        for (let j = i + 1; j < n; j++) {
          const b = s.balls[j]
          const dx = b.x - a.x, dy = b.y - a.y, rr = a.r + b.r
          if (dx * dx + dy * dy >= rr * rr) continue
          if (a.lv === b.lv) {
            if (!a.m && !b.m) { a.m = b.m = true; pairs.push([a, b]) }
            if (a.m && b.m && pairs.some(p => (p[0] === a && p[1] === b))) continue
          }
          solvePair(a, b)
        }
      }
      for (const b of s.balls) wall(b)
    }
    if (pairs.length) doMerges(pairs)
    // 越线判定：落稳了的球压在危险线以上
    let over = false
    for (const b of s.balls) {
      if (b.age > C.graceAge && b.y - b.r < C.lineY && Math.hypot(b.vx, b.vy) < C.dangerSpeed) { over = true; break }
    }
    s.dangerT = over ? s.dangerT + h : Math.max(0, s.dangerT - h * 2)
    if (s.dangerT >= C.dangerLimit) {
      s.phase = 'over'
      say(s.best >= 0 ? `毛线球堆满啦！这局最高合成到「${C.names[s.best]}」，再来一局吧～` : '毛线球堆满啦，再来一局吧～')
      emit('over', s.best)
    }
  }

  function dispatch(e) {
    if (e.type === 'aim') {
      const [lo, hi] = aimLimits(s.held)
      s.aimX = clamp(e.x, lo, hi)
    } else if (e.type === 'nudge') {
      const [lo, hi] = aimLimits(s.held)
      s.aimX = clamp(s.aimX + e.dir * C.aimStep, lo, hi)
    } else if (e.type === 'drop') {
      if (s.phase !== 'playing' || s.cooldown > 0) return snapshot()
      const [lo, hi] = aimLimits(s.held)
      addBall(s.held, clamp(e.x ?? s.aimX, lo, hi), C.spawnY)
      s.held = s.next; s.next = pickLevel()
      s.cooldown = C.dropCooldown
      const [l2, h2] = aimLimits(s.held)
      s.aimX = clamp(s.aimX, l2, h2)
      emit('drop', s.held)
    } else if (e.type === 'restart') {
      rng = mulberry32(e.seed ?? Math.floor(rng() * 4294967296))
      const keepMsg = msgSeq
      s = fresh(); acc = 0
      s.message = { id: keepMsg + 1, text: '新的一局！左右移动，松手丢球。' }; msgSeq = keepMsg + 1
      emit('restart', -1)
    }
    return snapshot()
  }

  function tick(dt) {
    if (s.phase !== 'playing') return snapshot()
    acc += clamp(dt, 0, C.maxFrame)
    while (acc >= C.step && s.phase === 'playing') { physicsStep(C.step); acc -= C.step }
    return snapshot()
  }

  function snapshot() {
    return {
      phase: s.phase,
      balls: s.balls.map(b => ({ id: b.id, lv: b.lv, r: b.r, x: b.x, y: b.y, vx: b.vx, vy: b.vy })),
      aimX: s.aimX, held: s.held, next: s.next,
      cooldown: s.cooldown, score: s.score, best: s.best, merges: s.merges,
      danger: clamp(s.dangerT / C.dangerLimit, 0, 1),
      message: s.message, event: s.event,
    }
  }

  return { dispatch, tick, snapshot }
}
