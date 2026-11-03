export const CAT_PLAY_CONFIG = Object.freeze({
  width: 640,
  height: 400,
  catBounds: Object.freeze({ left: 56, right: 584, top: 176, bottom: 350 }),
  featherBounds: Object.freeze({ left: 36, right: 604, top: 150, bottom: 350 }),
  featherHitSize: 60,
  featherEdgeMargin: 36,
  initialCat: Object.freeze({ x: 220, y: 278 }),
  initialFeather: Object.freeze({ x: 345, y: 252 }),
  minPounceDistance: 60,
  maxPounceDistance: 160,
  followSpeed: 120,
  followStopDistance: 40,
  stillGrace: 0.12,
  chargeSeconds: 0.55,
  stillTolerance: 6,
  readyMoveTolerance: 12,
  perfectSeconds: 0.6,
  goodSeconds: 1.5,
  pounceSeconds: 0.48,
  caughtSeconds: 0.5,
  recoverSeconds: 0.3,
  missMaxDistance: 90,
  fixedStep: 1 / 120,
  maxDt: 0.1,
})

export const CAT_PLAY_CHALLENGE_CONFIG = Object.freeze({
  initialCat: Object.freeze({ x: 160, y: 292 }),
  initialFeather: Object.freeze({ x: 242, y: 280 }),
  rug: Object.freeze({ x: 270, y: 246, w: 100, h: 72 }),
  catRadius: 16,
  waypoint: Object.freeze({ x: 326, y: 202, radius: 28 }),
  goal: Object.freeze({ x: 478, y: 286, radius: 40 }),
  missMaxDistance: 36,
  followStopDistance: 72,
  retryDistanceMargin: 8,
})
export const CAT_PLAY_HELP = '拖动羽毛，停一停，看到它准备好再松手'
export const CAT_PLAY_CHALLENGE_HELP = '引猫绕过垫子、经过脚印，再在目标圈扑到羽毛'

const EPSILON = 1e-9
const clamp = (n, min, max) => Math.max(min, Math.min(max, n))
const distance = (a, b) => Math.hypot(a.x - b.x, a.y - b.y)
const pointIn = (point, bounds) => ({
  x: clamp(point.x, bounds.left, bounds.right),
  y: clamp(point.y, bounds.top, bounds.bottom),
})
const inCircle = (point, circle) => distance(point, circle) <= circle.radius + EPSILON

export function expandCatPlayRect(rect, radius) {
  return { left: rect.x - radius, right: rect.x + rect.w + radius,
    top: rect.y - radius, bottom: rect.y + rect.h + radius }
}

// 连续线段与闭合扩框相交；擦边也算阻挡，而不只检测终点。
function segmentRectEntry(from, to, bounds) {
  let entry = 0
  let exit = 1
  for (const [axis, min, max] of [['x', bounds.left, bounds.right], ['y', bounds.top, bounds.bottom]]) {
    const delta = to[axis] - from[axis]
    if (Math.abs(delta) < EPSILON) {
      if (from[axis] < min || from[axis] > max) return null
    } else {
      const a = (min - from[axis]) / delta
      const b = (max - from[axis]) / delta
      entry = Math.max(entry, Math.min(a, b))
      exit = Math.min(exit, Math.max(a, b))
      if (entry > exit + EPSILON) return null
    }
  }
  return entry <= 1 && exit >= 0 ? entry : null
}

export function catPlaySegmentIntersectsRect(from, to, bounds) {
  return segmentRectEntry(from, to, bounds) !== null
}

export function getCatPlayTimingEvaluation(readySeconds, config = CAT_PLAY_CONFIG) {
  if (readySeconds <= config.perfectSeconds + EPSILON) {
    return { grade: 'perfect', title: '时机特别好！', detail: '绿圈亮起后及时松手，小猫抓到了羽毛。' }
  }
  if (readySeconds <= config.goodSeconds + EPSILON) {
    return { grade: 'good', title: '不错，抓到了！', detail: '下次绿圈亮起后，试着更快松手。' }
  }
  return { grade: 'late', title: '慢了一点，还是抓到了！', detail: '准备不会过期，下次看到绿圈就可以松手。' }
}

