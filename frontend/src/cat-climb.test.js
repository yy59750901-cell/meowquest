// 运行：node --test src/cat-climb.test.js
import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'
import { parse, compileScript, compileTemplate, compileStyle } from '@vue/compiler-sfc'
import {
  CAT_CLIMB_CONFIG as C, CAT_CLIMB_DIFFICULTIES, CAT_CLIMB_STAGE_LABELS, CAT_CLIMB_THEME_LABELS,
  CAT_CLIMB_SURFACES,
  getCatClimbStage, createCatClimbRandom, catClimbDescendingTime, getCatClimbPlatformX,
  proveCatClimbSegment, createCatClimbGenerator, sweepCatClimbLanding, createCatClimbEngine,
} from './cat-climb.js'

const near = (a, b, eps = 1e-6) => assert.ok(Math.abs(a - b) < eps, `${a} != ${b}`)
const wrap = (x) => ((x % C.width) + C.width) % C.width
const ringDistance = (a, b) => {
  const d = Math.abs(wrap(a) - wrap(b))
  return Math.min(d, C.width - d)
}
const FILE = new URL('./components/CatClimb.vue', import.meta.url)
const source = readFileSync(FILE, 'utf8')

test('难度档：横速 180→320、镜头 32→60，且每档镜头速度低于最小间距的理论爬升速率', () => {
  assert.equal(CAT_CLIMB_DIFFICULTIES.length, 5)
  assert.deepEqual(CAT_CLIMB_DIFFICULTIES.map((s) => s.speed), [180, 220, 255, 290, 320])
  assert.deepEqual(CAT_CLIMB_DIFFICULTIES.map((s) => s.scroll), [32, 38, 46, 54, 60])
  for (const s of CAT_CLIMB_DIFFICULTIES) {
    const t = catClimbDescendingTime(s.minGap)
    assert.ok(s.scroll <= (s.minGap / t) * C.scrollSafety + 1e-9,
      `${s.stage} 镜头 ${s.scroll} 超过理论爬升速率 ${s.minGap / t}`)
    assert.ok(s.maxGap >= s.minGap && s.minGap >= C.minGap)
    assert.ok(s.shortChance >= 0 && s.shortChance <= 0.3)
  }
  assert.equal(C.scrollCap, 60)
  assert.equal(C.coyote, 0.1)
  assert.equal(C.jumpBuffer, 0.1)
})

test('四种地面材质参数与随难度解锁的节奏', () => {
  assert.deepEqual(Object.keys(CAT_CLIMB_SURFACES), ['normal', 'ice', 'sticky', 'belt'])
  assert.equal(CAT_CLIMB_SURFACES.normal.accel, 60)
  assert.equal(CAT_CLIMB_SURFACES.ice.accel, 3, '冰面响应最慢')
  assert.equal(CAT_CLIMB_SURFACES.sticky.speedFactor, 0.55)
  assert.equal(CAT_CLIMB_SURFACES.belt.belt, 0.35)
  for (const s of Object.values(CAT_CLIMB_SURFACES)) {
    assert.ok(s.worst > 0 && s.worst <= 1 && s.label && s.short)
    assert.ok(s.accel > 0)
  }
  assert.deepEqual(CAT_CLIMB_DIFFICULTIES.slice(0, 2).map((s) => s.surfaces), [['normal'], ['normal']])
  assert.ok(CAT_CLIMB_DIFFICULTIES[2].surfaces.includes('ice'))
  assert.ok(CAT_CLIMB_DIFFICULTIES[2].surfaces.includes('sticky'))
  assert.ok(!CAT_CLIMB_DIFFICULTIES[2].surfaces.includes('belt'))
  assert.ok(CAT_CLIMB_DIFFICULTIES[3].surfaces.includes('belt'))
  assert.ok(CAT_CLIMB_DIFFICULTIES[4].surfaceChance > CAT_CLIMB_DIFFICULTIES[3].surfaceChance)
  const speed = 320
  const belt = CAT_CLIMB_SURFACES.belt
  assert.equal(speed * belt.speedFactor + speed * belt.belt, speed * 1.35)
  assert.equal(Math.abs(-speed * belt.speedFactor + speed * belt.belt), speed * 0.65)
})

test('升档按每 N 层、封顶 4 档，标签与主题字典完整', () => {
  assert.equal(getCatClimbStage(0).level, 0)
  assert.equal(getCatClimbStage(5, { difficultyEvery: 5 }).level, 0)
  assert.equal(getCatClimbStage(6, { difficultyEvery: 5 }).level, 1)
  assert.equal(getCatClimbStage(21, { difficultyEvery: 5 }).level, 4)
  assert.equal(getCatClimbStage(999, { difficultyEvery: 5 }).level, 4)
  assert.equal(getCatClimbStage(3, { difficultyEvery: 1 }).level, 2)
  assert.equal(getCatClimbStage(0).label, CAT_CLIMB_STAGE_LABELS.warmup)
  assert.deepEqual(Object.keys(CAT_CLIMB_THEME_LABELS), ['stair', 'zigzag', 'cross', 'rush'])
  assert.throws(() => getCatClimbStage(1, { difficultyEvery: 0 }), RangeError)
})

