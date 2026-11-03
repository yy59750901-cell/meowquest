import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'
import { CAT_PLAY_CONFIG as C, CAT_PLAY_CHALLENGE_CONFIG as CH, CAT_PLAY_CHALLENGE_HELP,
  createCatPlayEngine, getCatPlayFeatherBounds, getCatPlayTimingEvaluation,
  expandCatPlayRect, catPlaySegmentIntersectsRect } from './cat-play.js'

function run(engine, seconds, dt = 1 / 60) {
  let remaining = seconds
  while (remaining > 1e-9) {
    const step = Math.min(dt, remaining)
    engine.tick(step)
    remaining -= step
  }
  return engine.snapshot()
}

function hold(engine, pointerId = 1) {
  const { feather } = engine.snapshot()
  return engine.dispatch({ type: 'pointerdown', pointerId, ...feather })
}

function prepare(engine) {
  hold(engine)
  return run(engine, C.stillGrace + C.chargeSeconds + 0.05)
}

function release(engine, pointerId = 1) {
  return engine.dispatch({ type: 'pointerup', pointerId })
}

function assertInside(state) {
  for (const [point, bounds] of [[state.cat, C.catBounds], [state.feather, state.bounds]]) {
    assert.ok(point.x >= bounds.left && point.x <= bounds.right)
    assert.ok(point.y >= bounds.top && point.y <= bounds.bottom)
  }
}

test('扑空自然恢复后原地重按仍能准备，不必额外拉开羽毛', () => {
  const engine = createCatPlayEngine()
  hold(engine)
  run(engine, 0.05)
  release(engine)
  const recovered = run(engine, 1)
  assert.equal(recovered.phase, 'idle')
  assert.equal(recovered.catches, 0)
  assert.match(recovered.status, /松得太早/)
  assert.equal(prepare(engine).phase, 'ready')
  release(engine)
  assert.equal(run(engine, 0.6).catches, 1)
})

test('正常按住、拖动、停住、准备、松手、扑跃、落地抓到', () => {
  const engine = createCatPlayEngine()
  hold(engine)
  engine.dispatch({ type: 'pointermove', pointerId: 1, x: 365, y: 250 })
  const first = run(engine, 0.05)
  assert.equal(first.phase, 'follow')
  assert.ok(first.cat.x > C.initialCat.x && first.cat.x < 365)
  const charging = run(engine, 0.15)
  assert.equal(charging.phase, 'charging')
  assert.ok(charging.chargeProgress > 0 && charging.chargeProgress < 1)
  const ready = run(engine, 0.55)
  assert.equal(ready.phase, 'ready')
  assert.equal(ready.chargeProgress, 1)
  assert.match(ready.status, /准备好/)
  assert.equal(release(engine).phase, 'pounce')
  const airborne = run(engine, 0.22)
  assert.equal(airborne.phase, 'pounce')
  assert.ok(airborne.cat.lift > 30)
  assert.ok(airborne.cat.x > ready.cat.x && airborne.cat.x < 365)
  assert.equal(airborne.catches, 0)
  const landed = run(engine, 0.28)
  assert.equal(landed.phase, 'caught')
  assert.equal(landed.catches, 1)
  assert.equal(landed.cat.lift, 0)
  assert.match(landed.status, /抓到/)
  run(engine, 3)
  assert.equal(engine.snapshot().catches, 1)
})

test('在扑跃距离内停止追赶，准备保持超过两秒而不追到零距离', () => {
  const engine = createCatPlayEngine()
  const ready = prepare(engine)
  assert.equal(ready.phase, 'ready')
  const held = run(engine, 5)
  assert.equal(held.phase, 'ready')
  assert.deepEqual(held.cat, ready.cat)
  const d = Math.hypot(held.cat.x - held.feather.x, held.cat.y - held.feather.y)
  assert.ok(d >= C.minPounceDistance && d <= C.maxPounceDistance)
  release(engine)
  assert.equal(run(engine, 0.5).catches, 1)
})

for (const [name, initialFeather, expected, message] of [
  ['太早', C.initialFeather, 'too-early', /太早/],
  ['太远', { x: 580, y: 278 }, 'too-far', /太远/],
  ['太近', { x: 225, y: 278 }, 'too-close', /太近/],
]) {
  test(`${name}松手给明确反馈，有限扑空且不奖励`, () => {
    const engine = createCatPlayEngine({ initialFeather })
    hold(engine)
    run(engine, 0.05)
    const before = engine.snapshot()
    const miss = release(engine)
    assert.equal(miss.phase, 'pounce')
    assert.equal(miss.result, expected)
    assert.equal(miss.evaluation, null)
    assert.equal(miss.status, '扑过去啦！')
    const after = run(engine, 0.5)
    assert.match(after.status, message)
    assert.match(after.evaluation.detail, message)
    assert.ok(Math.hypot(after.cat.x - before.cat.x, after.cat.y - before.cat.y) <= C.missMaxDistance + 1e-9)
    assert.equal(after.catches, 0)
    assert.equal(after.phase, 'recover')
    assertInside(after)
    assert.equal(run(engine, 0.4).phase, 'idle')
  })
}

test('蓄势中松手不会算成功', () => {
  const engine = createCatPlayEngine()
  hold(engine)
  assert.equal(run(engine, 0.3).phase, 'charging')
  assert.equal(release(engine).result, 'too-early')
  assert.equal(run(engine, 1).catches, 0)
})

test('准备好后显著移动羽毛会重置，小幅抖动不重置且累计移动不能绕过阈值', () => {
  const engine = createCatPlayEngine()
  const ready = prepare(engine)
  engine.dispatch({ type: 'pointermove', pointerId: 1, x: ready.feather.x + 4, y: ready.feather.y })
  assert.equal(engine.snapshot().phase, 'ready')
  engine.dispatch({ type: 'pointermove', pointerId: 1, x: ready.feather.x + 9, y: ready.feather.y })
  assert.equal(engine.snapshot().phase, 'ready')
  const moved = engine.dispatch({ type: 'pointermove', pointerId: 1, x: ready.feather.x + 20, y: ready.feather.y })
  assert.equal(moved.phase, 'follow')
  assert.equal(moved.chargeProgress, 0)
  assert.equal(release(engine).result, 'too-early')
})

test('蓄势期间移走羽毛会重新计时', () => {
  const engine = createCatPlayEngine()
  hold(engine)
  run(engine, 0.4)
  const { feather } = engine.snapshot()
  engine.dispatch({ type: 'pointermove', pointerId: 1, x: feather.x + 15, y: feather.y })
  assert.equal(engine.snapshot().phase, 'follow')
  assert.equal(run(engine, 0.4).phase, 'charging')
  assert.equal(run(engine, 0.35).phase, 'ready')
})

test('不按住时不会自动准备、自动抓到或因 pointerup 获得奖励', () => {
  const engine = createCatPlayEngine()
  const initial = engine.snapshot()
  run(engine, 30)
  assert.equal(engine.snapshot().phase, 'idle')
  assert.deepEqual(engine.snapshot().cat, initial.cat)
  assert.deepEqual(engine.snapshot().feather, initial.feather)
  assert.equal(release(engine).phase, 'idle')
  assert.equal(engine.snapshot().catches, 0)
})

test('pointercancel 不触发扑跃，不保留准备', () => {
  const engine = createCatPlayEngine()
  prepare(engine)
  const canceled = engine.dispatch({ type: 'pointercancel', pointerId: 1 })
  assert.equal(canceled.phase, 'idle')
  assert.equal(canceled.held, false)
  release(engine)
  assert.equal(run(engine, 3).catches, 0)
  hold(engine)
  assert.equal(engine.snapshot().phase, 'follow')
})

for (const reason of ['blur', 'visibilitychange', 'pause']) {
  test(`${reason} 清除准备、冻结时间，恢复不保留旧 ready`, () => {
    const engine = createCatPlayEngine()
    prepare(engine)
    const paused = engine.dispatch({ type: reason })
    assert.equal(paused.phase, 'paused')
    assert.equal(paused.held, false)
    assert.deepEqual(run(engine, 4), paused)
    engine.dispatch({ type: 'resume' })
    assert.equal(engine.snapshot().phase, 'idle')
    release(engine)
    assert.equal(run(engine, 1).catches, 0)
    assert.equal(hold(engine).phase, 'follow')
  })
  test(`${reason} 在扑跃中取消尚未落地的抓取，不补发奖励`, () => {
    const engine = createCatPlayEngine()
    prepare(engine)
    release(engine)
    run(engine, 0.2)
    engine.dispatch({ type: reason })
    engine.dispatch({ type: 'resume' })
    const state = run(engine, 2)
    assert.equal(state.catches, 0)
    assert.equal(state.cat.lift, 0)
    assert.equal(state.phase, 'idle')
  })
}