function constrainFeatherBounds(bounds) {
  const floor = CAT_PLAY_CONFIG.featherBounds
  if (!bounds || !['left', 'right', 'top', 'bottom'].every((key) => Number.isFinite(bounds[key]))) {
    throw new RangeError('羽毛边界必须是有限数字')
  }
  const next = {
    left: Math.max(floor.left, bounds.left), right: Math.min(floor.right, bounds.right),
    top: Math.max(floor.top, bounds.top), bottom: Math.min(floor.bottom, bounds.bottom),
  }
  if (next.left > next.right || next.top > next.bottom) throw new RangeError('羽毛边界必须位于场地地板内')
  return next
}

export function getCatPlayFeatherBounds(stageWidth, stageHeight) {
  const { width, height, featherEdgeMargin: margin } = CAT_PLAY_CONFIG
  if (!Number.isFinite(stageWidth) || !Number.isFinite(stageHeight) || stageWidth < margin * 2 || stageHeight < margin * 2) {
    throw new RangeError('场地尺寸必须能容纳完整羽毛热区')
  }
  return constrainFeatherBounds({
    left: margin * width / stageWidth, right: width - margin * width / stageWidth,
    top: margin * height / stageHeight, bottom: height - margin * height / stageHeight,
  })
}

// 所有时间均由调用方以秒传入；固定小步使不同刷新率共享同一判定。
export function createCatPlayEngine(options = {}) {
  const config = { ...CAT_PLAY_CONFIG, ...options }
  const challengeConfig = CAT_PLAY_CHALLENGE_CONFIG
  const obstacle = expandCatPlayRect(challengeConfig.rug, challengeConfig.catRadius)
  let featherBounds = constrainFeatherBounds(config.featherBounds)
  let state
  let accumulator = 0
  let stillAnchor
  let readyAnchor = null
  let jump = null

  function reset(mode = state?.mode || (options.mode === 'challenge' ? 'challenge' : 'free')) {
    const paused = state?.phase === 'paused'
    const initial = mode === 'challenge' ? challengeConfig : config
    state = {
      mode,
      phase: paused ? 'paused' : 'idle',
      cat: { ...pointIn(initial.initialCat, config.catBounds), facing: 1, lift: 0 },
      feather: pointIn(initial.initialFeather, featherBounds),
      challenge: mode === 'challenge' ? {
        checkpointVisited: false, completed: false,
        rug: { ...challengeConfig.rug }, waypoint: { ...challengeConfig.waypoint }, goal: { ...challengeConfig.goal },
      } : null,
      pointerId: null,
      held: false,
      stillSeconds: 0,
      readySeconds: 0,
      phaseSeconds: 0,
      chargeProgress: 0,
      pounceProgress: 0,
      catches: 0,
      needsNextPlacement: false,
      blocked: false,
      result: null,
      evaluation: null,
      status: mode === 'challenge' ? '先绕到垫子上方，引小猫走进脚印。' : CAT_PLAY_HELP,
    }
    stillAnchor = { ...state.feather }
    readyAnchor = null
    jump = null
    accumulator = 0
  }

  function snapshot() {
    return { ...state, cat: { ...state.cat }, feather: { ...state.feather }, bounds: { ...featherBounds },
      evaluation: state.evaluation ? { ...state.evaluation } : null,
      challenge: state.challenge ? { ...state.challenge, rug: { ...state.challenge.rug },
        waypoint: { ...state.challenge.waypoint }, goal: { ...state.challenge.goal } } : null }
  }

  function phase(next) {
    state.phase = next
    state.phaseSeconds = 0
  }

  function clearPreparation() {
    state.stillSeconds = 0
    state.readySeconds = 0
    state.chargeProgress = 0
    readyAnchor = null
    stillAnchor = { ...state.feather }
  }

  function rangeStatus(d) {
    if (d < config.minPounceDistance) return '羽毛太近啦，拉开一点，让它有地方扑。'
    if (d > config.maxPounceDistance) return '羽毛太远啦，拉近一点再停住。'
    return null
  }

  function pathBlocked(to = state.feather) {
    return state.mode === 'challenge' && catPlaySegmentIntersectsRect(state.cat, to, obstacle)
  }

  function routeFailure() {
    if (!state.challenge) return null
    if (pathBlocked()) return 'blocked'
    if (!state.challenge.checkpointVisited) return 'route-needed'
    if (!inCircle(state.feather, state.challenge.goal)) return 'outside-goal'
    return null
  }

  function routeStatus(reason = routeFailure()) {
    return {
      blocked: '垫子挡住啦，拖羽毛往上方绕，猫不会穿过垫子。',
      'route-needed': '先引小猫走进上方脚印，羽毛碰到脚印不算。',
      'outside-goal': '已经经过脚印！把羽毛引到右侧目标圈，再停一停。',
    }[reason] || rangeStatus(distance(state.cat, state.feather)) || '已到目标圈，停住羽毛，等它准备好再松手。'
  }

  function checkPreparationRange() {
    if (!state.held || (state.phase !== 'ready' && state.phase !== 'charging')) return false
    const reason = routeFailure()
    const status = reason ? routeStatus(reason) : rangeStatus(distance(state.cat, state.feather))
    if (!status) return false
    clearPreparation()
    phase('follow')
    state.status = status
    return true
  }

  function cancel(status = '已松开羽毛，按住就能再玩。') {
    if (state.mode === 'free' && ['pounce', 'caught', 'recover'].includes(state.phase)) state.needsNextPlacement = true
    state.held = false
    state.pointerId = null
    clearPreparation()
    jump = null
    accumulator = 0
    state.cat.lift = 0
    state.pounceProgress = 0
    state.result = null
    state.blocked = false
    phase('idle')
    state.status = status
  }

  function placeFeatherForNextRound() {
    if (state.mode === 'challenge') return
    state.needsNextPlacement = false
    const dx = state.cat.x + 125 <= featherBounds.right ? 125 : -125
    state.feather = pointIn({ x: state.cat.x + dx, y: state.cat.y - 18 }, featherBounds)
    stillAnchor = { ...state.feather }
  }

  function resize({ bounds }) {
    const next = constrainFeatherBounds(bounds)
    if (Object.keys(next).every((key) => next[key] === featherBounds[key])) return snapshot()
    featherBounds = next
    state.feather = pointIn(state.feather, featherBounds)
    if (state.phase === 'caught' || state.phase === 'recover') placeFeatherForNextRound()
    const wasPaused = state.phase === 'paused'
    const status = state.status === CAT_PLAY_HELP ? CAT_PLAY_HELP : '场地大小变了，重新按住羽毛再试。'
    cancel(status)
    if (wasPaused) phase('paused')
    return snapshot()
  }

  function timingEvaluation(d) {
    if (d > config.maxPounceDistance) return { grade: 'far', title: '羽毛太远啦！', detail: rangeStatus(d) }
    if (d < config.minPounceDistance) return { grade: 'close', title: '羽毛太近啦！', detail: rangeStatus(d) }
    if (state.phase !== 'ready') return { grade: 'early', title: '快了！', detail: '松得太早啦，等小猫准备好、圈圈变绿再松手。' }
    return getCatPlayTimingEvaluation(state.readySeconds, config)
  }

  function safeDestination(from, to) {
    if (state.mode !== 'challenge') return to
    const entry = segmentRectEntry(from, to, obstacle)
    if (entry === null) return to
    // 停在扩框外，留下极小间隙，玩家改方向后可以继续直线跟随。
    const t = Math.max(0, entry - 0.00001 / Math.max(distance(from, to), EPSILON))
    return { x: from.x + (to.x - from.x) * t, y: from.y + (to.y - from.y) * t }
  }

  function visitCheckpoint() {
    if (state.challenge && !state.challenge.checkpointVisited && inCircle(state.cat, state.challenge.waypoint)) {
      state.challenge.checkpointVisited = true
      state.status = routeStatus()
    }
  }

  function startJump() {
    const d = distance(state.cat, state.feather)
    const reason = routeFailure()
    const evaluation = reason ? {
      grade: reason,
      title: { blocked: '垫子挡住啦！', 'route-needed': '先经过上方脚印！', 'outside-goal': '还没到目标圈！' }[reason],
      detail: routeStatus(reason),
    } : timingEvaluation(d)
    const success = ['perfect', 'good', 'late'].includes(evaluation.grade)
    state.result = success ? 'caught' : reason || (evaluation.grade === 'far' ? 'too-far'
      : evaluation.grade === 'close' ? 'too-close' : 'too-early')
    state.status = '扑过去啦！'
    const missMax = state.mode === 'challenge' ? challengeConfig.missMaxDistance : config.missMaxDistance
    const missTravel = Math.min(missMax, d * 0.65)
    // 连续扑空仍留出下一次蓄势的距离；不足时只做原地小跃动。
    const travel = success ? d : state.challenge
      ? Math.min(missTravel, Math.max(0, d - config.minPounceDistance - challengeConfig.retryDistanceMargin))
      : missTravel
    const direction = d > 0 ? { x: (state.feather.x - state.cat.x) / d, y: (state.feather.y - state.cat.y) / d }
      : { x: state.cat.facing, y: 0 }
    const from = { x: state.cat.x, y: state.cat.y }
    jump = {
      from,
      to: safeDestination(from, pointIn({ x: from.x + direction.x * travel, y: from.y + direction.y * travel }, config.catBounds)),
      success,
      evaluation,
    }
    // 羽毛边界比猫更宽；边缘成功时猫脚点停在安全边界，羽毛抱到脚边。
    state.cat.facing = direction.x < 0 ? -1 : 1
    state.held = false
    state.pointerId = null
    clearPreparation()
    accumulator = 0
    state.pounceProgress = 0
    phase('pounce')
  }

  function dispatch(event) {
    if (event.type === 'resize') return resize(event)
    if (event.type === 'set-mode') {
      if (event.mode === 'free' || event.mode === 'challenge') reset(event.mode)
      return snapshot()
    }
    if (event.type === 'reset') {
      reset()
      return snapshot()
    }
    if (['pause', 'blur', 'visibilitychange'].includes(event.type)) {
      cancel(event.type === 'blur' ? '已暂停，回来后重新按住羽毛。'
        : event.type === 'visibilitychange' ? '已暂停，回到页面后重新按住羽毛。' : '休息一下，继续时重新按住羽毛。')
      phase('paused')
      return snapshot()
    }
    if (event.type === 'resume') {
      if (state.phase === 'paused') cancel('继续玩吧！重新按住羽毛，停一停再松手。')
      return snapshot()
    }
    if (state.phase === 'paused') return snapshot()
    if (event.type === 'cancel') {
      if (state.held || ['pounce', 'caught', 'recover'].includes(state.phase)) cancel('拖动已取消，按住羽毛可以再试。')
      return snapshot()
    }
    if (event.type === 'pointercancel') {
      if ((state.held && event.pointerId === state.pointerId) || (state.phase === 'pounce' && event.pointerId === jump.pointerId)) {
        cancel('拖动已取消，按住羽毛可以再试。')
      }
      return snapshot()
    }
    if (state.challenge?.completed) return snapshot()
    if (event.type === 'pointerdown') {
      if (state.held || state.phase === 'pounce') return snapshot()
      if (!Number.isFinite(event.x) || !Number.isFinite(event.y) || event.pointerId == null) return snapshot()
      // 重按时先放好下一轮羽毛；旧热区上的事件坐标不能覆盖新位置。
      if (state.mode === 'free' && (state.needsNextPlacement || state.phase === 'caught' || state.phase === 'recover')) placeFeatherForNextRound()
      else state.feather = pointIn(event, featherBounds)
      state.held = true
      state.pointerId = event.pointerId
      state.result = null
      state.evaluation = null
      state.blocked = false
      accumulator = 0
      clearPreparation()
      phase('follow')
      state.status = state.challenge ? routeStatus() : '它在跟着羽毛！找个距离，停一停。'
      return snapshot()
    }
    if (!state.held || event.pointerId !== state.pointerId) return snapshot()
    if (event.type === 'pointermove') {
      if (!Number.isFinite(event.x) || !Number.isFinite(event.y)) return snapshot()
      state.feather = pointIn(event, featherBounds)
      if (!checkPreparationRange()) {
        const anchor = state.phase === 'ready' ? readyAnchor : stillAnchor
        const tolerance = state.phase === 'ready' ? config.readyMoveTolerance : config.stillTolerance
        if (distance(state.feather, anchor) > tolerance) {
          clearPreparation()
          accumulator = 0
          phase('follow')
          state.status = state.challenge ? routeStatus()
            : rangeStatus(distance(state.cat, state.feather)) || '羽毛又动啦，停一停，让它重新准备。'
        }
      } else accumulator = 0
    } else if (event.type === 'pointerup') {
      const pointerId = state.pointerId
      startJump()
      jump.pointerId = pointerId
    }
    return snapshot()
  }

  function advance(dt) {
    if (state.challenge?.completed) return
    state.phaseSeconds += dt
    if (state.held) {
      // 只计算已经进入 ready 的完整小步，蓄势和此前按住时间不计入。
      if (state.phase === 'ready') state.readySeconds += dt
      checkPreparationRange()
      state.stillSeconds += dt
      const d = distance(state.cat, state.feather)
      const inRange = d >= config.minPounceDistance && d <= config.maxPounceDistance
      if (state.phase === 'follow' && inRange && !routeFailure() && state.stillSeconds + EPSILON >= config.stillGrace) {
        phase('charging')
        state.status = '它蹲下来蓄势了，保持羽毛不动……'
      }
      if (state.phase === 'charging') {
        state.chargeProgress = clamp(state.phaseSeconds / config.chargeSeconds, 0, 1)
        if (state.phaseSeconds + EPSILON >= config.chargeSeconds) {
          phase('ready')
          state.chargeProgress = 1
          readyAnchor = { ...state.feather }
          state.status = '准备好啦！圈圈变绿了，现在松手时机好！'
        }
      }
      if (state.phase === 'follow') {
        // 脚印前靠近引导点以走通上方通道；脚印后留出扑距，慢拖也不会贴到零距离。
        const stopDistance = state.challenge?.checkpointVisited ? challengeConfig.followStopDistance : config.followStopDistance
        const travel = Math.min(config.followSpeed * dt, Math.max(0, d - stopDistance))
        state.blocked = false
        if (travel > 0) {
          const dx = state.feather.x - state.cat.x
          state.cat.facing = dx < 0 ? -1 : 1
          const next = pointIn({
            x: state.cat.x + dx / d * travel,
            y: state.cat.y + (state.feather.y - state.cat.y) / d * travel,
          }, config.catBounds)
          const safe = safeDestination(state.cat, next)
          state.blocked = distance(next, safe) > EPSILON
          state.cat.x = safe.x
          state.cat.y = safe.y
          visitCheckpoint()
        }
        const status = state.challenge ? routeStatus() : rangeStatus(d)
        if (status) state.status = status
      }
      return
    }
    if (state.phase === 'pounce') {
      const progress = clamp(state.phaseSeconds / config.pounceSeconds, 0, 1)
      const eased = progress * progress * (3 - 2 * progress)
      state.pounceProgress = progress
      state.cat.x = jump.from.x + (jump.to.x - jump.from.x) * eased
      state.cat.y = jump.from.y + (jump.to.y - jump.from.y) * eased
      state.cat.lift = Math.sin(Math.PI * progress) * (jump.success ? 42 : 24)
      visitCheckpoint()
      if (progress + EPSILON >= 1) {
        state.cat.lift = 0
        state.evaluation = { ...jump.evaluation }
        state.status = state.evaluation.detail
        if (jump.success) {
          state.catches += 1
          state.feather = pointIn({ x: state.cat.x + state.cat.facing * 20, y: state.cat.y - 8 }, featherBounds)
          phase('caught')
          if (state.challenge) {
            state.challenge.completed = true
            state.evaluation.title = `挑战完成 · ${state.evaluation.title}`
            state.evaluation.detail = `绕路成功！${state.evaluation.detail}`
            state.status = state.evaluation.detail
          } else state.status = '抓到啦！它抱住了羽毛。还可以再来！'
        } else phase('recover')
        jump = null
      }
    } else if (state.phase === 'caught' && state.phaseSeconds + EPSILON >= config.caughtSeconds) {
      placeFeatherForNextRound()
      phase('recover')
    } else if (state.phase === 'recover' && state.phaseSeconds + EPSILON >= config.recoverSeconds) {
      // 自由玩恢复适合扑跃的距离；挑战保留猫与羽毛位置、真实脚印进度。
      if (state.result !== 'caught') placeFeatherForNextRound()
      phase('idle')
      if (state.challenge) state.status = routeStatus()
      else if (state.result === 'caught') state.status = '羽毛放好啦，按住它就能再来一次！'
      else state.status += ' 羽毛重新放好啦，按住再试。'
    }
  }

  function tick(dt) {
    if (!Number.isFinite(dt) || dt <= 0 || state.phase === 'paused' || state.challenge?.completed) return snapshot()
    accumulator += Math.min(dt, config.maxDt)
    while (accumulator + EPSILON >= config.fixedStep) {
      advance(config.fixedStep)
      accumulator = Math.max(0, accumulator - config.fixedStep)
    }
    return snapshot()
  }

  reset()
  return { dispatch, tick, snapshot, resize }
}
