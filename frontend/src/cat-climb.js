// 世界坐标向上为正；cat.y 是脚底；x 首尾相接成圆柱，取模 360。
export const CAT_CLIMB_CONFIG = Object.freeze({
  width: 360, height: 560, step: 1 / 120, maxDt: 0.1,
  jumpSpeed: 430, gravity: 1000, horizontalSpeed: 180, catHalfWidth: 12,
  startY: 56, cameraThreshold: 0.82, dropBuffer: 110,
  ahead: 560 + 2 * (430 * 430 / 2000), maxPlatforms: 60,
  minGap: 48, maxGap: 80, warmupWidth: 95, platformWidth: 60,
  movingWidth: 65, maxAmplitude: 16, minPeriod: 3.6,
  landingMargin: 4, horizontalSafety: 10, difficultyEvery: 5, difficultyMaxLevel: 4,
  coyote: 0.1, jumpBuffer: 0.1, scrollCap: 60, scrollSafety: 0.62,
  shortWidth: 34, themeEvery: 6,
})
const C = CAT_CLIMB_CONFIG
const EPS = 1e-9
const clamp = (n, lo, hi) => Math.min(hi, Math.max(lo, n))
const wrap = (x) => ((x % C.width) + C.width) % C.width
const ringDistance = (a, b) => {
  const d = Math.abs(wrap(a) - wrap(b))
  return Math.min(d, C.width - d)
}
const ringDelta = (from, to) => {
  const d = wrap(to) - wrap(from)
  if (d > C.width / 2) return d - C.width
  if (d < -C.width / 2) return d + C.width
  return d
}
const validMode = (mode) => mode === 'fixed' || mode === 'random'
const validSeed = (seed) => Number.isInteger(seed) && seed >= 0 && seed <= 0xffffffff

export const CAT_CLIMB_STAGE_LABELS = Object.freeze({
  warmup: '宽台热身', alternating: '交错爬高', narrow: '窄台练习', moving: '窄台与慢移台',
})
export const CAT_CLIMB_THEME_LABELS = Object.freeze({
  stair: '小台阶', zigzag: '大摆动', cross: '跨半周', rush: '穿接缝',
})
// 外观只由 surface 决定；moving 是正交属性，任何地面都可以移动或静止。
// accel 是 vx 趋近目标速度的一阶响应系数，worst 是可达性证明用的最坏有效横速比例。
export const CAT_CLIMB_SURFACES = Object.freeze({
  normal: Object.freeze({ key: 'normal', label: '普通地面', short: '普通', accel: 60, speedFactor: 1, belt: 0, worst: 0.95 }),
  ice: Object.freeze({ key: 'ice', label: '冰面 · 会打滑', short: '冰面', accel: 3, speedFactor: 1, belt: 0, worst: 0.5 }),
  sticky: Object.freeze({ key: 'sticky', label: '粘地面 · 变慢', short: '粘地', accel: 5, speedFactor: 0.55, belt: 0, worst: 0.35 }),
  belt: Object.freeze({ key: 'belt', label: '传送带', short: '传送带', accel: 12, speedFactor: 1, belt: 0.35, worst: 0.6 }),
})

// 每档同时收紧平台、拉大间距、提高横速与镜头速度；scroll 恒低于该档最小 gap 的理论爬升速率。
export const CAT_CLIMB_DIFFICULTIES = Object.freeze([
  { stage: 'warmup', width: 95, minGap: 48, maxGap: 56, minOffset: 40, maxOffset: 70,
    speed: 180, scroll: 32, movingInterval: 0, shortChance: 0, surfaceChance: 0, surfaces: ['normal'] },
  { stage: 'alternating', width: 85, minGap: 52, maxGap: 60, minOffset: 60, maxOffset: 110,
    speed: 220, scroll: 38, movingInterval: 0, shortChance: 0.10, surfaceChance: 0, surfaces: ['normal'] },
  { stage: 'narrow', width: 72, minGap: 58, maxGap: 66, minOffset: 80, maxOffset: 150,
    speed: 255, scroll: 46, movingInterval: 6, shortChance: 0.18,
    surfaceChance: 0.18, surfaces: ['normal', 'ice', 'sticky'] },
  { stage: 'moving', width: 64, minGap: 64, maxGap: 72, minOffset: 100, maxOffset: 175,
    speed: 290, scroll: 54, movingInterval: 4, shortChance: 0.24,
    surfaceChance: 0.26, surfaces: ['normal', 'ice', 'sticky', 'belt'] },
  { stage: 'moving', width: 60, minGap: 68, maxGap: 76, minOffset: 120, maxOffset: 190,
    speed: 320, scroll: 60, movingInterval: 3, shortChance: 0.30,
    surfaceChance: 0.34, surfaces: ['normal', 'ice', 'sticky', 'belt'] },
].map((settings) => Object.freeze(settings)))