test('双指不切换主 pointer，副指移动、松开、取消均忽略', () => {
  const engine = createCatPlayEngine()
  hold(engine, 7)
  engine.dispatch({ type: 'pointerdown', pointerId: 8, x: 36, y: 150 })
  engine.dispatch({ type: 'pointermove', pointerId: 8, x: 36, y: 150 })
  engine.dispatch({ type: 'pointercancel', pointerId: 8 })
  engine.dispatch({ type: 'pointerup', pointerId: 8 })
  assert.equal(engine.snapshot().pointerId, 7)
  assert.deepEqual(engine.snapshot().feather, C.initialFeather)
  assert.equal(run(engine, 0.75).phase, 'ready')
  release(engine, 7)
  assert.equal(run(engine, 0.5).catches, 1)
})

test('多回合自然恢复，有效抓到次数只在本次累加，重新开始清零', () => {
  const engine = createCatPlayEngine()
  for (let round = 1; round <= 5; round += 1) {
    assert.equal(prepare(engine).phase, 'ready')
    release(engine)
    assert.equal(run(engine, 0.5).catches, round)
    const next = run(engine, 0.85)
    assert.equal(next.phase, 'idle')
    const d = Math.hypot(next.feather.x - next.cat.x, next.feather.y - next.cat.y)
    assert.ok(d >= C.minPounceDistance && d <= C.maxPounceDistance)
    assertInside(next)
  }
  const reset = engine.dispatch({ type: 'reset' })
  assert.equal(reset.catches, 0)
  assert.equal(reset.phase, 'idle')
  assert.deepEqual(reset.feather, C.initialFeather)
  assert.equal(reset.cat.x, C.initialCat.x)
})

test('抓住或恢复期间能立即开始下一次拖动，扑跃中忽略新按下', () => {
  const engine = createCatPlayEngine()
  prepare(engine)
  release(engine)
  assert.equal(hold(engine, 2).held, false)
  run(engine, 0.5)
  const { cat } = engine.snapshot()
  const next = engine.dispatch({ type: 'pointerdown', pointerId: 2, x: cat.x - 120, y: cat.y })
  assert.equal(next.phase, 'follow')
  assert.equal(run(engine, 0.75).phase, 'ready')
  release(engine, 2)
  assert.equal(run(engine, 0.5).catches, 2)
})

test('拖动超出边界被裁切，跟随和扑空均不出场地', () => {
  for (const [x, y] of [[-999, -999], [9999, -999], [-999, 9999], [9999, 9999]]) {
    const engine = createCatPlayEngine()
    hold(engine)
    engine.dispatch({ type: 'pointermove', pointerId: 1, x, y })
    for (let i = 0; i < 25; i += 1) assertInside(engine.tick(0.1))
    release(engine)
    for (let i = 0; i < 10; i += 1) assertInside(engine.tick(0.1))
  }
})

test('准备后越过距离边界松开仍判定太近或太远', () => {
  for (const [initialDistance, move, result] of [[75, -12, 'too-close'], [170, 12, 'too-far']]) {
    const engine = createCatPlayEngine({ initialFeather: { x: C.initialCat.x + initialDistance, y: C.initialCat.y } })
    const ready = prepare(engine)
    assert.equal(ready.phase, 'ready')
    const moved = engine.dispatch({ type: 'pointermove', pointerId: 1, x: ready.feather.x + move, y: ready.feather.y })
    assert.equal(moved.phase, 'follow')
    assert.equal(moved.chargeProgress, 0)
    assert.match(moved.status, result === 'too-close' ? /太近/ : /太远/)
    assert.equal(release(engine).result, result)
  }
})

test('长 dt 被截断，不瞬移、不直接准备，非法 dt 被忽略', () => {
  const engine = createCatPlayEngine()
  hold(engine)
  const before = engine.snapshot()
  for (const dt of [NaN, Infinity, -1, 0]) assert.deepEqual(engine.tick(dt), before)
  const after = engine.tick(999)
  assert.equal(after.phase, 'follow')
  assert.ok(Math.hypot(after.cat.x - before.cat.x, after.cat.y - before.cat.y) <= C.followSpeed * C.maxDt + 1e-9)
  assert.equal(after.catches, 0)
})

test('30、60、120Hz 和不整齐 dt 对相同输入具有一致跟随、阈值和扑跃时序', () => {
  const states = []
  for (const dt of [1 / 30, 1 / 60, 1 / 120, 0.013]) {
    const engine = createCatPlayEngine()
    hold(engine)
    engine.dispatch({ type: 'pointermove', pointerId: 1, x: 430, y: 290 })
    const following = run(engine, 0.2, dt)
    const charging = run(engine, 0.4, dt)
    const ready = run(engine, 0.65, dt)
    assert.equal(ready.phase, 'ready')
    release(engine)
    const jumping = run(engine, 0.25, dt)
    const caught = run(engine, 0.25, dt)
    states.push([following, charging, ready, jumping, caught])
  }
  for (const state of states.slice(1)) assert.deepEqual(state, states[0])
})

test('状态快照不允许调用方意外修改引擎位置', () => {
  const engine = createCatPlayEngine()
  const snapshot = engine.snapshot()
  snapshot.cat.x = -100
  snapshot.feather.y = -100
  snapshot.catches = 999
  snapshot.bounds.left = -100
  assert.equal(engine.snapshot().bounds.left, C.featherBounds.left)
  assert.equal(engine.snapshot().cat.x, C.initialCat.x)
  assert.equal(engine.snapshot().feather.y, C.initialFeather.y)
  assert.equal(engine.snapshot().catches, 0)
})

for (const [name, initialDistance, move, message] of [
  ['近边界 61 左移 12', 61, -12, /太近/],
  ['远边界 159 右移 12', 159, 12, /太远/],
]) {
  test(`ready 小幅抖动越界立即取消绿灯：${name}`, () => {
    const engine = createCatPlayEngine({ followSpeed: 0, initialFeather: { x: C.initialCat.x + initialDistance, y: C.initialCat.y } })
    const ready = prepare(engine)
    assert.equal(ready.phase, 'ready')
    const moved = engine.dispatch({ type: 'pointermove', pointerId: 1, x: ready.feather.x + move, y: ready.feather.y })
    assert.equal(moved.phase, 'follow')
    assert.equal(moved.chargeProgress, 0)
    assert.equal(moved.stillSeconds, 0)
    assert.match(moved.status, message)
    for (let i = 0; i < 20; i += 1) {
      const next = engine.tick(0.1)
      assert.equal(next.phase, 'follow')
      assert.equal(next.chargeProgress, 0)
      assert.match(next.status, message)
    }
    release(engine)
    assert.equal(run(engine, 0.5).catches, 0)
  })
}

test('ready 抖动到恰好 60 或 160 仍有效，越界后移回须重新蓄势', () => {
  for (const [initialDistance, move] of [[61, -1], [159, 1]]) {
    const engine = createCatPlayEngine({ followSpeed: 0, initialFeather: { x: C.initialCat.x + initialDistance, y: C.initialCat.y } })
    const ready = prepare(engine)
    engine.dispatch({ type: 'pointermove', pointerId: 1, x: ready.feather.x + move, y: ready.feather.y })
    assert.equal(run(engine, 0.2).phase, 'ready')
    engine.dispatch({ type: 'pointermove', pointerId: 1, x: ready.feather.x + move * 12, y: ready.feather.y })
    assert.equal(engine.snapshot().phase, 'follow')
    engine.dispatch({ type: 'pointermove', pointerId: 1, ...ready.feather })
    assert.equal(engine.snapshot().phase, 'follow')
    assert.equal(run(engine, 0.2).phase, 'charging')
    assert.equal(run(engine, 0.55).phase, 'ready')
  }
})

for (const pointerId of [2, 'keyboard']) {
  for (const phase of ['caught', 'recover']) {
    test(`${phase} 重按旧羽毛坐标先放好下一轮，${pointerId} 无需拖远即可再抓`, () => {
      const engine = createCatPlayEngine()
      prepare(engine)
      release(engine)
      const caught = run(engine, 0.5)
      const oldFeather = { ...caught.feather }
      if (phase === 'recover') run(engine, 0.5)
      assert.equal(engine.snapshot().phase, phase)
      const next = engine.dispatch({ type: 'pointerdown', pointerId, ...oldFeather })
      assert.equal(next.phase, 'follow')
      assert.notDeepEqual(next.feather, oldFeather)
      const d = Math.hypot(next.cat.x - next.feather.x, next.cat.y - next.feather.y)
      assert.ok(d >= C.minPounceDistance && d <= C.maxPounceDistance)
      // 与组件一样按处理后的快照计算偏移，同一手指坐标不会拖回旧位置。
      const offset = { x: oldFeather.x - next.feather.x, y: oldFeather.y - next.feather.y }
      const samePoint = engine.dispatch({ type: 'pointermove', pointerId, x: oldFeather.x - offset.x, y: oldFeather.y - offset.y })
      assert.deepEqual(samePoint.feather, next.feather)
      assert.equal(run(engine, 0.75).phase, 'ready')
      release(engine, pointerId)
      assert.equal(run(engine, 0.5).catches, 2)
    })
  }
}

function assertPixelMargin(state, width, height) {
  assertInside(state)
  const x = state.feather.x / C.width * width
  const y = state.feather.y / C.height * height
  assert.ok(Math.min(x, width - x, y, height - y) >= C.featherEdgeMargin - 1e-8)
}