test('PRNG 确定性、下降时间物理与非法输入', () => {
  const a = createCatClimbRandom(42)
  const b = createCatClimbRandom(42)
  for (let i = 0; i < 50; i++) assert.equal(a(), b())
  assert.ok(catClimbDescendingTime(0) > 0.8)
  assert.equal(catClimbDescendingTime(-1), null)
  assert.equal(catClimbDescendingTime(C.jumpSpeed ** 2 / (2 * C.gravity) + 1), null)
  for (const gap of [48, 56, 68, 76]) {
    const t = catClimbDescendingTime(gap)
    near(C.jumpSpeed * t - C.gravity * t ** 2 / 2, gap)
  }
})

test('环绕坐标：移动平台 x 取模、环形距离走短路', () => {
  assert.equal(wrap(-10), 350)
  assert.equal(wrap(370), 10)
  assert.equal(getCatClimbPlatformX({ baseX: 355, type: 'fixed' }, 0), 355)
  const moving = { baseX: 355, type: 'moving', amplitude: 10, period: 4, phase: 0 }
  for (const t of [1, 2, 3]) {
    const x = getCatClimbPlatformX(moving, t)
    assert.ok(x >= 0 && x < C.width)
    assert.ok(ringDistance(x, 355) <= 10.001)
  }
})

test('环形可达性：跨缝 30px 视为可达，不会被直线 330px 误判', () => {
  const from = { id: 0, baseX: 350, y: 0, width: 60, type: 'fixed' }
  const to = { id: 1, baseX: 20, y: 56, width: 60, type: 'fixed' }
  const proof = proveCatClimbSegment(from, to, { speed: 180, scroll: 32, maxGap: 56 })
  assert.equal(proof.valid, true)
  assert.equal(proof.scrollValid, true)
  assert.ok(Math.max(...proof.endpointDistances) < 60)
  assert.ok(proof.climbRate > proof.scroll)
  const far = { ...to, baseX: 180 }
  assert.equal(proveCatClimbSegment(from, far, { speed: 180, scroll: 32, maxGap: 56 }).valid, false)
})

test('可达性证明同时守住横向预算与镜头纵向约束', () => {
  const from = { id: 0, baseX: 180, y: 0, width: 60, type: 'fixed' }
  const to = { id: 1, baseX: 180, y: 56, width: 60, type: 'fixed' }
  assert.equal(proveCatClimbSegment(from, to, { speed: 180, scroll: 32, maxGap: 56 }).valid, true)
  assert.equal(proveCatClimbSegment(from, to, { speed: 180, scroll: 500, maxGap: 56 }).scrollValid, false)
  assert.equal(proveCatClimbSegment(from, { ...to, y: 100 }, { speed: 180, scroll: 32, maxGap: 56 }).reason, 'invalid-gap')
  assert.equal(proveCatClimbSegment(from, { ...to, width: 2 }, {}).reason, 'invalid-geometry')
})

test('下降碰撞用环形距离，跨缝落台能接住', () => {
  const platform = { id: 3, index: 3, baseX: 4, x: 4, y: 100, width: 60, type: 'fixed', stage: 'warmup' }
  const cat = { x: 352, y: 110, vx: 0, vy: -100 }
  const hit = sweepCatClimbLanding(cat, [platform], 0.1, 0)
  assert.ok(hit, '跨缝应能落到平台')
  assert.equal(hit.id, 3)
  assert.ok(hit.x >= 0 && hit.x < C.width)
  assert.equal(sweepCatClimbLanding({ ...cat, x: 180 }, [platform], 0.1, 0), null)
})

test('200 种子 × 120 段：固定与随机路线全部通过环形可达与镜头约束', () => {
  for (const mode of ['fixed', 'random']) {
    for (let seed = 1; seed <= 200; seed++) {
      const g = createCatClimbGenerator({ seed, mode, difficultyEvery: 5 })
      let last = g.start()
      for (let i = 1; i <= 120; i++) {
        const segment = g.next()
        const proof = segment.proofs[0]
        assert.equal(proof.valid, true, `${mode}/${seed}/${i} 不可达`)
        assert.equal(proof.scrollValid, true, `${mode}/${seed}/${i} 镜头过快`)
        const p = segment.primary
        assert.ok(p.x >= 0 && p.x < C.width)
        assert.ok(p.y > last.y)
        assert.ok(p.width >= C.shortWidth || p.width >= C.movingWidth)
        assert.ok(['stair', 'zigzag', 'cross', 'rush'].includes(p.theme))
        assert.ok(CAT_CLIMB_SURFACES[p.surface], '地面材质必须合法')
        assert.ok(p.beltDir === 1 || p.beltDir === -1)
        if (p.theme === 'cross' || p.theme === 'rush') {
          assert.equal(p.surface, 'normal', '大跨度路线不能是减速地面')
        }
        last = p
      }
    }
  }
})