function checkDifficultyEvery(difficultyEvery) {
  if (!Number.isSafeInteger(difficultyEvery) || difficultyEvery <= 0) throw new RangeError('升档层数必须是安全非负整数')
  return difficultyEvery
}

export function getCatClimbStage(index = 0, { difficultyEvery = C.difficultyEvery } = {}) {
  checkDifficultyEvery(difficultyEvery)
  if (!Number.isSafeInteger(index) || index < 0) throw new RangeError('平台索引必须是安全非负整数')
  const level = Math.min(C.difficultyMaxLevel, Math.floor(Math.max(0, index - 1) / difficultyEvery))
  const settings = CAT_CLIMB_DIFFICULTIES[level]
  return { ...settings, level, difficultyLevel: level, difficultyEvery, label: CAT_CLIMB_STAGE_LABELS[settings.stage] }
}

export function createCatClimbRandom(seed = 1) {
  let value = validSeed(seed) ? seed >>> 0 : 1
  return () => {
    value = (value + 0x6d2b79f5) >>> 0
    let n = Math.imul(value ^ (value >>> 15), 1 | value)
    n ^= n + Math.imul(n ^ (n >>> 7), 61 | n)
    return ((n ^ (n >>> 14)) >>> 0) / 4294967296
  }
}

export function catClimbDescendingTime(gap) {
  if (!Number.isFinite(gap) || gap < 0 || gap > C.jumpSpeed ** 2 / (2 * C.gravity)) return null
  return (C.jumpSpeed + Math.sqrt(C.jumpSpeed ** 2 - 2 * C.gravity * gap)) / C.gravity
}

export function getCatClimbPlatformX(platform, time) {
  const base = platform.baseX ?? platform.x
  return wrap(platform.type === 'moving'
    ? base + platform.amplitude * Math.sin(time * 2 * Math.PI / platform.period + platform.phase)
    : base)
}

// 环形可达性：脚底接触区间按环形最短距离度量，并额外校验镜头追不上玩家的理论爬升速率。
export function proveCatClimbSegment(from, to, opts = {}) {
  const speed = Number.isFinite(opts.speed) ? opts.speed : C.horizontalSpeed
  const scroll = Number.isFinite(opts.scroll) ? opts.scroll : 0
  const maxGap = Number.isFinite(opts.maxGap) ? opts.maxGap : C.maxGap
  const geometry = (p) => p && Number.isFinite(p.baseX ?? p.x) && Number.isFinite(p.y)
    && Number.isFinite(p.width) && p.width >= C.landingMargin * 2
    && ['fixed', 'moving'].includes(p.type)
    && (p.type !== 'moving' || (Number.isFinite(p.amplitude) && p.amplitude >= 0
      && p.amplitude <= C.maxAmplitude && Number.isFinite(p.period) && p.period >= C.minPeriod
      && Number.isFinite(p.phase)))
  if (!geometry(from) || !geometry(to)) return { valid: false, reason: 'invalid-geometry' }
  const gap = to.y - from.y
  const time = catClimbDescendingTime(gap)
  if (!time || gap < C.minGap || gap > maxGap) return { valid: false, reason: 'invalid-gap', gap }
  const a = from.type === 'moving' ? from.amplitude : 0
  const b = to.type === 'moving' ? to.amplitude : 0
  const x1 = wrap(from.baseX ?? from.x)
  const x2 = wrap(to.baseX ?? to.x)
  const halfFrom = from.width / 2 + a + C.catHalfWidth
  const entries = [wrap(x1 - halfFrom), x1, wrap(x1 + halfFrom)]
  const halfLanding = Math.max(0, to.width / 2 - b + C.catHalfWidth - C.landingMargin)
  const endpointDistances = entries.map((x) => Math.max(0, ringDistance(x, x2) - halfLanding))
  const horizontalBudget = speed * time - C.horizontalSafety
  const climbRate = gap / time
  const scrollValid = scroll <= climbRate * C.scrollSafety + EPS
  const valid = halfLanding > 0 && scrollValid
    && Math.max(...endpointDistances) <= horizontalBudget + EPS
  return { valid, gap, descendingTime: time, horizontalBudget, climbRate, scroll, scrollValid,
    fromEntry: entries, landingInterval: [wrap(x2 - halfLanding), wrap(x2 + halfLanding)],
    landingHalfWidth: halfLanding, endpointDistances, margin: C.landingMargin,
    safety: C.horizontalSafety, fromId: from.id, toId: to.id }
}