for (const [width, height] of [[332, 207.5], [320, 200], [720, 450], [960, 600]]) {
  test(`${width}×${height} 场地输入与自动抱羽毛、下一轮、重置都保留完整热区`, () => {
    const bounds = getCatPlayFeatherBounds(width, height)
    for (const x of [80, 560]) {
      const engine = createCatPlayEngine({ initialCat: { x: x < 320 ? 220 : 420, y: 278 }, initialFeather: { x, y: 310 }, featherBounds: bounds })
      assertPixelMargin(engine.snapshot(), width, height)
      hold(engine)
      engine.dispatch({ type: 'pointermove', pointerId: 1, x: -9999, y: 9999 })
      assertPixelMargin(engine.snapshot(), width, height)
      engine.dispatch({ type: 'pointermove', pointerId: 1, x: 9999, y: -9999 })
      assertPixelMargin(engine.snapshot(), width, height)
      engine.dispatch({ type: 'reset' })
      for (let round = 1; round <= 4; round += 1) {
        assert.equal(prepare(engine).phase, 'ready')
        release(engine)
        for (let i = 0; i < 15; i += 1) assertPixelMargin(engine.tick(0.1), width, height)
        assert.equal(engine.snapshot().catches, round)
      }
      assertPixelMargin(engine.dispatch({ type: 'reset' }), width, height)
      assert.deepEqual(engine.snapshot().bounds, bounds)
    }
  })
}

test('运行时 resize 夹住已有羽毛、取消旧准备和主 pointer，重置仍使用同一边界', () => {
  const engine = createCatPlayEngine()
  prepare(engine)
  const bounds = { left: 380, right: 520, top: 200, bottom: 320 }
  const resized = engine.resize({ bounds })
  assert.equal(resized.phase, 'idle')
  assert.equal(resized.held, false)
  assert.equal(resized.pointerId, null)
  assert.equal(resized.chargeProgress, 0)
  assert.match(resized.status, /场地大小/)
  assert.equal(resized.feather.x, bounds.left)
  assertInside(resized)
  release(engine)
  assert.equal(run(engine, 1).catches, 0)
  const reset = engine.dispatch({ type: 'reset' })
  assert.equal(reset.feather.x, bounds.left)
  assert.deepEqual(reset.bounds, bounds)
})

test('resize 不补发飞行中抓取、不清除已有次数，暂停保持暂停', () => {
  const engine = createCatPlayEngine()
  prepare(engine)
  release(engine)
  run(engine, 1.4)
  prepare(engine)
  release(engine)
  run(engine, 0.2)
  engine.resize({ bounds: getCatPlayFeatherBounds(332, 207.5) })
  assert.equal(run(engine, 1).catches, 1)
  engine.dispatch({ type: 'pause' })
  const paused = engine.resize({ bounds: getCatPlayFeatherBounds(320, 200) })
  assert.equal(paused.phase, 'paused')
  assert.deepEqual(run(engine, 1), paused)
  engine.dispatch({ type: 'resume' })
  assert.equal(engine.snapshot().phase, 'idle')
})

test('resize 同边界幂等，外部边界对象不影响状态，输入边界不会扩出地板', () => {
  const engine = createCatPlayEngine()
  const bounds = getCatPlayFeatherBounds(332, 207.5)
  engine.resize({ bounds })
  const ready = prepare(engine)
  assert.deepEqual(engine.resize({ bounds: { ...bounds } }), ready)
  bounds.left = -500
  assert.deepEqual(engine.snapshot(), ready)
  const floor = engine.dispatch({ type: 'resize', bounds: { left: -999, right: 9999, top: -999, bottom: 9999 } })
  assert.deepEqual(floor.bounds, C.featherBounds)
  assertInside(floor)
  for (const invalid of [{ left: 800, right: 900, top: 200, bottom: 300 }, { ...C.featherBounds, top: NaN }, null]) {
    assert.throws(() => engine.resize({ bounds: invalid }), /羽毛边界/)
    assert.deepEqual(engine.snapshot(), floor)
  }
  for (const dimensions of [[0, 400], [640, Infinity], [NaN, 200], [60, 40]]) {
    assert.throws(() => getCatPlayFeatherBounds(...dimensions), /场地尺寸/)
  }
})

const componentSource = readFileSync(new URL('./components/CatPlayground.vue', import.meta.url), 'utf8')

// 直接执行组件输入函数；只模拟宿主接口，不替代主代理的真实浏览器验收。
function componentHarness() {
  const timers = new Map()
  const frames = new Map()
  const listeners = new Map()
  const emissions = []
  const hooks = {}
  let nextId = 1
  let now = 0
  let rect = { left: 0, top: 0, width: 332, height: 207.5 }
  const document = { hidden: false, activeElement: null, hasFocus: () => true }
  const target = (name) => ({
    isConnected: true, focusCalls: 0,
    focus() { this.focusCalls += 1; document.activeElement = this },
    name,
  })
  const previous = target('进入前按钮')
  document.activeElement = previous
  const close = target('退出')
  const feather = target('羽毛')
  const captures = new Set()
  feather.setPointerCapture = (id) => captures.add(id)
  feather.hasPointerCapture = (id) => captures.has(id)
  feather.releasePointerCapture = (id) => captures.delete(id)
  const listenerHost = (prefix) => ({
    addEventListener: (type, fn) => listeners.set(`${prefix}:${type}`, fn),
    removeEventListener: (type, fn) => { if (listeners.get(`${prefix}:${type}`) === fn) listeners.delete(`${prefix}:${type}`) },
  })
  Object.assign(document, listenerHost('document'))
  const context = {
    WORLD: C, CHALLENGE: CH, CAT_PLAY_HELP: '拖动羽毛，停一停，看到它准备好再松手', CAT_PLAY_CHALLENGE_HELP, createCatPlayEngine, getCatPlayFeatherBounds,
    ref: (value) => ({ value }), shallowRef: (value) => ({ value }), computed: (get) => ({ get value() { return get() } }),
    useId: () => '单测', defineProps: () => ({}), defineEmits: () => (event) => emissions.push(event),
    onMounted: (fn) => { hooks.mount = fn }, onBeforeUnmount: (fn) => { hooks.cleanup = fn },
    document, window: listenerHost('window'), performance: { now: () => now },
    ResizeObserver: class { observe() {} disconnect() { hooks.disconnected = true } },
    setTimeout: (fn) => { const id = nextId++; timers.set(id, fn); return id },
    clearTimeout: (id) => timers.delete(id),
    requestAnimationFrame: (fn) => { const id = nextId++; frames.set(id, fn); return id },
    cancelAnimationFrame: (id) => frames.delete(id),
  }
  const script = componentSource.match(/<script setup>([\s\S]*?)<\/script>/)[1].replace(/^import .*$/gm, '')
  const ui = runInNewContext(`${script}\n;({ state, engine, stage, dialog, featherButton, closeButton, stageSize, featherStyle, inputBlocked, pointerDown, pointerMove, pointerUp, keyboardDown, keyboardUp, accessibleClick, syncBounds, requestClose, cancelDrag, switchMode, restart, togglePause, manualPaused, phaseLabel, challengeProgress, dialogKeydown })`, context)
  ui.stage.value = { getBoundingClientRect: () => rect }
  ui.dialog.value = { querySelectorAll: () => [close, feather], contains: (value) => value === close || value === feather }
  ui.featherButton.value = feather
  ui.closeButton.value = close
  hooks.mount()
  return {
    ...ui, document, close, feather, captures, timers, frames, listeners, emissions,
    get now() { return now },
    setTime(timestamp) { now = timestamp },
    frameAt(timestamp, performanceTime = timestamp) {
      now = performanceTime
      const next = frames.entries().next().value
      if (next) {
        frames.delete(next[0])
        next[1](timestamp)
      }
      return ui.state.value
    },
    setSize(width, height) { rect = { ...rect, width, height }; ui.syncBounds(true) },
    flushTimers() { for (const [id, fn] of [...timers]) { timers.delete(id); fn() } },
    point(point, pointerId = 1) {
      return { clientX: point.x / C.width * rect.width, clientY: point.y / C.height * rect.height,
        pointerId, button: 0, isPrimary: true, preventDefault() {} }
    },
    cleanup() { hooks.cleanup() },
  }
}

function componentRun(ui, seconds) {
  let remaining = seconds
  while (remaining > 1e-9) {
    const dt = Math.min(1 / 60, remaining)
    ui.frameAt(ui.now + dt * 1000)
    remaining -= dt
  }
  return ui.state.value
}

const spaceEvent = (repeat = false) => ({ code: 'Space', key: ' ', repeat, preventDefault() { this.prevented = true } })
const clickEvent = () => ({ detail: 0, preventDefault() {} })

