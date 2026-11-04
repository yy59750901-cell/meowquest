// 世界坐标向上为正；cat.y 始终是脚底，平台 x 是中心、y 是顶面。
export const CAT_CLIMB_CONFIG = Object.freeze({
  width: 360, height: 560, step: 1 / 120, maxDt: 0.1,
  jumpSpeed: 430, gravity: 1000, horizontalSpeed: 160, catHalfWidth: 12,
  startY: 56, cameraThreshold: 0.6, dropBuffer: 80,
  ahead: 560 + 2 * (430 * 430 / 2000), maxPlatforms: 60,
  minGap: 48, maxGap: 68, warmupWidth: 95, platformWidth: 60,
  movingWidth: 65, maxAmplitude: 16, minPeriod: 3.6,
  landingMargin: 4, horizontalSafety: 10, difficultyEvery: 5, difficultyMaxLevel: 4,
})
const C = CAT_CLIMB_CONFIG
const EPS = 1e-9
const clamp = (n, lo, hi) => Math.min(hi, Math.max(lo, n))
const validMode = (mode) => mode === 'fixed' || mode === 'random'
const validSeed = (seed) => Number.isInteger(seed) && seed >= 0 && seed <= 0xffffffff

export const CAT_CLIMB_STAGE_LABELS = Object.freeze({
  warmup: '宽台热身', alternating: '交错爬高', narrow: '窄台练习', moving: '窄台与慢移台',
})

export const CAT_CLIMB_DIFFICULTIES = Object.freeze([
  { stage: 'warmup', width: 95, minGap: 48, maxGap: 56, minOffset: 48, maxOffset: 60, movingBranchInterval: 0 },
  { stage: 'alternating', width: 85, minGap: 48, maxGap: 60, minOffset: 54, maxOffset: 66, movingBranchInterval: 0 },
  { stage: 'narrow', width: 72, minGap: 48, maxGap: 64, minOffset: 58, maxOffset: 70, movingBranchInterval: 6 },
  { stage: 'moving', width: 64, minGap: 48, maxGap: 68, minOffset: 60, maxOffset: 74, movingBranchInterval: 4 },
  { stage: 'moving', width: 60, minGap: 50, maxGap: 68, minOffset: 64, maxOffset: 76, movingBranchInterval: 3 },
].map((settings) => Object.freeze(settings)))

function checkDifficultyEvery(difficultyEvery) {
  if (!Number.isSafeInteger(difficultyEvery) || difficultyEvery <= 0) throw new RangeError('升档层数必须是安全正整数')
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
  return platform.type === 'moving'
    ? base + platform.amplitude * Math.sin(time * 2 * Math.PI / platform.period + platform.phase)
    : base
}

// 对全部可能的脚底接触中心（含平台运动两端）证明，而非只证明中心到中心。
// 目标区间取各运动相位的交集，并要求脚底至少重叠 landingMargin。
// 任意 entry 到该区间的最短距离 <= 下降落地时间 * 速度 - safety。
export function proveCatClimbSegment(from, to) {
  const geometry = (p) => p && Number.isFinite(p.baseX ?? p.x) && Number.isFinite(p.y)
    && Number.isFinite(p.width) && p.width >= C.landingMargin * 2
    && ['fixed', 'moving'].includes(p.type)
    && (p.type !== 'moving' || (Number.isFinite(p.amplitude) && p.amplitude >= 0
      && p.amplitude <= C.maxAmplitude && Number.isFinite(p.period) && p.period >= C.minPeriod
      && Number.isFinite(p.phase)))
  if (!geometry(from) || !geometry(to)) return { valid: false, reason: 'invalid-geometry' }
  const gap = to.y - from.y
  const time = catClimbDescendingTime(gap)
  if (!time || gap < C.minGap || gap > C.maxGap) return { valid: false, reason: 'invalid-gap', gap }
  const a = from.type === 'moving' ? from.amplitude : 0
  const b = to.type === 'moving' ? to.amplitude : 0
  const x1 = from.baseX ?? from.x
  const x2 = to.baseX ?? to.x
  const fromEntry = [clamp(x1 - from.width / 2 - a - C.catHalfWidth, C.catHalfWidth, C.width - C.catHalfWidth),
    clamp(x1 + from.width / 2 + a + C.catHalfWidth, C.catHalfWidth, C.width - C.catHalfWidth)]
  const landingInterval = [Math.max(C.catHalfWidth, x2 - to.width / 2 + b - C.catHalfWidth + C.landingMargin),
    Math.min(C.width - C.catHalfWidth, x2 + to.width / 2 - b + C.catHalfWidth - C.landingMargin)]
  const endpointDistances = fromEntry.map((x) => Math.max(landingInterval[0] - x, x - landingInterval[1], 0))
  const horizontalBudget = C.horizontalSpeed * time - C.horizontalSafety
  const inBounds = x1 - from.width / 2 - a >= 0 && x1 + from.width / 2 + a <= C.width
    && x2 - to.width / 2 - b >= 0 && x2 + to.width / 2 + b <= C.width
  return { valid: inBounds && landingInterval[0] <= landingInterval[1]
      && Math.max(...endpointDistances) <= horizontalBudget + EPS,
    gap, descendingTime: time, horizontalBudget, fromEntry, landingInterval, endpointDistances,
    margin: C.landingMargin, safety: C.horizontalSafety, fromId: from.id, toId: to.id }
}