test('固定路线确定性、随机路线换种子、高层出现大横移与短平台', () => {
  const route = (seed, mode) => {
    const g = createCatClimbGenerator({ seed, mode, difficultyEvery: 5 })
    const out = []
    for (let i = 0; i < 120; i++) out.push(g.next().primary)
    return out
  }
  assert.deepEqual(route(1, 'fixed'), route(999, 'fixed'))
  assert.notDeepEqual(route(1, 'random'), route(2, 'random'))
  const r = route(11, 'random')
  assert.ok(r.some((p) => p.theme === 'rush'), '高层应出现穿接缝主题')
  assert.ok(r.some((p) => p.theme === 'cross'), '高层应出现跨半周主题')
  assert.ok(r.some((p) => p.width === C.shortWidth), '高层应出现短平台')
  assert.ok(r.some((p, i) => i > 0 && ringDistance(p.x, r[i - 1].x) > 140), '应出现大幅横移')
  assert.ok(r.some((p) => p.x < 20 || p.x > C.width - 20), '应有跨缝平台')
})

test('外观与运动正交：四种地面都能静止或移动，移动不再有专属条纹', () => {
  const g = createCatClimbGenerator({ seed: 11, mode: 'random', difficultyEvery: 5 })
  const rows = []
  for (let i = 0; i < 200; i++) rows.push(g.next().primary)
  const combos = new Set(rows.map((p) => `${p.surface}-${p.type}`))
  for (const s of ['normal', 'ice', 'sticky', 'belt']) {
    assert.ok(combos.has(`${s}-fixed`), `${s} 应能静止`)
    assert.ok(combos.has(`${s}-moving`), `${s} 应能移动`)
  }
  assert.ok(rows.some((p) => p.surface === 'belt' && p.beltDir === 1))
  assert.ok(rows.some((p) => p.surface === 'belt' && p.beltDir === -1))
  const vue = readFileSync(FILE, 'utf8')
  assert.ok(!vue.includes('-stripe'), '移动平台不应再有专属条纹图案')
  assert.ok(!vue.includes('url(#'), '平台外观不应由运动类型决定')
  assert.ok(vue.includes('surfaceArt(platform).fill'), '外观应由材质决定')
  assert.ok(vue.includes('data-copy="offset"'), '双边绘制应保留')
})

test('开局站着不跳，会在约 4 秒被上升的镜头甩出屏幕', () => {
  const e = createCatClimbEngine({ seed: 7 })
  let s = e.dispatch({ type: 'start' })
  assert.equal(s.cat.grounded, true)
  assert.equal(s.phase, 'running')
  const startCamera = s.cameraY
  for (let i = 0; i < 600; i++) {
    s = e.tick(1 / 60)
    if (s.phase === 'ended') break
  }
  assert.equal(s.phase, 'ended')
  assert.match(s.reason, /地面/)
  assert.ok(s.cameraY > startCamera)
  assert.ok(s.simTime > 3 && s.simTime < 6)
  near(s.cameraY, CAT_CLIMB_DIFFICULTIES[0].scroll * s.simTime, 0.5)
})

test('点按跳跃才起跳，落地回到 grounded，不再自动连跳', () => {
  const e = createCatClimbEngine({ seed: 7 })
  e.dispatch({ type: 'start' })
  let s = e.dispatch({ type: 'jump' })
  assert.equal(s.cat.grounded, false)
  assert.equal(s.cat.vy, C.jumpSpeed)
  let landed = false
  for (let i = 0; i < 300; i++) {
    s = e.tick(1 / 120)
    const target = s.platforms.find((p) => !p.launchpad && p.index === s.floor + 1)
    if (target) {
      const d = ((target.x - s.cat.x + 540) % 360) - 180
      e.dispatch({ type: 'direction', direction: d > 3 ? 1 : d < -3 ? -1 : 0 })
    }
    if (s.cat.grounded) { landed = true; break }
  }
  assert.equal(landed, true)
  assert.equal(s.cat.vy, 0)
  e.dispatch({ type: 'direction', direction: 0 })
  const frozen = s.cat.y
  for (let i = 0; i < 30; i++) s = e.tick(1 / 120)
  near(s.cat.y, frozen)
  assert.equal(s.cat.grounded, true, '落地后必须再按跳跃')
})