test('组件 Space 多回合仅一按一松，忽略 repeat 与 keyup 后 ghost click，扑跃不抢退出焦点', (t) => {
  const ui = componentHarness()
  t.after(() => ui.cleanup())
  ui.feather.focus()
  assert.match(componentSource, /:aria-disabled="inputBlocked"/)
  assert.doesNotMatch(componentSource, /:disabled=/)
  for (let round = 1; round <= 3; round += 1) {
    const down = spaceEvent()
    ui.keyboardDown(down)
    assert.equal(down.prevented, true)
    assert.equal(ui.state.value.held, true)
    ui.accessibleClick(clickEvent())
    assert.equal(ui.state.value.held, true)
    ui.keyboardDown(spaceEvent(true))
    assert.equal(componentRun(ui, 0.75).phase, 'ready')
    const up = spaceEvent()
    ui.keyboardUp(up)
    assert.equal(up.prevented, true)
    assert.equal(ui.inputBlocked.value, true)
    ui.accessibleClick(clickEvent())
    ui.keyboardUp(spaceEvent())
    const caught = componentRun(ui, 0.5)
    assert.equal(caught.catches, round)
    ui.accessibleClick(clickEvent())
    assert.equal(ui.state.value.phase, 'caught')
    assert.equal(ui.document.activeElement, ui.feather)
    ui.flushTimers()
  }
  ui.close.focus()
  componentRun(ui, 1)
  assert.equal(ui.document.activeElement, ui.close)
  assert.equal(ui.feather.focusCalls, 1)
})

test('组件 caught 重按触控旧热区，以新羽毛计算偏移，同点 move/up 不跳回旧位置', (t) => {
  const ui = componentHarness()
  t.after(() => ui.cleanup())
  const firstPoint = ui.point(ui.state.value.feather)
  ui.pointerDown(firstPoint)
  componentRun(ui, 0.75)
  ui.pointerUp(firstPoint)
  componentRun(ui, 0.5)
  const oldFeather = { ...ui.state.value.feather }
  const oldPoint = ui.point(oldFeather, 9)
  oldPoint.clientX += 20
  oldPoint.clientY += 15
  ui.pointerDown(oldPoint)
  const nextFeather = { ...ui.state.value.feather }
  assert.notDeepEqual(nextFeather, oldFeather)
  ui.pointerMove(oldPoint)
  assert.deepEqual(ui.state.value.feather, nextFeather)
  ui.pointerDown({ ...oldPoint, pointerId: 10 })
  assert.equal(ui.state.value.pointerId, 9)
  assert.deepEqual([...ui.captures], [9])
  assert.equal(componentRun(ui, 0.75).phase, 'ready')
  ui.pointerUp(oldPoint)
  assert.equal(componentRun(ui, 0.5).catches, 2)
  assert.equal(ui.captures.size, 0)
})

test('组件同步尺寸保持 60px border-box，resize 取消准备，重复关闭只 emit 一次且清理资源', () => {
  const ui = componentHarness()
  assert.equal(ui.featherStyle.value.width, '60px')
  assert.equal(ui.featherStyle.value.height, '60px')
  assert.match(componentSource, /\.play-feather\s*\{[^}]*box-sizing: border-box/)
  assertPixelMargin(ui.state.value, 332, 207.5)
  ui.keyboardDown(spaceEvent())
  componentRun(ui, 0.75)
  ui.setSize(320, 200)
  assert.equal(ui.state.value.phase, 'idle')
  assert.equal(ui.state.value.held, false)
  assert.equal(ui.stageSize.value.width, 320)
  assertPixelMargin(ui.state.value, 320, 200)
  ui.keyboardUp(spaceEvent())
  assert.equal(componentRun(ui, 1).catches, 0)
  ui.requestClose()
  ui.requestClose()
  assert.deepEqual(ui.emissions, ['close'])
  assert.equal(ui.frames.size, 0)
  ui.cleanup()
  assert.equal(ui.timers.size, 0)
  assert.equal(ui.listeners.size, 0)
  assert.equal(ui.document.activeElement.name, '进入前按钮')
})

const landingSeconds = Math.ceil(C.pounceSeconds / C.fixedStep) * C.fixedStep

function reachReady(engine) {
  for (let i = 0; i < 1200; i += 1) {
    if (engine.snapshot().phase === 'ready') return engine.snapshot()
    engine.tick(C.fixedStep)
  }
  assert.fail(`没有准备好：${engine.snapshot().status}`)
}

for (const [seconds, grade] of [
  [0, 'perfect'], [0.6 - 1e-7, 'perfect'], [0.6, 'perfect'], [0.6 + 1e-10, 'perfect'],
  [0.6 + 1e-7, 'good'], [1.5 - 1e-7, 'good'], [1.5, 'good'], [1.5 + 1e-10, 'good'],
  [1.5 + 1e-7, 'late'], [30, 'late'],
]) {
  test(`时机边界 ${seconds}s → ${grade}，浮点微差不抖动`, () => {
    const evaluation = getCatPlayTimingEvaluation(seconds)
    assert.equal(evaluation.grade, grade)
    assert.deepEqual(Object.keys(evaluation).sort(), ['detail', 'grade', 'title'])
  })
}

for (const [seconds, grade, title] of [
  [0, 'perfect', '时机特别好！'], [0.6, 'perfect', '时机特别好！'],
  [0.6 + C.fixedStep, 'good', '不错，抓到了！'], [1.5, 'good', '不错，抓到了！'],
  [1.5 + C.fixedStep, 'late', '慢了一点，还是抓到了！'], [5, 'late', '慢了一点，还是抓到了！'],
]) {
  test(`真实 ready 经过 ${seconds}s：仅在约 0.48s 落地发布 ${grade}`, () => {
    for (const dt of [1 / 30, 1 / 60, 1 / 120, 0.013]) {
      const engine = createCatPlayEngine()
      hold(engine)
      const ready = reachReady(engine)
      assert.equal(ready.readySeconds, 0)
      assert.ok(ready.stillSeconds >= C.chargeSeconds)
      assert.equal(ready.evaluation, null)
      const waiting = run(engine, seconds, dt)
      assert.ok(Math.abs(waiting.readySeconds - seconds) < 1e-8)
      assert.equal(waiting.status, ready.status)
      assert.equal(release(engine).evaluation, null)
      assert.equal(run(engine, landingSeconds - C.fixedStep, dt).evaluation, null)
      const landed = run(engine, C.fixedStep, dt)
      assert.equal(landed.phase, 'caught')
      assert.deepEqual(landed.evaluation, getCatPlayTimingEvaluation(seconds))
      assert.equal(landed.evaluation.title, title)
      assert.equal(landed.catches, 1)
      assert.deepEqual(run(engine, 3).evaluation, landed.evaluation)
    }
  })
}

for (const [initialFeather, grade, title] of [
  [C.initialFeather, 'early', '快了！'], [{ x: 580, y: 278 }, 'far', '羽毛太远啦！'],
  [{ x: 225, y: 278 }, 'close', '羽毛太近啦！'],
]) {
  test(`未 ready 松手区分 ${grade}，不把空间原因冒充时机`, () => {
    const engine = createCatPlayEngine({ initialFeather })
    hold(engine)
    run(engine, 0.05)
    assert.equal(release(engine).evaluation, null)
    const landed = run(engine, 0.5)
    assert.equal(landed.evaluation.grade, grade)
    assert.equal(landed.evaluation.title, title)
    assert.equal(landed.catches, 0)
  })
}

for (const reason of ['cancel', 'pointercancel', 'blur', 'visibilitychange', 'pause', 'resize', 'set-mode', 'reset']) {
  test(`${reason} 中止飞行不生成评语或补抓取`, () => {
    const engine = createCatPlayEngine()
    prepare(engine)
    release(engine)
    run(engine, 0.2)
    engine.dispatch({ type: reason, pointerId: 1, mode: 'challenge', bounds: getCatPlayFeatherBounds(320, 200) })
    engine.dispatch({ type: 'resume' })
    const after = run(engine, 2)
    assert.equal(after.evaluation, null)
    assert.equal(after.catches, 0)
    assert.equal(after.cat.lift, 0)
  })
}

test('已落地评语保留至新按下、reset 或切模式；暂停、取消与 resize 不抹掉', () => {
  const engine = createCatPlayEngine()
  prepare(engine)
  release(engine)
  const evaluation = run(engine, 0.5).evaluation
  for (const reason of ['pointercancel', 'blur', 'visibilitychange', 'pause', 'resize']) {
    engine.dispatch({ type: reason, pointerId: 1, bounds: getCatPlayFeatherBounds(320, 200) })
    engine.dispatch({ type: 'resume' })
    assert.deepEqual(run(engine, 1).evaluation, evaluation)
  }
  assert.equal(hold(engine).evaluation, null)
  run(engine, 0.75)
  release(engine)
  run(engine, 0.5)
  assert.equal(engine.dispatch({ type: 'reset' }).evaluation, null)
  prepare(engine)
  release(engine)
  run(engine, 0.5)
  assert.equal(engine.dispatch({ type: 'set-mode', mode: 'challenge' }).evaluation, null)
})