export function createCatClimbGenerator({ seed = 1, mode = 'random', difficultyEvery = C.difficultyEvery } = {}) {
  checkDifficultyEvery(difficultyEvery)
  const routeMode = validMode(mode) ? mode : 'random'
  const routeSeed = routeMode === 'fixed' ? 0xc47 : validSeed(seed) ? seed : 1
  const random = createCatClimbRandom(routeSeed)
  const settingsAt = (index) => getCatClimbStage(index, { difficultyEvery })
  const branchDue = (index, settings) => settings.movingBranchInterval > 0
    && (index - 1 - settings.level * difficultyEvery) % settings.movingBranchInterval === 0
  const start = () => ({ id: 0, index: 0, stage: 'warmup', difficultyLevel: 0, level: 0,
    x: 180, baseX: 180, y: C.startY, width: C.warmupWidth,
    type: 'fixed', primary: true, launchpad: true, chunk: 0 })
  let index = 0
  let last = start()
  let fork = null
  let pattern = 0
  let sign = 1
  function next() {
    index++
    const settings = settingsAt(index)
    const gap = routeMode === 'fixed' ? Math.floor((settings.minGap + settings.maxGap) / 2)
      : settings.minGap + Math.floor(random() * (settings.maxGap - settings.minGap + 1))
    const width = settings.width
    const y = last.y + gap
    const chunk = Math.floor(index / 6)
    if (index % 6 === 0) {
      pattern = routeMode === 'fixed' ? chunk % 3 : Math.floor(random() * 3)
      sign = last.baseX > 180 ? -1 : 1
    }
    const primary = { id: index * 2, index, stage: settings.stage, difficultyLevel: settings.level, level: settings.level,
      x: last.baseX, baseX: last.baseX, y, width, type: 'fixed', primary: true, chunk }
    const branches = []
    const proofs = []
    // 世界边界与全 entry 可达区间取交集，不能先夹边再用另一夹持把平台推出边界。
    const budget = C.horizontalSpeed * catClimbDescendingTime(gap) - C.horizontalSafety
    const maxOffset = budget - (last.width - width) / 2 - C.landingMargin
    let minX = Math.max(width / 2 + 10, last.baseX - maxOffset)
    let maxX = Math.min(C.width - width / 2 - 10, last.baseX + maxOffset)
    if (fork) {
      primary.baseX = fork.joinX
      primary.x = primary.baseX
      proofs.push(proveCatClimbSegment(fork.branch, primary))
      fork = null
    } else {
      const nextSettings = settingsAt(index + 1)
      if (branchDue(index + 1, nextSettings)) {
        // 前一固定台预留分叉起跳区；最坏幅度/宽度/间距也可接两侧，N=1 不跳过慢移入门档。
        const incoming = C.horizontalSpeed * catClimbDescendingTime(nextSettings.maxGap) - C.horizontalSafety
          - (width - nextSettings.width) / 2 - C.landingMargin
        const joinSettings = settingsAt(index + 2)
        const outgoing = C.horizontalSpeed * catClimbDescendingTime(joinSettings.maxGap) - C.horizontalSafety
          - (nextSettings.width - joinSettings.width) / 2 - C.maxAmplitude - C.landingMargin
        const offset = Math.min(nextSettings.maxOffset, incoming - C.maxAmplitude, outgoing)
        const minLaunch = nextSettings.width / 2 + 10 + 2 * offset - incoming + C.maxAmplitude
        const maxLaunch = C.width - nextSettings.width / 2 - C.maxAmplitude - 2 * offset + incoming
        minX = Math.max(minX, minLaunch)
        maxX = Math.min(maxX, maxLaunch)
      }
      const offset = settings.minOffset + Math.floor(random() * (settings.maxOffset - settings.minOffset + 1))
      const sequence = [[1, -1, 1, -1], [-1, -1, 1, 1], [1, 1, -1, -1]][pattern]
      // 起点软垫只用于启动；第一落点需要右移，不可不操作刷层数。
      primary.baseX = clamp(index === 1 ? last.baseX + settings.maxOffset
        : last.baseX + sign * sequence[index % 4] * offset, minX, maxX)
      if (branchDue(index, settings)) {
        const amplitude = 12 + Math.floor(random() * 5)
        const nextSettings = settingsAt(index + 1)
        // 汇合按下一档最窄宽度、最大 gap、所有移动相位证明；N=1 也不可沿用当前档。
        const nextBudget = C.horizontalSpeed * catClimbDescendingTime(nextSettings.maxGap) - C.horizontalSafety
        const forkOffset = Math.min(offset, maxOffset - amplitude,
          nextBudget - (width - nextSettings.width) / 2 - amplitude - C.landingMargin)
        const minOrigin = Math.max(width / 2 + 10 + forkOffset,
          nextSettings.width / 2 + 10, last.baseX + forkOffset - maxOffset)
        const maxOrigin = Math.min(C.width - width / 2 - amplitude - forkOffset,
          C.width - nextSettings.width / 2 - 10, last.baseX - forkOffset + maxOffset - amplitude)
        if (minOrigin <= maxOrigin) {
          const origin = clamp(last.baseX, minOrigin, maxOrigin)
          const forkPrimary = { ...primary, baseX: origin - forkOffset, x: origin - forkOffset }
          const branch = { ...primary, id: index * 2 + 1, baseX: origin + forkOffset, x: origin + forkOffset,
            type: 'moving', primary: false, amplitude, period: C.minPeriod + random() * 1.6,
            phase: random() * 2 * Math.PI, rejoinsId: (index + 1) * 2 }
          const nextPreview = { ...primary, id: (index + 1) * 2, index: index + 1, stage: nextSettings.stage,
            difficultyLevel: nextSettings.level, level: nextSettings.level, width: nextSettings.width,
            baseX: origin, x: origin, y: y + nextSettings.maxGap }
          const branchProof = proveCatClimbSegment(last, branch)
          if (proveCatClimbSegment(last, forkPrimary).valid && branchProof.valid
              && proveCatClimbSegment(forkPrimary, nextPreview).valid && proveCatClimbSegment(branch, nextPreview).valid) {
            Object.assign(primary, forkPrimary)
            branches.push(branch)
            proofs.push(branchProof)
            fork = { joinX: origin, branch }
          }
        }
      }
    }
    primary.x = primary.baseX
    const proof = proveCatClimbSegment(last, primary)
    if (!proof.valid || proofs.some((p) => !p.valid)) throw new Error('爬高路段可达性约束失败')
    proofs.unshift(proof)
    last = primary
    return { primary: { ...primary }, branches: branches.map((p) => ({ ...p })),
      proofs: proofs.map((p) => ({ ...p, fromEntry: [...p.fromEntry], landingInterval: [...p.landingInterval],
        endpointDistances: [...p.endpointDistances] })) }
  }
  function snapshot() {
    return { mode: routeMode, seed: routeSeed, difficultyEvery, index,
      difficultyLevel: last.difficultyLevel, level: last.level, last: { ...last } }
  }
  return { next, start, snapshot }
}