test('土狼时间与跳跃预输入：离台 100ms 内可跳，落地前按跳立即起跳', () => {
  const e = createCatClimbEngine({ seed: 7 })
  e.dispatch({ type: 'start' })
  e.dispatch({ type: 'direction', direction: 1 })
  let s
  for (let i = 0; i < 240; i++) {
    s = e.tick(1 / 240)
    if (!s.cat.grounded) break
  }
  assert.equal(s.cat.grounded, false, '走出平台边缘应离地')
  e.dispatch({ type: 'direction', direction: 0 })
  let after = e.dispatch({ type: 'jump' })
  assert.equal(after.cat.vy, C.jumpSpeed, '土狼时间内仍可起跳')
  // 预输入：在即将落地前按跳，落地瞬间应自动起跳
  let queued = false
  let pressed = false
  let jumpsBefore = 0
  for (let i = 0; i < 400; i++) {
    const now = e.tick(1 / 120)
    if (now.phase === 'ended') break
    const support = now.platforms.find((p) => !p.launchpad && p.index === now.floor + 1)
    if (!pressed && support && now.cat.vy < 0 && now.cat.y - support.y < 12) {
      jumpsBefore = now.jumps
      e.dispatch({ type: 'jump' })
      pressed = true
    }
    if (pressed && now.jumps > jumpsBefore && !now.cat.grounded) { queued = true; break }
  }
  assert.equal(pressed, true, '应能在落地前按下跳跃')
  assert.equal(queued, true, '预输入应在落地时兑现')
})

test('环绕坐标始终取模，走出平台边缘会离地（不会穿到界外）', () => {
  const e = createCatClimbEngine({ seed: 7 })
  e.dispatch({ type: 'start' })
  e.dispatch({ type: 'direction', direction: 1 })
  let leftPlatform = false
  for (let i = 0; i < 400; i++) {
    const s = e.tick(1 / 120)
    assert.ok(s.cat.x >= 0 && s.cat.x < C.width)
    if (!s.cat.grounded) { leftPlatform = true; break }
  }
  assert.equal(leftPlatform, true, '走到平台边缘应离地进入土狼时间')
})

test('暂停冻结镜头与时钟，恢复后继续推进', () => {
  const e = createCatClimbEngine({ seed: 7 })
  e.dispatch({ type: 'start' })
  for (let i = 0; i < 60; i++) e.tick(1 / 60)
  const before = e.snapshot()
  const paused = e.dispatch({ type: 'pause' })
  near(paused.cameraY, before.cameraY)
  for (let i = 0; i < 60; i++) e.tick(1 / 60)
  near(e.snapshot().cameraY, before.cameraY)
  assert.equal(e.snapshot().simTime, before.simTime)
  e.dispatch({ type: 'resume' })
  for (let i = 0; i < 60; i++) e.tick(1 / 60)
  assert.ok(e.snapshot().cameraY > before.cameraY)
})

test('重开回到 ready 并重置镜头；随机模式换种子、固定模式复现', () => {
  const e = createCatClimbEngine({ seed: 7 })
  e.dispatch({ type: 'start' })
  for (let i = 0; i < 120; i++) e.tick(1 / 60)
  let s = e.dispatch({ type: 'reset' })
  assert.equal(s.phase, 'ready')
  assert.equal(s.cameraY, 0)
  assert.equal(s.cat.grounded, true)
  assert.equal(s.floor, 0)
  s = e.dispatch({ type: 'reset', mode: 'fixed' })
  const fixed = s.seed
  s = e.dispatch({ type: 'reset', mode: 'fixed', seed: 999 })
  assert.equal(s.seed, fixed)
  assert.deepEqual(s.platforms.map((p) => p.x), e.snapshot().platforms.map((p) => p.x))
})

// 真实控制器：始终锁定下一层平台，落地/土狼时间内按跳。
function controlledClimb(engine, frames = 7000) {
  let s = engine.dispatch({ type: 'start' })
  let target = null
  let crossed = false
  let previous = s.cat.x
  for (let i = 0; i < frames; i++) {
    s = engine.tick(1 / 120)
    if (s.phase === 'ended') break
    if (Math.abs(s.cat.x - previous) > C.width / 2) crossed = true
    previous = s.cat.x
    const candidates = s.platforms.filter((p) => !p.launchpad && p.index === s.floor + 1)
    target = candidates[0] || target
    if (target) {
      const d = ((target.x - s.cat.x + 540) % 360) - 180
      const predicted = d - s.cat.vx * 0.22
      engine.dispatch({ type: 'direction', direction: predicted > 3 ? 1 : predicted < -3 ? -1 : 0 })
    }
    if (s.cat.grounded) engine.dispatch({ type: 'jump' })
  }
  return { ...s, crossed }
}