test('ready 计时随真实小抖动继续，累计移出锚点重置后不继承旧秒数', () => {
  const engine = createCatPlayEngine()
  hold(engine)
  const ready = reachReady(engine)
  run(engine, 0.5)
  engine.dispatch({ type: 'pointermove', pointerId: 1, x: ready.feather.x + 4, y: ready.feather.y })
  run(engine, 0.5)
  engine.dispatch({ type: 'pointermove', pointerId: 1, x: ready.feather.x + 9, y: ready.feather.y })
  assert.ok(Math.abs(engine.snapshot().readySeconds - 1) < 1e-8)
  const moved = engine.dispatch({ type: 'pointermove', pointerId: 1, x: ready.feather.x + 20, y: ready.feather.y })
  assert.equal(moved.phase, 'follow')
  assert.equal(moved.readySeconds, 0)
  assert.equal(release(engine).evaluation, null)
  assert.equal(run(engine, 0.5).evaluation.grade, 'early')
  hold(engine)
  assert.equal(reachReady(engine).readySeconds, 0)
  run(engine, 0.7)
  release(engine)
  assert.equal(run(engine, 0.5).evaluation.grade, 'good')
})

test('长帧仅累计 maxDt，后台恢复既不继承 ready 也不凭旧按住时间造 perfect', () => {
  const engine = createCatPlayEngine()
  hold(engine)
  reachReady(engine)
  assert.ok(Math.abs(engine.tick(999).readySeconds - C.maxDt) < 1e-8)
  engine.dispatch({ type: 'blur' })
  assert.equal(run(engine, 60).readySeconds, 0)
  engine.dispatch({ type: 'resume' })
  release(engine)
  assert.equal(run(engine, 1).evaluation, null)
  hold(engine)
  release(engine)
  assert.equal(run(engine, 0.5).evaluation.grade, 'early')
})

test('set-mode 重置位置、会话次数与挑战进度；暂停模式切换和 reset 保持暂停', () => {
  const engine = createCatPlayEngine()
  assert.equal(engine.snapshot().mode, 'free')
  prepare(engine)
  release(engine)
  run(engine, 0.5)
  engine.dispatch({ type: 'pause' })
  const challenge = engine.dispatch({ type: 'set-mode', mode: 'challenge' })
  assert.equal(challenge.phase, 'paused')
  assert.equal(challenge.pointerId, null)
  assert.equal(challenge.catches, 0)
  assert.equal(challenge.evaluation, null)
  assert.equal(challenge.challenge.checkpointVisited, false)
  assert.equal(challenge.challenge.completed, false)
  assert.equal(challenge.cat.x, CH.initialCat.x)
  assert.equal(challenge.cat.y, CH.initialCat.y)
  assert.deepEqual(challenge.feather, CH.initialFeather)
  assert.deepEqual(run(engine, 5), challenge)
  assert.equal(hold(engine).held, false)
  assert.equal(engine.dispatch({ type: 'reset' }).phase, 'paused')
  const free = engine.dispatch({ type: 'set-mode', mode: 'free' })
  assert.equal(free.phase, 'paused')
  assert.equal(free.challenge, null)
  assert.deepEqual(engine.dispatch({ type: 'set-mode', mode: 'unknown' }), free)
  engine.dispatch({ type: 'resume' })
  assert.equal(prepare(engine).phase, 'ready')
  release(engine)
  assert.equal(run(engine, 0.5).catches, 1)
})

const expandedRug = expandCatPlayRect(CH.rug, CH.catRadius)

test('扩框线段碰撞包含斜穿、擦边、顶点、平行和零长度，端点均在外也不能穿', () => {
  assert.deepEqual(expandedRug, { left: 254, right: 386, top: 230, bottom: 334 })
  for (const [from, to, hits] of [
    [{ x: 220, y: 292 }, { x: 430, y: 292 }, true],
    [{ x: 220, y: 200 }, { x: 430, y: 360 }, true],
    [{ x: 200, y: 230 }, { x: 430, y: 230 }, true],
    [{ x: 200, y: 230 - 1e-6 }, { x: 430, y: 230 - 1e-6 }, false],
    [{ x: 254, y: 180 }, { x: 254, y: 360 }, true],
    [{ x: 253, y: 180 }, { x: 253, y: 360 }, false],
    [{ x: 230, y: 254 }, { x: 278, y: 206 }, true],
    [{ x: 300, y: 280 }, { x: 300, y: 280 }, true],
    [{ x: 230, y: 200 }, { x: 230, y: 200 }, false],
    [{ x: 400, y: 280 }, { x: 500, y: 280 }, false],
  ]) {
    assert.equal(catPlaySegmentIntersectsRect(from, to, expandedRug), hits)
    assert.equal(catPlaySegmentIntersectsRect(to, from, expandedRug), hits)
  }
})

function runSafe(engine, seconds, dt = 1 / 60) {
  let remaining = seconds
  while (remaining > 1e-9) {
    const before = engine.snapshot().cat
    const step = Math.min(dt, remaining)
    const after = engine.tick(step)
    assertInside(after)
    assert.equal(catPlaySegmentIntersectsRect(before, after.cat, expandedRug), false)
    remaining -= step
  }
  return engine.snapshot()
}

function move(engine, point) {
  return engine.dispatch({ type: 'pointermove', pointerId: 1, ...point })
}

function dragSlowly(engine, point) {
  const from = engine.snapshot().feather
  for (let i = 1; i <= 24; i += 1) {
    move(engine, { x: from.x + (point.x - from.x) * i / 24, y: from.y + (point.y - from.y) * i / 24 })
    runSafe(engine, 1 / 30)
  }
}

function guideAboveRug(engine) {
  hold(engine)
  dragSlowly(engine, { x: 220, y: 190 })
  runSafe(engine, 1.6)
  assert.ok(engine.snapshot().cat.y < expandedRug.top)
  assert.equal(engine.snapshot().challenge.checkpointVisited, false)
  dragSlowly(engine, { x: 430, y: 185 })
  runSafe(engine, 2.2)
  assert.equal(engine.snapshot().challenge.checkpointVisited, true)
  assert.ok(engine.snapshot().cat.x > CH.waypoint.x)
  assert.equal(catPlaySegmentIntersectsRect(engine.snapshot().cat, CH.goal, expandedRug), false)
  assert.equal(engine.snapshot().phase, 'follow')
  return engine.snapshot()
}

for (const target of [{ x: 478, y: 286 }, { x: 478, y: 340 }]) {
  test(`挑战直接拖到 (${target.x},${target.y})：猫遇扩框停止，长 dt 和扑跃都不穿垫`, () => {
    const engine = createCatPlayEngine({ mode: 'challenge' })
    hold(engine)
    move(engine, target)
    const before = engine.snapshot().cat
    const first = engine.tick(999)
    assert.ok(Math.hypot(first.cat.x - before.x, first.cat.y - before.y) <= C.followSpeed * C.maxDt + 1e-8)
    const stopped = runSafe(engine, 5)
    assert.ok(stopped.cat.x < expandedRug.left)
    assert.equal(stopped.blocked, true)
    assert.equal(stopped.phase, 'follow')
    assert.equal(stopped.challenge.checkpointVisited, false)
    assert.equal(stopped.challenge.completed, false)
    assert.equal(release(engine).evaluation, null)
    const landed = runSafe(engine, 0.5)
    assert.equal(landed.evaluation.grade, 'blocked')
    assert.match(landed.evaluation.title, /垫子挡住/)
    assert.equal(landed.catches, 0)
  })
}

test('羽毛单独碰到脚印不算，猫脚点必须真实进入检查点半径', () => {
  const engine = createCatPlayEngine({ mode: 'challenge', followSpeed: 0 })
  hold(engine)
  move(engine, CH.waypoint)
  const waiting = runSafe(engine, 5)
  assert.equal(waiting.challenge.checkpointVisited, false)
  assert.equal(waiting.phase, 'follow')
  assert.equal(waiting.chargeProgress, 0)
  assert.equal(waiting.cat.x, CH.initialCat.x)
  release(engine)
  assert.equal(runSafe(engine, 0.5).challenge.completed, false)
})

test('未经过脚印不蓄势，直接松手给路线反馈而非直接完成', () => {
  const engine = createCatPlayEngine({ mode: 'challenge' })
  hold(engine)
  const waiting = runSafe(engine, 2)
  assert.equal(waiting.phase, 'follow')
  assert.equal(waiting.readySeconds, 0)
  assert.match(waiting.status, /上方脚印/)
  release(engine)
  const landed = runSafe(engine, 0.5)
  assert.equal(landed.evaluation.grade, 'route-needed')
  assert.equal(landed.challenge.completed, false)
})

test('经过脚印后羽毛在目标圈外只能跟随，落地给 outside-goal 并保留真实进度', () => {
  const engine = createCatPlayEngine({ mode: 'challenge' })
  const before = guideAboveRug(engine)
  assert.match(before.status, /目标圈/)
  assert.equal(before.chargeProgress, 0)
  release(engine)
  const landed = runSafe(engine, 0.5)
  assert.equal(landed.evaluation.grade, 'outside-goal')
  const recovered = runSafe(engine, 1)
  assert.equal(recovered.challenge.checkpointVisited, true)
  assert.deepEqual(recovered.feather, before.feather)
  assert.deepEqual(recovered.cat, landed.cat)
  assert.equal(hold(engine).challenge.checkpointVisited, true)
  assert.equal(engine.snapshot().evaluation, null)
})