function themePool(level) {
  if (level <= 0) return ['stair']
  if (level === 1) return ['stair', 'zigzag']
  if (level === 2) return ['stair', 'zigzag', 'cross']
  return ['stair', 'zigzag', 'cross', 'rush']
}

export function createCatClimbGenerator({ seed = 1, mode = 'random', difficultyEvery = C.difficultyEvery } = {}) {
  checkDifficultyEvery(difficultyEvery)
  const routeMode = validMode(mode) ? mode : 'random'
  const routeSeed = routeMode === 'fixed' ? 0xc47 : validSeed(seed) ? seed : 1
  const random = createCatClimbRandom(routeSeed)
  const settingsAt = (index) => getCatClimbStage(index, { difficultyEvery })
  let index = 0
  let last = {
    id: 0, index: 0, baseX: 180, x: 180, y: C.startY, width: C.warmupWidth,
    type: 'fixed', primary: true, launchpad: true, stage: 'warmup', level: 0,
    difficultyLevel: 0, theme: 'stair', surface: 'normal', beltDir: 1,
  }
  function start() { return { ...last } }
  function next() {
    index += 1
    const settings = settingsAt(index)
    const pool = themePool(settings.level)
    const theme = routeMode === 'fixed'
      ? pool[Math.floor(index / C.themeEvery) % pool.length]
      : pool[Math.floor(random() * pool.length)]
    let gap = settings.minGap + Math.floor(random() * (settings.maxGap - settings.minGap + 1))
    // 镜头速度必须能被该 gap 的理论爬升速率覆盖，否则数学上追不上。
    while (gap < settings.maxGap
      && settings.scroll > (gap / catClimbDescendingTime(gap)) * C.scrollSafety) gap += 1
    const y = last.y + gap
    let width = settings.width
    if (random() < settings.shortChance) width = C.shortWidth
    const moving = settings.movingInterval > 0 && index % settings.movingInterval === 0
    if (moving) width = Math.max(width, C.movingWidth)
    const amplitude = moving ? 8 + Math.floor(random() * 8) : 0
    const chunk = Math.floor(index / C.themeEvery)
    // 大跨度路线强制普通地面，避免减速类地面把跨半周做成死局。
    const bigJump = theme === 'cross' || theme === 'rush'
    const specials = settings.surfaces.filter((s) => s !== 'normal')
    const surface = (bigJump || !specials.length || random() >= settings.surfaceChance)
      ? 'normal' : specials[Math.floor(random() * specials.length)]
    // zigzag 段传送带顺向助推，其余随机左右。
    const beltDir = theme === 'zigzag' ? (chunk % 2 === 0 ? 1 : -1)
      : (random() < 0.5 ? -1 : 1)
    // 地面效果延续到起跳后的空中，所以证明用「起跳平台」的最坏有效横速。
    const proveSpeed = settings.speed * CAT_CLIMB_SURFACES[last.surface || 'normal'].worst
    let delta = 0
    if (theme === 'stair') {
      const top = Math.min(settings.maxOffset, 80)
      delta = (random() < 0.5 ? -1 : 1) * (settings.minOffset + random() * (top - settings.minOffset))
    } else if (theme === 'zigzag') {
      delta = (chunk % 2 === 0 ? 1 : -1) * settings.maxOffset * (0.7 + random() * 0.3)
    } else if (theme === 'cross') {
      delta = (random() < 0.5 ? -1 : 1) * (C.width / 2 - 8 + random() * 16)
    } else {
      delta = ringDelta(last.baseX, random() < 0.5 ? 8 : C.width - 8)
    }
    const proofFor = (baseX, candidateGap) => {
      const candidate = {
        id: index, index, baseX, x: baseX, y: last.y + candidateGap, width,
        type: moving ? 'moving' : 'fixed', primary: true, theme, surface, beltDir,
        ...(moving ? { amplitude, period: C.minPeriod + random() * 1.6, phase: random() * 2 * Math.PI } : {}),
      }
      return { candidate,
        proof: proveCatClimbSegment(last, candidate,
          { speed: proveSpeed, scroll: settings.scroll, maxGap: settings.maxGap }) }
    }
    let { candidate, proof } = proofFor(wrap(last.baseX + delta), gap)
    let shrink = 1
    while (!proof.valid && shrink > 0.04) {
      shrink *= 0.78
      const retry = proofFor(wrap(last.baseX + delta * shrink), gap)
      candidate = retry.candidate
      proof = retry.proof
    }
    if (!proof.valid) {
      const retry = proofFor(wrap(last.baseX + settings.minOffset), gap)
      candidate = retry.candidate
      proof = retry.proof
    }
    if (!proof.valid) throw new Error('爬高路段可达性约束失败')
    const platform = { ...candidate, stage: settings.stage, level: settings.level,
      difficultyLevel: settings.level }
    last = platform
    return { primary: { ...platform }, branches: [],
      proofs: [{ ...proof, fromEntry: [...proof.fromEntry], landingInterval: [...proof.landingInterval],
        endpointDistances: [...proof.endpointDistances] }] }
  }
  function snapshot() {
    return { mode: routeMode, seed: routeSeed, difficultyEvery, index,
      difficultyLevel: last.difficultyLevel, level: last.level, theme: last.theme, last: { ...last } }
  }
  return { next, start, snapshot }
}