test('真实控制器爬过升档边界并穿过接缝；多种子都能跨过首次升档', () => {
  const main = controlledClimb(createCatClimbEngine({ seed: 1 }))
  assert.ok(main.floor >= 25, `主种子只到 ${main.floor} 层`)
  assert.equal(main.difficultyLevel, 4)
  assert.equal(main.difficultyCapped, true)
  assert.equal(main.scrollSpeed, 60)
  assert.equal(main.speed, 320)
  assert.equal(main.crossed, true, '高层应实际穿过圆柱接缝')
  for (const seed of [7, 11, 42, 99]) {
    const s = controlledClimb(createCatClimbEngine({ seed }), 4000)
    assert.ok(s.floor >= 6, `seed ${seed} 应能跨过第一次升档，实际 ${s.floor}`)
  }
})

test('地面效果延续到空中：落地切换材质标签，冰面松手仍会滑行', () => {
  const e = createCatClimbEngine({ seed: 1 })
  let s = e.dispatch({ type: 'start' })
  let target = null
  const seen = new Set()
  let iceSlide = false
  for (let i = 0; i < 6000; i++) {
    s = e.tick(1 / 120)
    if (s.phase === 'ended') break
    seen.add(s.cat.surface)
    assert.equal(s.surfaceLabel, CAT_CLIMB_SURFACES[s.cat.surface].label)
    const candidates = s.platforms.filter((p) => !p.launchpad && p.index === s.floor + 1)
    target = candidates[0] || target
    if (target) {
      const d = ((target.x - s.cat.x + 540) % 360) - 180
      const predicted = d - s.cat.vx * 0.22
      e.dispatch({ type: 'direction', direction: predicted > 3 ? 1 : predicted < -3 ? -1 : 0 })
    }
    if (s.cat.grounded) e.dispatch({ type: 'jump' })
    if (s.cat.surface === 'ice' && Math.abs(s.cat.vx) > 30) {
      e.dispatch({ type: 'direction', direction: 0 })
      const before = Math.abs(s.cat.vx)
      let after = s
      for (let k = 0; k < 12; k++) after = e.tick(1 / 120)
      if (Math.abs(after.cat.vx) > before * 0.3) iceSlide = true
      break
    }
  }
  assert.ok(seen.has('ice') && seen.has('sticky') && seen.has('belt'),
    `实际游玩应遇到多种地面，实际 ${[...seen]}`)
  assert.equal(iceSlide, true, '冰面松手后应继续滑行而不是立刻停住')
})

test('非法事件不改变状态，未知模式/种子被忽略', () => {
  const e = createCatClimbEngine({ seed: 7 })
  const before = e.snapshot()
  for (const event of [{ type: 'jump' }, { type: 'direction', direction: 9 },
    { type: 'reset', mode: 'bad' }, { type: 'reset', seed: -1 }, { type: 'other' }]) {
    e.dispatch(event)
  }
  const after = e.snapshot()
  assert.equal(after.phase, before.phase)
  assert.equal(after.seed, before.seed)
  assert.equal(after.cameraY, before.cameraY)
})

test('SFC 模板与样式编译通过，含环绕双边渲染、双向控件与文字选择防护', () => {
  const descriptor = parse(source).descriptor
  const script = compileScript(descriptor, { id: 'climb-test' })
  const template = compileTemplate({ source: descriptor.template.content, filename: FILE.pathname,
    id: 'climb-test', compilerOptions: { bindingMetadata: script.bindings } })
  assert.deepEqual(template.errors, [])
  const style = compileStyle({ source: descriptor.styles[0].content, filename: FILE.pathname,
    id: 'climb-test', scoped: true })
  assert.deepEqual(style.errors, [])
  const t = descriptor.template.content
  for (const token of ['climb-steer', 'climb-jump', 'climb-controls', 'climb-scroll', 'climb-surface',
    'surfaceArt', 'iceShine', 'iceGlint', 'stickyDot', 'beltArrows', 'platformMotion', 'squash',
    'movingHint', 'motionPhase', 'onMotionChange', 'prefers-reduced-motion', 'surfaceShort',
    'wrapOffsets', 'data-copy', 'data-motion', 'seamX', 'parallaxX', 'data-cylinder', 'data-belt',
    '@pointerdown.stop="steerDown"', '@pointerdown.stop="jumpDown"', 'state.scrollSpeed',
    'selectstart.prevent', 'dragstart.prevent']) {
    assert.ok(t.includes(token) || source.includes(token), `缺少 ${token}`)
  }
  assert.doesNotMatch(descriptor.styles[0].content, /@keyframes/, '动效必须由 simTime 驱动，不能用 CSS 无限动画')
  assert.match(style.content ?? descriptor.styles[0].content, /user-select: none; -webkit-user-select: none; -webkit-touch-callout: none;/)
  assert.match(descriptor.styles[0].content, /\.climb-steer, \.climb-jump/)
  assert.match(descriptor.styles[0].content, /display: flex; flex-direction: column;\s*padding: 0;/)
})