for (const [wait, grade] of [[0, 'perfect'], [0.9, 'good'], [2, 'late']]) {
  test(`合法路线 220,190 → 430,185 → 478,286，${grade} 均可完成且只能抓一次`, () => {
    for (const bounds of [C.featherBounds, getCatPlayFeatherBounds(270, 168.75), getCatPlayFeatherBounds(436, 272.5)]) {
      const engine = createCatPlayEngine({ mode: 'challenge', featherBounds: bounds })
      guideAboveRug(engine)
      dragSlowly(engine, { x: 478, y: 286 })
      reachReady(engine)
      runSafe(engine, wait)
      const releaseState = release(engine)
      assert.equal(releaseState.evaluation, null)
      assert.equal(releaseState.challenge.completed, false)
      assert.equal(runSafe(engine, landingSeconds - C.fixedStep).challenge.completed, false)
      const completed = runSafe(engine, C.fixedStep)
      assert.equal(completed.challenge.completed, true)
      assert.equal(completed.evaluation.grade, grade)
      assert.match(completed.evaluation.title, /挑战完成/)
      assert.equal(completed.catches, 1)
      assert.deepEqual(runSafe(engine, 5), completed)
      assert.deepEqual(hold(engine), completed)
      release(engine)
      assert.deepEqual(engine.snapshot(), completed)
      const reset = engine.dispatch({ type: 'reset' })
      assert.equal(reset.mode, 'challenge')
      assert.equal(reset.catches, 0)
      assert.equal(reset.challenge.completed, false)
      assert.equal(reset.challenge.checkpointVisited, false)
      assert.equal(reset.evaluation, null)
      assert.equal(reset.cat.x, CH.initialCat.x)
      assert.deepEqual(reset.feather, CH.initialFeather)
    }
  })
}

test('合法绕路后太早扑空可原地重按重试，按住不重置检查点，失败也不自动换羽毛', () => {
  const engine = createCatPlayEngine({ mode: 'challenge' })
  guideAboveRug(engine)
  move(engine, { x: 478, y: 286 })
  release(engine)
  const landed = runSafe(engine, 0.5)
  assert.equal(landed.evaluation.grade, 'early')
  assert.equal(landed.challenge.checkpointVisited, true)
  assert.equal(landed.challenge.completed, false)
  const idle = runSafe(engine, 1)
  assert.deepEqual(idle.cat, landed.cat)
  assert.deepEqual(idle.feather, landed.feather)
  hold(engine)
  assert.equal(engine.snapshot().challenge.checkpointVisited, true)
  reachReady(engine)
  release(engine)
  assert.equal(runSafe(engine, 0.5).challenge.completed, true)
})

test('完成后暂停/恢复仍阻止自动下一抓，切自由玩清除本地挑战且正常多轮', () => {
  const engine = createCatPlayEngine({ mode: 'challenge' })
  guideAboveRug(engine)
  dragSlowly(engine, CH.goal)
  reachReady(engine)
  release(engine)
  runSafe(engine, 0.5)
  engine.dispatch({ type: 'pause' })
  engine.dispatch({ type: 'resume' })
  assert.equal(hold(engine).held, false)
  assert.equal(run(engine, 10).catches, 1)
  const free = engine.dispatch({ type: 'set-mode', mode: 'free' })
  assert.equal(free.challenge, null)
  assert.equal(free.catches, 0)
  assert.equal(free.evaluation, null)
  assert.equal(free.cat.x, C.initialCat.x)
  assert.deepEqual(free.feather, C.initialFeather)
  for (let i = 1; i <= 2; i += 1) {
    assert.equal(prepare(engine).phase, 'ready')
    release(engine)
    assert.equal(run(engine, 1.4).catches, i)
  }
})

test('挑战新嵌套对象和落地评价快照全部隔离，外部修改不能污染路线与评语', () => {
  const engine = createCatPlayEngine({ mode: 'challenge' })
  const original = engine.snapshot()
  const copy = engine.snapshot()
  copy.challenge.rug.x = -999
  copy.challenge.waypoint.radius = 999
  copy.challenge.goal.x = 160
  copy.challenge.checkpointVisited = true
  copy.challenge.completed = true
  copy.cat.x = 999
  copy.feather.x = 999
  copy.bounds.top = -999
  assert.deepEqual(engine.snapshot(), original)
  guideAboveRug(engine)
  dragSlowly(engine, CH.goal)
  reachReady(engine)
  release(engine)
  const completed = runSafe(engine, 0.5)
  const evaluation = { ...completed.evaluation }
  completed.evaluation.grade = 'forged'
  completed.evaluation.title = '被外部修改'
  completed.evaluation.detail = '被外部修改'
  completed.challenge.completed = false
  assert.deepEqual(engine.snapshot().evaluation, evaluation)
  assert.equal(engine.snapshot().challenge.completed, true)
})

test('组件模式切换先释放主捕获、键盘与旧 RAF，旧 pointerup/keyup 不会触发新模式扑跃', (t) => {
  const ui = componentHarness()
  t.after(() => ui.cleanup())
  ui.pointerDown(ui.point(ui.state.value.feather, 7))
  componentRun(ui, 0.75)
  const oldFrames = [...ui.frames.keys()]
  assert.deepEqual([...ui.captures], [7])
  ui.switchMode('challenge')
  assert.equal(ui.captures.size, 0)
  assert.equal(ui.state.value.mode, 'challenge')
  assert.equal(ui.state.value.held, false)
  assert.equal(ui.state.value.catches, 0)
  assert.equal(ui.state.value.evaluation, null)
  assert.equal(ui.frames.size, 1)
  assert.ok(oldFrames.every((id) => !ui.frames.has(id)))
  ui.pointerUp(ui.point(CH.initialFeather, 7))
  assert.equal(ui.state.value.phase, 'idle')
  ui.flushTimers()
  ui.keyboardDown(spaceEvent())
  assert.equal(ui.state.value.pointerId, 'keyboard')
  ui.switchMode('free')
  ui.keyboardUp(spaceEvent())
  ui.accessibleClick(clickEvent())
  assert.equal(componentRun(ui, 1).phase, 'idle')
  assert.equal(ui.state.value.catches, 0)
})

test('组件手动暂停及后台状态切模式不启动 RAF，不改变暂停偏好；重试保持同一挑战', (t) => {
  const ui = componentHarness()
  t.after(() => ui.cleanup())
  ui.togglePause()
  assert.equal(ui.manualPaused.value, true)
  ui.switchMode('challenge')
  assert.equal(ui.manualPaused.value, true)
  assert.equal(ui.state.value.phase, 'paused')
  assert.equal(ui.frames.size, 0)
  ui.switchMode('free')
  assert.equal(ui.frames.size, 0)
  ui.togglePause()
  assert.equal(ui.frames.size, 1)
  ui.listeners.get('window:blur')()
  ui.switchMode('challenge')
  assert.equal(ui.manualPaused.value, false)
  assert.equal(ui.state.value.phase, 'paused')
  assert.equal(ui.frames.size, 0)
  ui.restart()
  assert.equal(ui.state.value.mode, 'challenge')
  assert.equal(ui.state.value.phase, 'paused')
  assert.equal(ui.frames.size, 0)
  ui.listeners.get('window:focus')()
  assert.equal(ui.state.value.phase, 'idle')
  assert.equal(ui.frames.size, 1)
  ui.document.hidden = true
  ui.switchMode('free')
  assert.equal(ui.frames.size, 0)
  assert.equal(ui.state.value.phase, 'paused')
})