// 连续解脚底下降跨顶的时刻；横向夹边和移动平台均在该时刻取位置。
export function sweepCatClimbLanding(cat, platforms, dt, simTime = 0) {
  if (!cat || ![cat.x, cat.y, cat.vx, cat.vy, dt, simTime].every(Number.isFinite) || dt <= 0) return null
  let first = null
  for (const p of platforms) {
    if (p.launchpad) continue
    const discriminant = cat.vy ** 2 + 2 * C.gravity * (cat.y - p.y)
    if (discriminant < 0) continue
    const time = (cat.vy + Math.sqrt(discriminant)) / C.gravity
    if (time < -EPS || time > dt + EPS || cat.vy - C.gravity * time >= -EPS) continue
    const x = clamp(cat.x + cat.vx * time, C.catHalfWidth, C.width - C.catHalfWidth)
    const platformX = getCatClimbPlatformX(p, simTime + time)
    if (Math.abs(x - platformX) >= p.width / 2 + C.catHalfWidth - EPS) continue
    if (!first || time < first.time) first = { time: Math.max(0, time), x, y: p.y, id: p.id, type: p.type,
      index: p.index, stage: p.stage }
  }
  return first
}

export function createCatClimbEngine({ seed = 1, mode = 'random', difficultyEvery = C.difficultyEvery } = {}) {
  checkDifficultyEvery(difficultyEvery)
  let state
  let generator
  let accumulator = 0
  let direction = 0
  let priorPhase = 'ready'
  let routeSeed = validSeed(seed) ? seed : 1
  let routeMode = validMode(mode) ? mode : 'random'
  function reset() {
    generator = createCatClimbGenerator({ seed: routeSeed, mode: routeMode, difficultyEvery })
    accumulator = 0
    direction = 0
    priorPhase = 'ready'
    state = { phase: 'ready', mode: routeMode, seed: routeSeed,
      cat: { x: 180, y: C.startY, vx: 0, vy: 0, facing: 1 },
      platforms: [generator.start()], cameraY: 0, height: 0, floor: 0, maxLandingIndex: 0,
      difficultyEvery, difficultyLevel: 0, level: 0, difficultyCapped: false,
      difficultyStage: 'warmup', difficultyLabel: CAT_CLIMB_STAGE_LABELS.warmup,
      reason: '', simTime: 0, jumps: 0, lastLanding: null }
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
  function step() {
    let remaining = C.step
    while (remaining > EPS) {
      const cat = state.cat
      const hit = sweepCatClimbLanding(cat, state.platforms, remaining, state.simTime)
      const elapsed = hit ? hit.time : remaining
      // 精确采样步内顶点，最高高度不依赖渲染帧率。
      const apexTime = clamp(cat.vy / C.gravity, 0, elapsed)
      updateHeight(cat.y + cat.vy * apexTime - C.gravity * apexTime ** 2 / 2)
      cat.x = clamp(cat.x + cat.vx * elapsed, C.catHalfWidth, C.width - C.catHalfWidth)
      cat.y += cat.vy * elapsed - C.gravity * elapsed ** 2 / 2
      cat.vy -= C.gravity * elapsed
      state.simTime += elapsed
      remaining -= elapsed
      if (hit) {
        cat.y = hit.y
        cat.vy = C.jumpSpeed
        state.jumps++
        state.lastLanding = { id: hit.id, type: hit.type, index: hit.index, stage: hit.stage,
          x: hit.x, y: hit.y, time: state.simTime }
        state.floor = Math.max(state.floor, hit.index)
        state.maxLandingIndex = state.floor
        state.difficultyLevel = Math.min(C.difficultyMaxLevel, Math.floor(state.floor / difficultyEvery))
        state.level = state.difficultyLevel
        state.difficultyCapped = state.level === C.difficultyMaxLevel
        state.difficultyStage = CAT_CLIMB_DIFFICULTIES[state.level].stage
        state.difficultyLabel = CAT_CLIMB_STAGE_LABELS[state.difficultyStage]
      }
    }
    for (const p of state.platforms) p.x = getCatClimbPlatformX(p, state.simTime)
    generate()
    if (state.cat.y < state.cameraY - C.dropBuffer) {
      state.phase = 'ended'
      state.reason = '没接住平台'
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
        state.cat.vy = C.jumpSpeed
        state.jumps = 1
        break
      case 'direction':
        if (state.phase !== 'running' || ![-1, 0, 1].includes(event.direction)) break
        direction = event.direction
        state.cat.vx = direction * C.horizontalSpeed
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
            || (event.difficultyEvery !== undefined && (!Number.isSafeInteger(event.difficultyEvery) || event.difficultyEvery <= 0))) break
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
      state.cat.vx = direction * C.horizontalSpeed
      step()
    }
    return snapshot()
  }
  reset()
  return { dispatch, tick, snapshot }
}