// 执行真实编译后的 setup；宿主只替换 DOM/RAF，不做浏览器视觉验收。
async function mountComponent() {
  const descriptor = parse(source).descriptor
  const compiled = compileScript(descriptor, { id: 'climb-host-test' }).content
    .replace(/^import[^\n]*\n/gm, '').replace('export default', 'globalThis.component =')
  const frames = new Map()
  const mountedCallbacks = []
  const unmountCallbacks = []
  const emitted = []
  const targets = []
  let now = 0
  let frameId = 0
  let disconnected = false
  function eventTarget() {
    const listeners = new Map()
    const target = { listeners, addEventListener(type, fn) {
      if (!listeners.has(type)) listeners.set(type, new Set())
      listeners.get(type).add(fn)
    }, removeEventListener(type, fn) { listeners.get(type)?.delete(fn) } }
    targets.push(target)
    return target
  }
  const document = { ...eventTarget(), hidden: false, hasFocus: () => true, activeElement: null }
  function element(className, rect = { left: 0, top: 0, right: 80, bottom: 48, width: 80, height: 48 }) {
    const captures = new Set()
    const node = { classList: { contains: (name) => name === className }, isConnected: true,
      getBoundingClientRect: () => rect,
      focus: (options) => { document.activeElement = node; node.lastFocusOptions = options },
      hasPointerCapture: (id) => captures.has(id), setPointerCapture: (id) => captures.add(id),
      releasePointerCapture: (id) => captures.delete(id), captures }
    return node
  }
  const external = element('external')
  document.activeElement = external
  const window = { ...eventTarget(), innerWidth: 360, innerHeight: 560,
    visualViewport: { ...eventTarget(), width: 360, height: 560, offsetTop: 0, offsetLeft: 0 } }
  const context = { document, window, Math: Object.assign(Object.create(Math), { random: () => 0.001 }),
    performance: { now: () => now },
    requestAnimationFrame: (fn) => { frames.set(++frameId, fn); return frameId },
    cancelAnimationFrame: (id) => frames.delete(id),
    ResizeObserver: class { observe() {} disconnect() { disconnected = true } },
    ref: (value) => ({ value }), shallowRef: (value) => ({ value }),
    computed: (fn) => ({ get value() { return fn() } }), useId: () => 'climb-host',
    nextTick: (fn) => Promise.resolve().then(fn),
    onMounted: (fn) => mountedCallbacks.push(fn), onBeforeUnmount: (fn) => unmountCallbacks.push(fn),
    CatSprite: {}, WORLD: C, CAT_CLIMB_THEME_LABELS, CAT_CLIMB_SURFACES, createCatClimbEngine }
  runInNewContext(compiled, context)
  const api = context.component.setup({ species: 'black', catName: '小猫', standalone: true },
    { expose() {}, emit: (name) => emitted.push(name) })
  const stage = element('climb-stage')
  const steer = element('climb-steer', { left: 0, top: 0, right: 120, bottom: 100, width: 120, height: 100 })
  const jump = element('climb-jump')
  const close = element('climb-close')
  const start = element('climb-start')
  api.dialog.value = { contains: () => true, querySelectorAll: () => [stage, start, jump, close] }
  api.stage.value = stage
  api.scene.value = element('scene', { width: 200, height: 300 })
  api.playfield.value = element('climb-playfield', { left: 0, top: 0, right: 360, bottom: 560, width: 360, height: 560 })
  api.startButton.value = start
  mountedCallbacks.forEach((fn) => fn())
  await Promise.resolve()
  function event(extra = {}) {
    return { preventDefault() { this.prevented = true }, stopPropagation() { this.stopped = true }, ...extra }
  }
  return { api, frames, document, window, targets, emitted, stage, steer, jump, close, start, external, event,
    unmount() { unmountCallbacks.forEach((fn) => fn()) }, isDisconnected: () => disconnected }
}

test('真实 setup：双向控件按住左半向左、滑到右半换向，松手停', async () => {
  const h = await mountComponent()
  h.api.start()
  const down = h.event({ currentTarget: h.steer, pointerId: 1, button: 0, clientX: 20, clientY: 50 })
  h.api.steerDown(down)
  assert.equal(down.prevented, true)
  assert.equal([...h.api.pointers.values()][0].direction, -1)
  h.api.pointerMove(h.event({ pointerId: 1, clientX: 100, clientY: 50 }))
  assert.equal([...h.api.pointers.values()][0].direction, 1, '滑到右半应换向')
  h.api.cancelPointer(h.event({ pointerId: 1 }))
  assert.equal(h.api.pointers.size, 0)
  h.unmount()
})