test('组件固定结果槽、两模式选择、路线检测属性齐备，ready 提示不因评级窗口变化', (t) => {
  const ui = componentHarness()
  t.after(() => ui.cleanup())
  assert.match(componentSource, /class="play-status play-evaluation"/)
  assert.match(componentSource, /:grade="state.evaluation\?\.grade/)
  for (const marker of ['play-evaluation-title', 'play-evaluation-detail', 'play-mode-free', 'play-mode-challenge', 'play-waypoint', 'play-goal', 'data-challenge-completed', 'data-cx', 'data-cy', 'data-visited']) {
    assert.ok(componentSource.includes(marker), marker)
  }
  assert.match(componentSource, /data-mode="free"/)
  assert.match(componentSource, /data-mode="challenge"/)
  assert.match(componentSource, /v-if="state.challenge" class="play-route"/)
  ui.keyboardDown(spaceEvent())
  componentRun(ui, 0.75)
  const label = ui.phaseLabel.value
  componentRun(ui, 2)
  assert.equal(ui.phaseLabel.value, label)
  ui.switchMode('challenge')
  assert.match(ui.challengeProgress.value, /经过脚印.*扑到目标/)
})

test('组件切换后 Escape、Tab 焦点限制与关闭仍有效', (t) => {
  const ui = componentHarness()
  t.after(() => ui.cleanup())
  ui.switchMode('challenge')
  ui.feather.focus()
  const tab = { key: 'Tab', preventDefault() { this.prevented = true } }
  ui.dialogKeydown(tab)
  assert.equal(tab.prevented, true)
  assert.equal(ui.document.activeElement, ui.close)
  ui.dialogKeydown({ key: 'Escape', preventDefault() {}, stopPropagation() {} })
  assert.deepEqual(ui.emissions, ['close'])
  assert.equal(ui.frames.size, 0)
})

test('挑战 ready 移出目标圈即使只有两像素也重新准备，不继承旧计时', () => {
  const engine = createCatPlayEngine({ mode: 'challenge' })
  guideAboveRug(engine)
  move(engine, { x: CH.goal.x + CH.goal.radius - 1, y: CH.goal.y })
  reachReady(engine)
  runSafe(engine, 2)
  const outside = move(engine, { x: CH.goal.x + CH.goal.radius + 1, y: CH.goal.y })
  assert.equal(outside.phase, 'follow')
  assert.equal(outside.readySeconds, 0)
  assert.equal(outside.chargeProgress, 0)
  release(engine)
  assert.equal(runSafe(engine, 0.5).evaluation.grade, 'outside-goal')
})

test('经过脚印后向垫子内部拖动仍阻挡，扑跃整段也不能越过扩框', () => {
  const engine = createCatPlayEngine({ mode: 'challenge' })
  guideAboveRug(engine)
  move(engine, { x: 300, y: 310 })
  const stopped = runSafe(engine, 2)
  assert.equal(stopped.blocked, true)
  assert.equal(stopped.challenge.checkpointVisited, true)
  assert.equal(stopped.phase, 'follow')
  release(engine)
  const landed = runSafe(engine, 0.5)
  assert.equal(landed.evaluation.grade, 'blocked')
  assert.equal(landed.challenge.completed, false)
  assert.equal(landed.catches, 0)
})

test('到目标且直线无障碍也不能跳过猫脚印检查点', () => {
  const engine = createCatPlayEngine({ mode: 'challenge' })
  hold(engine)
  for (const point of [{ x: 160, y: 350 }, { x: 36, y: 350 }, { x: 604, y: 350 }]) {
    move(engine, point)
    runSafe(engine, 6)
  }
  move(engine, CH.goal)
  const atGoal = runSafe(engine, 3)
  assert.equal(atGoal.challenge.checkpointVisited, false)
  assert.equal(catPlaySegmentIntersectsRect(atGoal.cat, CH.goal, expandedRug), false)
  assert.equal(atGoal.phase, 'follow')
  assert.equal(atGoal.chargeProgress, 0)
  release(engine)
  const landed = runSafe(engine, 0.5)
  assert.equal(landed.evaluation.grade, 'route-needed')
  assert.equal(landed.challenge.completed, false)
})

for (const cancel of ['cancelDrag', 'Tab', 'resize']) {
  test(`组件 ${cancel} 中止飞行，保持无评语、不补发；已落地取消仍保留评语`, (t) => {
    const ui = componentHarness()
    t.after(() => ui.cleanup())
    ui.keyboardDown(spaceEvent())
    componentRun(ui, 0.75)
    ui.keyboardUp(spaceEvent())
    componentRun(ui, 0.2)
    if (cancel === 'cancelDrag') ui.cancelDrag()
    else if (cancel === 'Tab') ui.dialogKeydown({ key: 'Tab', preventDefault() {} })
    else ui.setSize(320, 200)
    const after = componentRun(ui, 2)
    assert.equal(after.evaluation, null)
    assert.equal(after.catches, 0)
    ui.flushTimers()
    ui.keyboardDown(spaceEvent())
    componentRun(ui, 0.75)
    ui.keyboardUp(spaceEvent())
    const evaluation = componentRun(ui, 0.5).evaluation
    assert.ok(evaluation)
    ui.cancelDrag()
    assert.deepEqual(ui.state.value.evaluation, evaluation)
  })
}

test('合法路线连续三次 early 后仍保留扑距，原地重新按住即可 ready 和完成', () => {
  const engine = createCatPlayEngine({ mode: 'challenge' })
  guideAboveRug(engine)
  move(engine, CH.goal)
  for (let retry = 0; retry < 3; retry += 1) {
    if (!engine.snapshot().held) hold(engine)
    runSafe(engine, 0.05)
    assert.equal(release(engine).result, 'too-early')
    const landed = runSafe(engine, 0.5)
    assert.equal(landed.evaluation.grade, 'early')
    assert.equal(landed.challenge.checkpointVisited, true)
    assert.equal(landed.challenge.completed, false)
    assert.equal(landed.catches, 0)
    const d = Math.hypot(landed.cat.x - landed.feather.x, landed.cat.y - landed.feather.y)
    assert.ok(d >= C.minPounceDistance + CH.retryDistanceMargin - 1e-8)
    assert.deepEqual(runSafe(engine, 0.5).cat, landed.cat)
    assert.deepEqual(engine.snapshot().feather, { x: CH.goal.x, y: CH.goal.y })
  }
  hold(engine)
  assert.equal(reachReady(engine).phase, 'ready')
  release(engine)
  assert.equal(runSafe(engine, 0.5).challenge.completed, true)
})

test('挑战路线条件已满足时距离提示优先，不对太近或太远误导为等待 ready', () => {
  const engine = createCatPlayEngine({ mode: 'challenge' })
  guideAboveRug(engine)
  assert.match(move(engine, { x: CH.goal.x + 39, y: CH.goal.y }).status, /太远/)
  move(engine, CH.goal)
  for (let retry = 0; retry < 3; retry += 1) {
    if (!engine.snapshot().held) hold(engine)
    release(engine)
    runSafe(engine, 1)
  }
  hold(engine)
  const { cat } = engine.snapshot()
  const d = Math.hypot(cat.x - CH.goal.x, cat.y - CH.goal.y)
  const tooClose = { x: CH.goal.x + (cat.x - CH.goal.x) / d * 35,
    y: CH.goal.y + (cat.y - CH.goal.y) / d * 35 }
  assert.match(move(engine, tooClose).status, /太近/)
  const following = runSafe(engine, 1)
  assert.equal(following.phase, 'follow')
  assert.match(following.status, /太近/)
  assert.doesNotMatch(following.status, /等它准备好/)
})

for (const reason of ['pause', 'blur', 'visibilitychange', 'cancel']) {
  for (const at of ['pounce', 'caught', 'recover']) {
    test(`自由玩 ${at} 时 ${reason}，恢复后旧热区首次重按摆下一轮，不补发抓取`, () => {
      for (const pointerId of [7, 'keyboard']) {
        const engine = createCatPlayEngine()
        prepare(engine)
        run(engine, 2)
        release(engine)
        run(engine, at === 'pounce' ? 0.45 : at === 'caught' ? 0.5 : 1)
        const before = engine.snapshot()
        assert.equal(before.phase, at)
        const oldFeather = { ...before.feather }
        engine.dispatch({ type: reason })
        engine.dispatch({ type: 'resume' })
        const resumed = run(engine, 2)
        assert.equal(resumed.needsNextPlacement, true)
        assert.equal(resumed.catches, before.catches)
        assert.deepEqual(resumed.evaluation, before.evaluation)
        assert.deepEqual(resumed.feather, oldFeather)
        const next = engine.dispatch({ type: 'pointerdown', pointerId, ...oldFeather })
        assert.equal(next.needsNextPlacement, false)
        assert.equal(next.evaluation, null)
        const d = Math.hypot(next.cat.x - next.feather.x, next.cat.y - next.feather.y)
        assert.ok(d >= C.minPounceDistance && d <= C.maxPounceDistance)
        assert.equal(run(engine, 0.75).phase, 'ready')
        release(engine, pointerId)
        assert.equal(run(engine, 0.5).catches, before.catches + 1)
      }
    })
  }
}

test('挑战取消不摆下一轮羽毛，不改路线进度，不补发或增加抓取', () => {
  const engine = createCatPlayEngine({ mode: 'challenge' })
  guideAboveRug(engine)
  move(engine, CH.goal)
  reachReady(engine)
  release(engine)
  runSafe(engine, 0.45)
  const before = engine.snapshot()
  engine.dispatch({ type: 'pause' })
  engine.dispatch({ type: 'resume' })
  const resumed = engine.snapshot()
  assert.equal(resumed.needsNextPlacement, false)
  assert.deepEqual(resumed.feather, before.feather)
  assert.equal(resumed.challenge.checkpointVisited, true)
  assert.equal(resumed.challenge.completed, false)
  assert.equal(resumed.evaluation, null)
  assert.equal(resumed.catches, 0)
  assert.deepEqual(hold(engine).feather, before.feather)
})

function uiHold(ui, input) {
  if (input === 'pointer') ui.pointerDown(ui.point(ui.state.value.feather))
  else if (input === 'keyboard') ui.keyboardDown(spaceEvent())
  else ui.accessibleClick(clickEvent())
}

function uiRelease(ui, input) {
  if (input === 'pointer') ui.pointerUp(ui.point(ui.state.value.feather))
  else if (input === 'keyboard') ui.keyboardUp(spaceEvent())
  else ui.accessibleClick(clickEvent())
}

for (const input of ['pointer', 'keyboard', 'accessible']) {
  for (const at of ['pounce', 'caught', 'recover']) {
    test(`组件 ${input} 在 ${at} 暂停后旧输入点重试 ready，不要求手动拖远`, (t) => {
      const ui = componentHarness()
      t.after(() => ui.cleanup())
      uiHold(ui, input)
      componentRun(ui, 2.75)
      uiRelease(ui, input)
      componentRun(ui, at === 'pounce' ? 0.45 : at === 'caught' ? 0.5 : 1)
      const before = ui.state.value
      assert.equal(before.phase, at)
      assert.equal(before.result, 'caught')
      const oldPoint = ui.point(before.feather, 9)
      oldPoint.clientX += 15
      oldPoint.clientY += 10
      ui.togglePause()
      componentRun(ui, 20)
      ui.togglePause()
      assert.equal(ui.state.value.needsNextPlacement, true)
      assert.equal(ui.state.value.catches, before.catches)
      assert.deepEqual(ui.state.value.evaluation, before.evaluation)
      ui.flushTimers()
      if (input === 'pointer') {
        ui.pointerDown(oldPoint)
        const placed = { ...ui.state.value.feather }
        ui.pointerMove(oldPoint)
        assert.deepEqual(ui.state.value.feather, placed)
      } else uiHold(ui, input)
      assert.equal(ui.state.value.needsNextPlacement, false)
      assert.equal(ui.state.value.evaluation, null)
      assert.equal(componentRun(ui, 0.75).phase, 'ready')
      if (input === 'pointer') ui.pointerUp(oldPoint)
      else uiRelease(ui, input)
      assert.equal(componentRun(ui, 0.5).catches, before.catches + 1)
      assert.equal(ui.captures.size, 0)
    })
  }
}

for (const input of ['pointer', 'keyboard', 'accessible']) {
  for (const [frameTime, eventTime, priorGrade, grade] of [
    [1200, 1295, 'perfect', 'good'], [2100, 2195, 'good', 'late'],
  ]) {
    test(`组件 ${input} 松手 ${eventTime}ms 按事件判 ${grade}，不把前一帧间隔算进扑跃`, (t) => {
      const ui = componentHarness()
      t.after(() => ui.cleanup())
      uiHold(ui, input)
      componentRun(ui, frameTime / 1000)
      assert.equal(ui.state.value.phase, 'ready')
      assert.equal(getCatPlayTimingEvaluation(ui.state.value.readySeconds).grade, priorGrade)
      ui.setTime(eventTime)
      uiRelease(ui, input)
      assert.equal(ui.state.value.phase, 'pounce')
      assert.equal(ui.state.value.phaseSeconds, 0)
      assert.equal(ui.state.value.evaluation, null)
      assert.equal(ui.state.value.catches, 0)
      ui.frameAt(eventTime + 5)
      assert.equal(ui.state.value.phaseSeconds, 0)
      for (let time = eventTime + 55; time < eventTime + 480; time += 50) {
        ui.frameAt(time)
        assert.equal(ui.state.value.phase, 'pounce')
        assert.equal(ui.state.value.evaluation, null)
      }
      ui.frameAt(eventTime + 480)
      assert.equal(ui.state.value.phase, 'pounce')
      assert.equal(ui.state.value.evaluation, null)
      const landed = ui.frameAt(eventTime + 485)
      assert.equal(landed.phase, 'caught')
      assert.equal(landed.evaluation.grade, grade)
      assert.equal(landed.catches, 1)
    })
  }
}

test('组件帧间从 charging 进入 ready 后松手按真实阶段成功，而非使用上一帧 early', (t) => {
  const ui = componentHarness()
  t.after(() => ui.cleanup())
  ui.keyboardDown(spaceEvent())
  assert.equal(componentRun(ui, 0.65).phase, 'charging')
  ui.setTime(695)
  ui.keyboardUp(spaceEvent())
  assert.equal(ui.state.value.phase, 'pounce')
  assert.equal(ui.state.value.result, 'caught')
  assert.equal(ui.state.value.evaluation, null)
  assert.equal(componentRun(ui, 0.5).evaluation.grade, 'perfect')
})

test('组件帧间拖动先推进旧 ready 再重置，后续帧不将旧间隔计为新 follow', (t) => {
  const ui = componentHarness()
  t.after(() => ui.cleanup())
  ui.pointerDown(ui.point(ui.state.value.feather))
  componentRun(ui, 0.75)
  const feather = { ...ui.state.value.feather }
  ui.setTime(835)
  ui.pointerMove(ui.point({ x: feather.x + 20, y: feather.y }))
  assert.equal(ui.state.value.phase, 'follow')
  assert.equal(ui.state.value.readySeconds, 0)
  assert.equal(ui.state.value.stillSeconds, 0)
  ui.frameAt(840)
  assert.equal(ui.state.value.phaseSeconds, 0)
  assert.equal(ui.state.value.stillSeconds, 0)
  assert.equal(ui.state.value.evaluation, null)
})

for (const boundary of ['pause', 'blur', 'visibilitychange', 'set-mode', 'reset', 'resize', 'cancel']) {
  test(`组件 ${boundary} 边界丢弃尚未处理的旧飞行时间，不能补发评价或跨新阶段`, (t) => {
    const ui = componentHarness()
    t.after(() => ui.cleanup())
    ui.keyboardDown(spaceEvent())
    componentRun(ui, 0.75)
    ui.keyboardUp(spaceEvent())
    assert.equal(componentRun(ui, 0.45).phase, 'pounce')
    ui.setTime(ui.now + 95)
    const boundaryTime = ui.now
    if (boundary === 'pause') ui.togglePause()
    else if (boundary === 'blur') ui.listeners.get('window:blur')()
    else if (boundary === 'visibilitychange') {
      ui.document.hidden = true
      ui.listeners.get('document:visibilitychange')()
    } else if (boundary === 'set-mode') ui.switchMode('challenge')
    else if (boundary === 'reset') ui.restart()
    else if (boundary === 'resize') ui.setSize(320, 200)
    else ui.cancelDrag()
    assert.equal(ui.state.value.catches, 0)
    assert.equal(ui.state.value.evaluation, null)
    if (boundary === 'pause') ui.togglePause()
    else if (boundary === 'blur') ui.listeners.get('window:focus')()
    else if (boundary === 'visibilitychange') {
      ui.document.hidden = false
      ui.listeners.get('document:visibilitychange')()
    }
    ui.frameAt(boundaryTime + 5)
    assert.equal(ui.state.value.phaseSeconds, 0)
    assert.equal(ui.state.value.catches, 0)
    assert.equal(ui.state.value.evaluation, null)
    assert.equal(ui.state.value.held, false)
  })
}

test('组件后台长间隔不推进旧 ready，恢复时重新建立 performance 时钟', (t) => {
  const ui = componentHarness()
  t.after(() => ui.cleanup())
  ui.keyboardDown(spaceEvent())
  componentRun(ui, 1.2)
  ui.setTime(50000)
  ui.document.hidden = true
  ui.keyboardUp(spaceEvent())
  assert.notEqual(ui.state.value.phase, 'pounce')
  ui.listeners.get('document:visibilitychange')()
  assert.equal(ui.state.value.phase, 'paused')
  assert.equal(ui.state.value.readySeconds, 0)
  ui.setTime(90000)
  ui.document.hidden = false
  ui.listeners.get('document:visibilitychange')()
  ui.frameAt(90005)
  assert.equal(ui.state.value.phase, 'idle')
  assert.equal(ui.state.value.phaseSeconds, 0)
  assert.equal(ui.state.value.evaluation, null)
  ui.flushTimers()
  ui.keyboardDown(spaceEvent())
  ui.frameAt(90010)
  assert.equal(ui.state.value.phase, 'follow')
  ui.keyboardUp(spaceEvent())
  assert.equal(componentRun(ui, 0.5).evaluation.grade, 'early')
})

test('组件事件推进受 maxDt 限制，事件后 RAF 不重复计入长间隔', (t) => {
  const ui = componentHarness()
  t.after(() => ui.cleanup())
  ui.pointerDown(ui.point(ui.state.value.feather))
  ui.pointerMove(ui.point({ x: 580, y: 278 }))
  componentRun(ui, 0.05)
  const before = ui.state.value.cat
  ui.setTime(5050)
  ui.pointerMove(ui.point({ x: 580, y: 278 }))
  const moved = ui.state.value.cat
  assert.ok(Math.hypot(moved.x - before.x, moved.y - before.y) <= C.followSpeed * C.maxDt + 1e-8)
  assert.equal(ui.state.value.phase, 'follow')
  ui.pointerUp(ui.point(ui.state.value.feather))
  ui.frameAt(5055)
  assert.equal(ui.state.value.phase, 'pounce')
  assert.equal(ui.state.value.phaseSeconds, 0)
  assert.equal(ui.state.value.evaluation, null)
})

test('组件较早的 RAF 时间戳不回退事件时钟，也不重复推进 ready', (t) => {
  const ui = componentHarness()
  t.after(() => ui.cleanup())
  ui.pointerDown(ui.point(ui.state.value.feather))
  componentRun(ui, 0.75)
  ui.setTime(835)
  ui.pointerMove(ui.point(ui.state.value.feather))
  const afterEvent = ui.state.value
  assert.deepEqual(ui.frameAt(800, 840), afterEvent)
  const next = ui.frameAt(850, 860)
  assert.ok(next.readySeconds > afterEvent.readySeconds)
  assert.ok(next.readySeconds - afterEvent.readySeconds <= 0.02)
})