// 连续解脚底下降跨顶的时刻；横向环绕，落点按环形距离判断。
export function sweepCatClimbLanding(cat, platforms, dt, simTime = 0) {
  if (!cat || ![cat.x, cat.y, cat.vx, cat.vy, dt, simTime].every(Number.isFinite) || dt <= 0) return null
  let first = null
  for (const p of platforms) {
    if (p.launchpad) continue
    const discriminant = cat.vy ** 2 + 2 * C.gravity * (cat.y - p.y)
    if (discriminant < 0) continue
    const time = (cat.vy + Math.sqrt(discriminant)) / C.gravity
    if (time < -EPS || time > dt + EPS || cat.vy - C.gravity * time >= -EPS) continue
    const x = wrap(cat.x + cat.vx * time)
    const platformX = getCatClimbPlatformX(p, simTime + time)
    if (ringDistance(x, platformX) >= p.width / 2 + C.catHalfWidth - EPS) continue
    if (!first || time < first.time) first = { time: Math.max(0, time), x, y: p.y, id: p.id,
      type: p.type, index: p.index, stage: p.stage, theme: p.theme,
      surface: p.surface || 'normal', beltDir: p.beltDir || 1 }
  }
  return first
}

export function createCatClimbEngine({ seed = 1, mode = 'random', difficultyEvery = C.difficultyEvery } = {}) {
  checkDifficultyEvery(difficultyEvery)
  let state, generator, accumulator = 0, direction = 0, priorPhase = 'ready'
  let routeSeed = validSeed(seed) ? seed : 1
  let routeMode = validMode(mode) ? mode : 'random'
  const settingsNow = () => CAT_CLIMB_DIFFICULTIES[state.difficultyLevel]
  function reset() {
    generator = createCatClimbGenerator({ seed: routeSeed, mode: routeMode, difficultyEvery })
    accumulator = 0
    direction = 0
    priorPhase = 'ready'
    const startPlatform = generator.start()
    state = {
      phase: 'ready', mode: routeMode, seed: generator.snapshot().seed,
      cat: { x: 180, y: C.startY, vx: 0, vy: 0, facing: 1, grounded: true, surface: 'normal' },
      groundId: startPlatform.id, coyoteUntil: 0, jumpQueuedUntil: 0,
      beltDir: 1, surfaceLabel: CAT_CLIMB_SURFACES.normal.label,
      platforms: [startPlatform], cameraY: 0, height: 0, floor: 0, maxLandingIndex: 0,
      difficultyEvery, difficultyLevel: 0, level: 0, difficultyCapped: false,
      difficultyStage: 'warmup', difficultyLabel: CAT_CLIMB_STAGE_LABELS.warmup,
      speed: CAT_CLIMB_DIFFICULTIES[0].speed, scrollSpeed: CAT_CLIMB_DIFFICULTIES[0].scroll,
      theme: 'stair', reason: '', simTime: 0, jumps: 0, lastLanding: null,
    }
    generate()
  }
  function generate() {
    while (state.platforms.at(-1).y < state.cameraY + C.ahead) {
      const segment = generator.next()
      const platforms = [segment.primary, ...segment.branches]
      for (const p of platforms) p.x = getCatClimbPlatformX(p, state.simTime)
      state.platforms.push(...platforms)
    }
    state.platforms = state.platforms.filter((p) => p.y >= state.cameraY - C.dropBuffer)
    if (state.platforms.length > C.maxPlatforms) throw new Error('爬高平台数量超过约束')
  }
  function snapshot() {
    return { ...state, cat: { ...state.cat }, platforms: state.platforms.map((p) => ({ ...p })),
      lastLanding: state.lastLanding ? { ...state.lastLanding } : null }
  }
  function updateHeight(y) {
    state.height = Math.max(state.height, (y - C.startY) / 10)
    state.cameraY = Math.max(state.cameraY, y - C.height * C.cameraThreshold)
  }
  function launch() {
    state.cat.vy = C.jumpSpeed
    state.cat.grounded = false
    state.groundId = null
    state.coyoteUntil = 0
    state.jumpQueuedUntil = 0
  }
  function step() {
    const settings = settingsNow()
    state.speed = settings.speed
    state.scrollSpeed = settings.scroll
    state.cameraY += settings.scroll * C.step
    // 地面效果延续到下一次落地：vx 按当前表面的一阶响应逼近目标速度，传送带叠加方向偏置。
    const surface = CAT_CLIMB_SURFACES[state.cat.surface] || CAT_CLIMB_SURFACES.normal
    const targetVx = direction * settings.speed * surface.speedFactor
      + (surface.belt ? state.beltDir * settings.speed * surface.belt : 0)
    state.cat.vx += (targetVx - state.cat.vx) * Math.min(1, surface.accel * C.step)
    let remaining = C.step
    while (remaining > EPS) {
      const cat = state.cat
      if (cat.grounded) {
        const elapsed = remaining
        remaining = 0
        const support = state.platforms.find((p) => p.id === state.groundId)
        if (support) {
          const before = getCatClimbPlatformX(support, state.simTime)
          const after = getCatClimbPlatformX(support, state.simTime + elapsed)
          cat.x = wrap(cat.x + wrap(after - before + C.width / 2) - C.width / 2 + cat.vx * elapsed)
          if (ringDistance(cat.x, after) > support.width / 2 + C.catHalfWidth - EPS) {
            cat.grounded = false
            cat.vy = 0
            state.groundId = null
            state.coyoteUntil = state.simTime + elapsed + C.coyote
          }
        } else {
          cat.x = wrap(cat.x + cat.vx * elapsed)
        }
        state.simTime += elapsed
        if (cat.grounded && state.jumpQueuedUntil >= state.simTime) launch()
      } else {
        const hit = sweepCatClimbLanding(cat, state.platforms, remaining, state.simTime)
        const elapsed = hit ? hit.time : remaining
        const apexTime = clamp(cat.vy / C.gravity, 0, elapsed)
        updateHeight(cat.y + cat.vy * apexTime - C.gravity * apexTime ** 2 / 2)
        cat.x = wrap(cat.x + cat.vx * elapsed)
        cat.y += cat.vy * elapsed - C.gravity * elapsed ** 2 / 2
        cat.vy -= C.gravity * elapsed
        state.simTime += elapsed
        remaining -= elapsed
        if (hit) {
          cat.y = hit.y
          cat.vy = 0
          cat.grounded = true
          state.groundId = hit.id
          state.jumps++
          state.lastLanding = { id: hit.id, type: hit.type, index: hit.index, stage: hit.stage,
            theme: hit.theme, x: hit.x, y: hit.y, time: state.simTime }
          state.floor = Math.max(state.floor, hit.index)
          state.maxLandingIndex = state.floor
          state.difficultyLevel = Math.min(C.difficultyMaxLevel, Math.floor(state.floor / difficultyEvery))
          state.level = state.difficultyLevel
          state.difficultyCapped = state.level === C.difficultyMaxLevel
          state.difficultyStage = CAT_CLIMB_DIFFICULTIES[state.level].stage
          state.difficultyLabel = CAT_CLIMB_STAGE_LABELS[state.difficultyStage]
          state.theme = hit.theme || 'stair'
          state.cat.surface = hit.surface || 'normal'
          state.beltDir = hit.beltDir || 1
          state.surfaceLabel = CAT_CLIMB_SURFACES[state.cat.surface].label
          if (state.jumpQueuedUntil >= state.simTime) launch()
        }
      }
    }
    for (const p of state.platforms) p.x = getCatClimbPlatformX(p, state.simTime)
    generate()
    if (state.cat.y < state.cameraY - C.dropBuffer) {
      state.phase = 'ended'
      state.reason = '没跟上上升的地面'
      state.cat.vx = 0
      direction = 0
      accumulator = 0
    }
  }
  function dispatch(event) {
    if (!event || typeof event !== 'object') return snapshot()
    switch (event.type) {
      case 'start':
        if (state.phase !== 'ready') break
        state.phase = 'running'
        state.cat.vy = 0
        state.cat.grounded = true
        break
      case 'jump':
        if (state.phase !== 'running') break
        state.jumpQueuedUntil = state.simTime + C.jumpBuffer
        if (state.cat.grounded || state.simTime <= state.coyoteUntil) launch()
        break
      case 'direction':
        if (state.phase !== 'running' || ![-1, 0, 1].includes(event.direction)) break
        direction = event.direction
        if (direction) state.cat.facing = direction
        break
      case 'pause':
        if (state.phase === 'paused') break
        priorPhase = state.phase
        state.phase = 'paused'
        state.cat.vx = 0
        direction = 0
        accumulator = 0
        break
      case 'resume':
        if (state.phase !== 'paused') break
        state.phase = priorPhase
        accumulator = 0
        break
      case 'reset':
        if ((event.mode !== undefined && !validMode(event.mode))
          || (event.seed !== undefined && !validSeed(event.seed))
          || (event.difficultyEvery !== undefined && (!Number.isSafeInteger(event.difficultyEvery)
            || event.difficultyEvery <= 0))) break
        routeMode = event.mode ?? routeMode
        routeSeed = event.seed ?? routeSeed
        difficultyEvery = event.difficultyEvery ?? difficultyEvery
        reset()
        break
    }
    return snapshot()
  }
  function tick(dt) {
    if (state.phase !== 'running' || !Number.isFinite(dt) || dt <= 0) return snapshot()
    accumulator += Math.min(dt, C.maxDt)
    while (accumulator + EPS >= C.step && state.phase === 'running') {
      accumulator = Math.max(0, accumulator - C.step)
      step()
    }
    return snapshot()
  }
  reset()
  return { dispatch, tick, snapshot }
}