test('真实 setup：右下点按跳跃发出 jump 事件，半区兜底热区按左右屏生效', async () => {
  const h = await mountComponent()
  h.api.start()
  const before = h.api.state.value.cat.vy
  h.api.jumpDown(h.event({ pointerId: 2, button: 0 }))
  assert.ok(h.api.state.value.cat.vy > before, '跳跃应给出起跳速度')
  const field = h.api.playfield.value
  const at = (x, pointerId) => h.event({ currentTarget: field, target: field, pointerId, button: 0, clientX: x, clientY: 300 })
  h.api.playfieldPointerDown(at(60, 3))
  assert.equal(h.api.pointers.get(3).direction, -1)
  h.api.cancelPointer(h.event({ pointerId: 3 }))
  h.api.playfieldPointerDown(at(300, 4))
  assert.equal(h.api.pointers.get(4).direction, 1)
  h.api.cancelPointer(h.event({ pointerId: 4 }))
  h.api.playfieldPointerDown(h.event({ currentTarget: field, target: { closest: () => ({}) }, pointerId: 5, button: 0, clientX: 60, clientY: 300 }))
  assert.equal(h.api.pointers.has(5), false, '按在控件上不应触发兜底热区')
  h.unmount()
})

test('真实 setup：兜底热区按住左半滑到右半实时换向，不用抬手重按', async () => {
  const h = await mountComponent()
  h.api.start()
  const field = h.api.playfield.value
  h.api.playfieldPointerDown(h.event({ currentTarget: field, target: field, pointerId: 7, button: 0, clientX: 60, clientY: 300 }))
  assert.equal(h.api.pointers.get(7).direction, -1)
  h.api.pointerMove(h.event({ pointerId: 7, clientX: 300, clientY: 300 }))
  assert.equal(h.api.pointers.has(7), true, '按住期间不能把滑动误判为松手')
  assert.equal(h.api.pointers.get(7).direction, 1, '不松手滑到右半应立即换向')
  h.api.pointerMove(h.event({ pointerId: 7, clientX: 60, clientY: 300 }))
  assert.equal(h.api.pointers.get(7).direction, -1, '滑回左半应再次换向')
  h.api.cancelPointer(h.event({ pointerId: 7 }))
  assert.equal(h.api.pointers.size, 0, '抬手后方向归零')
  h.unmount()
})

test('真实 setup：控件按住滑到舞台仍实时跟随，只有彻底滑出场地才归零', async () => {
  const h = await mountComponent()
  h.api.start()
  h.api.steerDown(h.event({ currentTarget: h.steer, pointerId: 8, button: 0, clientX: 20, clientY: 50 }))
  assert.equal(h.api.pointers.get(8).direction, -1)
  // 控件 rect 为 [0,120]，中线 60；滑到舞台右侧已出控件，仍按该中线延伸判向，不应取消或突然反向。
  h.api.pointerMove(h.event({ pointerId: 8, clientX: 300, clientY: 300 }))
  assert.equal(h.api.pointers.has(8), true, '滑出控件边界不应被当作松手')
  assert.equal(h.api.pointers.get(8).direction, 1)
  h.api.pointerMove(h.event({ pointerId: 8, clientX: 10, clientY: 300 }))
  assert.equal(h.api.pointers.get(8).direction, -1, '滑到分界线左侧应向左')
  h.api.pointerMove(h.event({ pointerId: 8, clientX: 400, clientY: 300 }))
  assert.equal(h.api.pointers.size, 0, '彻底滑出游戏场地才归零')
  h.unmount()
})

test('真实 setup：分界线随按下元素固定，控件与场地各自判向不串线', async () => {
  const h = await mountComponent()
  h.api.start()
  const field = h.api.playfield.value
  h.api.playfieldPointerDown(h.event({ currentTarget: field, target: field, pointerId: 11, button: 0, clientX: 100, clientY: 300 }))
  assert.equal(h.api.pointers.get(11).direction, -1, '场地中线 180，x=100 属左半')
  h.api.cancelPointer(h.event({ pointerId: 11 }))
  h.api.steerDown(h.event({ currentTarget: h.steer, pointerId: 12, button: 0, clientX: 100, clientY: 50 }))
  assert.equal(h.api.pointers.get(12).direction, 1, '控件中线 60，同一个 x=100 属右半')
  h.unmount()
})

test('真实 setup：动效相位由 simTime 驱动，减少动效时冻结，传送带箭头铺满且朝向与滚动一致', async () => {
  const h = await mountComponent()
  h.api.start()
  assert.equal(h.api.motionPhase.value, h.api.state.value.simTime)
  h.api.reducedMotion.value = true
  assert.equal(h.api.motionPhase.value, 0, '系统减少动效时装饰相位应冻结')
  h.api.reducedMotion.value = false
  const base = { id: 5, index: 5, x: 180, baseX: 180, y: 200, width: 80, type: 'fixed' }
  const right = { ...base, surface: 'belt', beltDir: 1 }
  const left = { ...base, id: 6, surface: 'belt', beltDir: -1 }
  const nums = (d) => d.match(/-?\d+(?:\.\d+)?/g).map(Number)
  const r = h.api.beltArrows(right, 0)
  const l = h.api.beltArrows(left, 0)
  assert.ok(r.length >= 3 && l.length >= 3, '箭头应铺满整个平台宽度')
  assert.ok(nums(r[0].d)[2] > nums(r[0].d)[0], '右向传送带箭头应朝右')
  assert.ok(nums(l[0].d)[2] < nums(l[0].d)[0], '左向传送带箭头应朝左')
  h.api.state.value = { ...h.api.state.value, simTime: 0 }
  const before = h.api.beltArrows(right, 0).map((a) => a.d).join('|')
  const glint0 = h.api.iceGlint(right, 0)
  const dot0 = h.api.stickyDot(right, 0, 1).r
  h.api.state.value = { ...h.api.state.value, simTime: 0.35 }
  assert.notEqual(h.api.beltArrows(right, 0).map((a) => a.d).join('|'), before, '相位推进时箭头应滚动')
  assert.notEqual(h.api.iceGlint(right, 0), glint0, '冰面扫光应移动')
  assert.notEqual(h.api.stickyDot(right, 0, 1).r, dot0, '黏点应呼吸')
  h.unmount()
})

test('真实 setup：落地压弹只在 0.18s 内生效，静止平台微浮不动碰撞坐标', async () => {
  const h = await mountComponent()
  const platform = { id: 5, x: 180, y: 200, width: 80, type: 'fixed', surface: 'normal' }
  h.api.state.value = { ...h.api.state.value, simTime: 1, lastLanding: { id: 5, time: 1 } }
  assert.ok(h.api.squash(platform) < 1, '落地瞬间应压缩')
  h.api.state.value = { ...h.api.state.value, simTime: 1.3 }
  assert.equal(h.api.squash(platform), 1, '0.18s 后应回弹结束')
  h.api.reducedMotion.value = true
  h.api.state.value = { ...h.api.state.value, simTime: 2, lastLanding: { id: 5, time: 2 } }
  assert.equal(h.api.squash(platform), 1, '减少动效时压弹应关闭')
  assert.equal(h.api.floatY({ ...platform, type: 'moving' }), 0)
  assert.ok(Math.abs(h.api.floatY(platform)) <= 1.2, '微浮幅度必须远小于落点余量')
  assert.match(h.api.platformMotion(platform), /translate\(0 .*\) scale\(1 .*\) translate\(0/)
  h.unmount()
})

test('真实 setup：Escape 退出、卸载清理监听与焦点归还', async () => {
  const h = await mountComponent()
  h.api.keyboardDown(h.event({ key: 'Escape' }))
  assert.deepEqual(h.emitted, ['close'])
  h.unmount()
  assert.equal(h.document.activeElement, h.external)
  assert.equal(h.isDisconnected(), true)
  assert.ok(h.targets.every((target) => [...target.listeners.values()].every((set) => set.size === 0)))
})

test('真实 setup：游戏期间拦截捏合缩放手势，单指不拦，卸载后移除', async () => {
  const h = await mountComponent()
  const listeners = h.document.listeners
  for (const type of ['gesturestart', 'gesturechange', 'touchmove']) {
    assert.ok(listeners.get(type)?.size >= 1, `缺少 ${type} 监听`)
  }
  const multi = h.event({ touches: [{}, {}] })
  for (const fn of listeners.get('touchmove')) fn(multi)
  assert.equal(multi.prevented, true, '多指手势应被拦截')
  const single = h.event({ touches: [{}] })
  for (const fn of listeners.get('touchmove')) fn(single)
  assert.equal(single.prevented, undefined, '单指不应拦截，避免影响正常操作')
  const gesture = h.event({})
  for (const fn of listeners.get('gesturestart')) fn(gesture)
  assert.equal(gesture.prevented, true)
  h.unmount()
  for (const type of ['gesturestart', 'gesturechange', 'touchmove']) {
    assert.equal(listeners.get(type)?.size ?? 0, 0, `卸载后应移除 ${type}`)
  }
})

test('真实 setup：页面捏合缩放时容器固定布局尺寸且不暂停游戏', async () => {
  const h = await mountComponent()
  h.api.start()
  const vv = h.window.visualViewport
  Object.assign(vv, { scale: 1.5, width: 240, height: 373, offsetTop: 12, offsetLeft: 8 })
  h.api.updateViewport()
  assert.equal(h.api.viewport.value.width, 360, '缩放时固定用布局视口宽度')
  assert.equal(h.api.viewport.value.height, 560)
  assert.equal(h.api.viewport.value.top, 0)
  assert.equal(h.api.state.value.phase, 'running', '缩放不应暂停游戏')
  h.api.resize()
  assert.equal(h.api.state.value.phase, 'running')
  Object.assign(vv, { scale: 1, width: 360, height: 560, offsetTop: 0, offsetLeft: 0 })
  h.api.resize()
  assert.equal(h.api.state.value.phase, 'running', '缩放恢复也不应暂停')
  h.unmount()
})
