// 运行：node --test src/cat-climb.test.js
import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'
import { parse, compileScript, compileTemplate, compileStyle } from '@vue/compiler-sfc'
import {
  CAT_CLIMB_CONFIG as C, CAT_CLIMB_STAGE_LABELS, getCatClimbStage,
  createCatClimbRandom, createCatClimbGenerator, createCatClimbEngine,
  catClimbDescendingTime, getCatClimbPlatformX, proveCatClimbSegment, sweepCatClimbLanding,
} from './cat-climb.js'

const near = (a, b, tolerance = 1e-7) => assert.ok(Math.abs(a - b) <= tolerance, `${a} 不接近 ${b}`)
const platform = (x = 180, y = 100, extra = {}) => ({ id: 1, x, baseX: x, y, width: 60, type: 'fixed', ...extra })
const expectedDifficulties = [
  { stage: 'warmup', width: 95, minGap: 48, maxGap: 56, minOffset: 48, maxOffset: 60, interval: 0 },
  { stage: 'alternating', width: 85, minGap: 48, maxGap: 60, minOffset: 54, maxOffset: 66, interval: 0 },
  { stage: 'narrow', width: 72, minGap: 48, maxGap: 64, minOffset: 58, maxOffset: 70, interval: 6 },
  { stage: 'moving', width: 64, minGap: 48, maxGap: 68, minOffset: 60, maxOffset: 74, interval: 4 },
  { stage: 'moving', width: 60, minGap: 50, maxGap: 68, minOffset: 64, maxOffset: 76, interval: 3 },
]
const platformLevel = (index, every = 5) => Math.min(4, Math.floor(Math.max(0, index - 1) / every))
const reachedLevel = (index, every = 5) => Math.min(4, Math.floor(index / every))
const isForkIndex = (index, every = 5) => {
  const level = platformLevel(index, every)
  const interval = expectedDifficulties[level].interval
  return interval > 0 && (index - 1 - level * every) % interval === 0
}

// 控制器只发左右/松手并推进 tick，不改 snapshot，不注入位置，不传内部状态。
function drive(engine, { landings = 36, moving = true, hz = 120, edge = 0, onFrame } = {}) {
  let s = engine.dispatch({ type: 'start' })
  let target = null
  let previousJumps = 0
  const landed = []
  let maxPlatforms = s.platforms.length
  let maxCamera = 0
  const limit = (landings + 8) * hz
  for (let i = 0; i < limit && s.phase === 'running' && landed.length < landings; i++) {
    if (!target || s.jumps !== previousJumps) {
      previousJumps = s.jumps
      const above = s.platforms.filter((p) => p.y > s.cat.y + 1).sort((a, b) => a.y - b.y)
      assert.ok(above.length, '生成器应提前生成后续平台')
      target = (moving && above.find((p) => p.y === above[0].y && p.type === 'moving')) || above[0]
    }
    const x = getCatClimbPlatformX(target, s.simTime) + edge * (target.width / 2 + C.catHalfWidth - 8)
    const difference = x - s.cat.x
    const direction = Math.abs(difference) <= C.horizontalSpeed / hz / 2 ? 0 : Math.sign(difference)
    engine.dispatch({ type: 'direction', direction })
    const n = engine.tick(1 / hz)
    assert.ok(n.cameraY >= maxCamera, '镜头不得回撤')
    assert.ok(n.height + 1e-7 >= s.height, '最高高度不得减少')
    assert.ok(n.floor >= s.floor, '实际最高层数不得回落')
    assert.equal(n.floor, Math.max(s.floor, n.lastLanding?.index ?? 0))
    assert.equal(n.maxLandingIndex, n.floor)
    assert.equal(n.difficultyLevel, reachedLevel(n.floor, n.difficultyEvery))
    assert.equal(n.level, n.difficultyLevel)
    assert.equal(n.difficultyCapped, n.level === 4)
    assert.equal(n.difficultyStage, expectedDifficulties[n.level].stage)
    assert.ok(n.cat.x >= C.catHalfWidth && n.cat.x <= C.width - C.catHalfWidth)
    if (n.jumps > s.jumps) {
      assert.equal(n.jumps, s.jumps + 1)
      assert.ok(n.lastLanding)
      assert.ok(n.lastLanding.time > (s.lastLanding?.time ?? 0))
      assert.ok(n.lastLanding.y >= (s.lastLanding?.y ?? C.startY), '控制器不能靠反复原地弹跳计数')
      landed.push(n.lastLanding)
    }
    maxCamera = n.cameraY
    maxPlatforms = Math.max(maxPlatforms, n.platforms.length)
    s = n
    onFrame?.(s, i)
  }
  return { state: s, landed, maxPlatforms }
}

test('物理阈值：世界、峰高、下降落地根及宽度上限', () => {
  assert.equal(C.width, 360)
  assert.equal(C.height, 560)
  assert.equal(C.step, 1 / 120)
  assert.equal(C.jumpSpeed, 430)
  assert.equal(C.gravity, 1000)
  assert.equal(C.horizontalSpeed, 160)
  near(C.jumpSpeed ** 2 / (2 * C.gravity), 92.45)
  for (const gap of [48, 58, 68]) {
    const t = catClimbDescendingTime(gap)
    near(C.jumpSpeed * t - C.gravity * t * t / 2, gap)
    assert.ok(C.jumpSpeed - C.gravity * t < 0)
    assert.ok(C.horizontalSpeed * t - C.horizontalSafety > 90)
  }
  for (const bad of [-1, 93, NaN, Infinity, '58']) assert.equal(catClimbDescendingTime(bad), null)
})

test('连续下降跨过薄台：脚底按交点立即反弹并使用剩余时间', () => {
  const cat = { x: 180, y: 170, vx: 0, vy: -900 }
  const hit = sweepCatClimbLanding(cat, [platform()], 0.1)
  assert.ok(hit)
  near(cat.y + cat.vy * hit.time - C.gravity * hit.time ** 2 / 2, 100)
  assert.ok(hit.time > 0 && hit.time < 0.1)
  const result = drive(createCatClimbEngine({ seed: 17 }), { landings: 1 })
  assert.equal(result.landed.length, 1)
  const remaining = result.state.simTime - result.state.lastLanding.time
  near(result.state.cat.y, result.state.lastLanding.y + C.jumpSpeed * remaining - C.gravity * remaining ** 2 / 2)
  near(result.state.cat.vy, C.jumpSpeed - C.gravity * remaining)
})

test('上升穿过平台底部不碰撞', () => {
  assert.equal(sweepCatClimbLanding({ x: 180, y: 90, vx: 0, vy: 430 }, [platform()], 0.1), null)
  const e = createCatClimbEngine({ seed: 1 })
  const first = e.snapshot().platforms[1]
  e.dispatch({ type: 'start' })
  e.dispatch({ type: 'direction', direction: 1 })
  for (let i = 0; i < 50; i++) {
    const s = e.tick(C.step)
    if (s.cat.vy > 0) assert.equal(s.lastLanding, null)
  }
  assert.ok(first.y > C.startY)
})

test('斜向误判：帧末对齐不能代替跨顶时刻对齐', () => {
  const hit = sweepCatClimbLanding({ x: 20, y: 101, vx: 1000, vy: -500 }, [platform(100, 100)], 0.1)
  assert.equal(hit, null)
})

test('斜向反例：跨顶时对齐，即使帧末错过也能落地', () => {
  const hit = sweepCatClimbLanding({ x: 100, y: 101, vx: 1000, vy: -500 }, [platform(100, 100)], 0.1)
  assert.ok(hit)
  assert.ok(hit.x < 104)
})

test('移动台连续碰撞使用交点时刻的正弦位置', () => {
  const p = platform(180, 100, { type: 'moving', width: 65, amplitude: 16, period: 3.6, phase: 0.4 })
  const time = 1.3
  const cat = { x: 180, y: 120, vx: 160, vy: -300 }
  const hit = sweepCatClimbLanding(cat, [p], 0.1, time)
  assert.ok(hit)
  const x = getCatClimbPlatformX(p, time + hit.time)
  assert.ok(Math.abs(hit.x - x) < p.width / 2 + C.catHalfWidth)
  near(cat.y + cat.vy * hit.time - C.gravity * hit.time ** 2 / 2, p.y)
})

test('边界夹持按交点时间，不出现左右穿越', () => {
  const right = sweepCatClimbLanding({ x: 340, y: 105, vx: 160, vy: -100 }, [platform(330, 100)], 0.1)
  assert.ok(right)
  assert.ok(right.x <= 348)
  const e = createCatClimbEngine()
  e.dispatch({ type: 'start' })
  e.dispatch({ type: 'direction', direction: -1 })
  for (let i = 0; i < 180; i++) assert.ok(e.tick(C.step).cat.x >= 12)
  const f = createCatClimbEngine()
  f.dispatch({ type: 'start' })
  f.dispatch({ type: 'direction', direction: 1 })
  for (let i = 0; i < 180; i++) assert.ok(f.tick(C.step).cat.x <= 348)
})

test('连续接触候选取最早下降落点，而非数组顺序', () => {
  const lower = platform(180, 80, { id: 1 })
  const upper = platform(180, 100, { id: 2 })
  assert.equal(sweepCatClimbLanding({ x: 180, y: 110, vx: 0, vy: -400 }, [lower, upper], 0.1).id, 2)
})

test('脚底刚好擦边没有重叠不算落地', () => {
  assert.equal(sweepCatClimbLanding({ x: 138, y: 101, vx: 0, vy: -100 }, [platform()], 0.1), null)
  assert.ok(sweepCatClimbLanding({ x: 138.01, y: 101, vx: 0, vy: -100 }, [platform()], 0.1))
})

test('开始前不计时，方向事件不得启动游戏', () => {
  const e = createCatClimbEngine()
  const initial = e.snapshot()
  e.dispatch({ type: 'direction', direction: 1 })
  assert.deepEqual(e.tick(10), initial)
  assert.equal(e.dispatch({ type: 'start' }).cat.vy, 430)
  e.tick(C.step)
  const s = e.snapshot()
  assert.deepEqual(e.dispatch({ type: 'start' }), s)
})

test('松开立即停止，没有强惯性', () => {
  const e = createCatClimbEngine()
  e.dispatch({ type: 'start' })
  e.dispatch({ type: 'direction', direction: 1 })
  const moved = e.tick(0.1)
  near(moved.cat.x, 196)
  assert.equal(e.dispatch({ type: 'direction', direction: 0 }).cat.vx, 0)
  near(e.tick(0.1).cat.x, moved.cat.x)
})

test('长 dt 最多推进 0.1 秒，不补算后台', () => {
  const a = createCatClimbEngine()
  const b = createCatClimbEngine()
  for (const e of [a, b]) e.dispatch({ type: 'start' })
  assert.deepEqual(a.tick(60), b.tick(0.1))
  near(a.snapshot().simTime, 0.1)
})

test('不足一步累计，暂停丢弃小余量', () => {
  const e = createCatClimbEngine()
  e.dispatch({ type: 'start' })
  assert.equal(e.tick(C.step / 2).simTime, 0)
  near(e.tick(C.step / 2).simTime, C.step)
  e.tick(C.step / 2)
  e.dispatch({ type: 'pause' })
  e.dispatch({ type: 'resume' })
  near(e.tick(C.step / 2).simTime, C.step)
})

for (const hz of [30, 60, 120]) {
  test(`相同时间边界的输入在 ${hz} Hz 与 120 Hz 完全确定`, () => {
    function run(rate) {
      const e = createCatClimbEngine({ seed: 823 })
      e.dispatch({ type: 'start' })
      for (let i = 0; i < rate * 8; i++) {
        const t = i / rate
        if (i % (rate / 2) === 0) e.dispatch({ type: 'direction', direction: [1, 0, -1, 0][Math.round(t * 2) % 4] })
        e.tick(1 / rate)
      }
      return e.snapshot()
    }
    assert.deepEqual(run(hz), run(120))
    assert.ok(run(hz).lastLanding, '此确定性检查应包含至少一次真实落地')
  })
}

for (const phase of ['ready', 'running', 'ended']) {
  test(`暂停保留 ${phase}，恢复不重开已完成阶段`, () => {
    const e = createCatClimbEngine()
    if (phase !== 'ready') e.dispatch({ type: 'start' })
    if (phase === 'ended') for (let i = 0; i < 160; i++) e.tick(C.step)
    if (phase === 'running') {
      e.dispatch({ type: 'direction', direction: 1 })
      e.tick(0.1)
    }
    assert.equal(e.snapshot().phase, phase)
    const paused = e.dispatch({ type: 'pause' })
    assert.equal(paused.cat.vx, 0)
    e.dispatch({ type: 'direction', direction: 1 })
    assert.deepEqual(e.tick(1000), paused)
    assert.deepEqual(e.dispatch({ type: 'pause' }), paused)
    const resumed = e.dispatch({ type: 'resume' })
    assert.equal(resumed.phase, phase)
    assert.equal(resumed.cat.vx, 0)
    assert.equal(resumed.height, paused.height)
    assert.equal(resumed.simTime, paused.simTime)
    if (phase === 'running') near(e.tick(0.1).cat.x, paused.cat.x)
  })
}

test('初始无输入跨 100 种子确实漏接并结束，不原地刷高', () => {
  for (let seed = 0; seed < 100; seed++) {
    const e = createCatClimbEngine({ seed })
    e.dispatch({ type: 'start' })
    for (let i = 0; i < 160; i++) e.tick(C.step)
    const s = e.snapshot()
    assert.equal(s.phase, 'ended', `种子 ${seed}`)
    assert.equal(s.lastLanding, null)
    near(s.height, 9.245)
    assert.equal(s.reason, '没接住平台')
  }
})

test('结束后 cat、移动平台、simTime、高度全部冻结', () => {
  const e = createCatClimbEngine()
  e.dispatch({ type: 'start' })
  for (let i = 0; i < 160; i++) e.tick(C.step)
  const s = e.snapshot()
  assert.equal(s.phase, 'ended')
  for (let i = 0; i < 100; i++) {
    assert.deepEqual(e.tick(0.1), s)
    assert.deepEqual(e.dispatch({ type: 'direction', direction: i % 2 ? -1 : 1 }), s)
    assert.deepEqual(e.dispatch({ type: 'start' }), s)
    assert.deepEqual(e.dispatch({ type: 'resume' }), s)
  }
})

test('重开回到 ready 并清零，默认可重复同一 seed', () => {
  const e = createCatClimbEngine({ seed: 77 })
  const original = e.snapshot()
  drive(e, { landings: 12 })
  assert.deepEqual(e.dispatch({ type: 'reset' }), original)
  assert.notDeepEqual(e.dispatch({ type: 'reset', seed: 78 }).platforms, original.platforms)
  assert.equal(e.snapshot().phase, 'ready')
})

test('固定路线与 seed 无关，随机模式 seed 不同且可确定复现', () => {
  const a = createCatClimbEngine({ seed: 1, mode: 'fixed' })
  const b = createCatClimbEngine({ seed: 2, mode: 'fixed' })
  assert.deepEqual(a.snapshot().platforms, b.snapshot().platforms)
  const fixed = a.snapshot().platforms
  const random = a.dispatch({ type: 'reset', mode: 'random', seed: 2 })
  assert.notDeepEqual(random.platforms, fixed)
  assert.deepEqual(random.platforms, createCatClimbEngine({ mode: 'random', seed: 2 }).snapshot().platforms)
  assert.equal(random.mode, 'random')
})

test('无效输入、模式和种子不修改状态，配置初始化有安全默认值', () => {
  const e = createCatClimbEngine()
  const initial = e.snapshot()
  for (const input of [null, undefined, 7, {}, { type: 'resize', width: 999 },
    { type: 'reset', seed: -1 }, { type: 'reset', seed: 1.5 }, { type: 'reset', seed: 2 ** 32 },
    { type: 'reset', mode: 'anything' }]) assert.deepEqual(e.dispatch(input), initial)
  e.dispatch({ type: 'start' })
  const running = e.snapshot()
  for (const input of [NaN, Infinity, -1, 0, '1', null]) assert.deepEqual(e.tick(input), running)
  for (const d of [-2, 2, '1', NaN, null, undefined]) assert.deepEqual(e.dispatch({ type: 'direction', direction: d }), running)
  assert.equal(createCatClimbEngine({ seed: Infinity, mode: 'x' }).snapshot().mode, 'random')
  assert.deepEqual(createCatClimbEngine({ seed: Infinity }).snapshot(), initial)
})

test('snapshot 新嵌套对象，调用者无法修改物理状态', () => {
  const e = createCatClimbEngine()
  const before = e.snapshot()
  const changed = e.snapshot()
  assert.notEqual(before.cat, changed.cat)
  assert.notEqual(before.platforms, changed.platforms)
  assert.notEqual(before.platforms[0], changed.platforms[0])
  changed.cat.x = -999
  changed.platforms[0].width = 900
  changed.platforms.push(platform())
  assert.deepEqual(e.snapshot(), before)
  drive(e, { landings: 2 })
  const landed = e.snapshot()
  const another = e.snapshot()
  another.lastLanding.x = 900
  assert.deepEqual(e.snapshot(), landed)
})

test('随机函数种子 0、最大 uint32 均可确定，输出不恒定', () => {
  for (const seed of [0, 1, 0xffffffff]) {
    const a = createCatClimbRandom(seed)
    const b = createCatClimbRandom(seed)
    const values = Array.from({ length: 100 }, () => a())
    assert.deepEqual(values, Array.from({ length: 100 }, () => b()))
    assert.ok(values.every((v) => v >= 0 && v < 1))
    assert.ok(new Set(values).size > 90)
  }
})

test('证明拒绝无解横距、运动幅度、周期、平台边界与间距', () => {
  const from = platform(80, 100)
  for (const to of [platform(300, 168), platform(80, 200), platform(-5, 158),
    platform(80, 158, { type: 'moving', amplitude: 17, period: 4, phase: 0 }),
    platform(80, 158, { type: 'moving', amplitude: 12, period: 2.9, phase: 0 })]) {
    assert.equal(proveCatClimbSegment(from, to).valid, false)
  }
  assert.equal(proveCatClimbSegment(null, from).valid, false)
})

test('共享阶段边界明确：平台宽度、间距与阶段标签独立于 seed', () => {
  assert.equal(C.difficultyEvery, 5)
  assert.equal(C.difficultyMaxLevel, 4)
  for (const [index, stage, width, minGap, maxGap] of [
    [0, 'warmup', 95, 48, 56], [1, 'warmup', 95, 48, 56], [5, 'warmup', 95, 48, 56],
    [6, 'alternating', 85, 48, 60], [10, 'alternating', 85, 48, 60],
    [11, 'narrow', 72, 48, 64], [15, 'narrow', 72, 48, 64],
    [16, 'moving', 64, 48, 68], [20, 'moving', 64, 48, 68],
    [21, 'moving', 60, 50, 68], [50000, 'moving', 60, 50, 68],
  ]) {
    const settings = getCatClimbStage(index)
    assert.equal(settings.stage, stage)
    assert.equal(settings.label, CAT_CLIMB_STAGE_LABELS[stage])
    assert.equal(settings.width, width)
    assert.equal(settings.minGap, minGap)
    assert.equal(settings.maxGap, maxGap)
    assert.equal(settings.level, platformLevel(index))
    assert.equal(settings.difficultyLevel, settings.level)
    assert.equal(settings.minOffset, expectedDifficulties[settings.level].minOffset)
    assert.equal(settings.maxOffset, expectedDifficulties[settings.level].maxOffset)
    assert.equal(settings.movingBranchInterval, expectedDifficulties[settings.level].interval)
  }
  for (const bad of [-1, 1.5, NaN, Infinity, '4']) assert.throws(() => getCatClimbStage(bad), RangeError)
  for (const mode of ['random', 'fixed']) {
    for (let seed = 0; seed < 40; seed++) {
      const generator = createCatClimbGenerator({ seed, mode })
      let previous = generator.start()
      for (let index = 1; index <= 30; index++) {
        const { primary, branches } = generator.next()
        const gap = primary.y - previous.y
        const settings = expectedDifficulties[platformLevel(index)]
        assert.equal(primary.width, settings.width)
        assert.ok(gap >= settings.minGap && gap <= settings.maxGap)
        if (index === 1) {
          assert.ok(primary.baseX - previous.baseX >= settings.minOffset && primary.baseX - previous.baseX <= settings.maxOffset)
          assert.ok(primary.baseX - previous.baseX > primary.width / 2 + C.catHalfWidth, '首跳也在档位 offset 内，但无输入仍不可接台')
        }
        if (mode === 'fixed') assert.equal(gap, Math.floor((settings.minGap + settings.maxGap) / 2))
        if (!isForkIndex(index)) assert.equal(branches.length, 0)
        assert.ok(proveCatClimbSegment(previous, primary).valid)
        previous = primary
      }
    }
  }
})

test('实际物理落台驱动难度，飞过下一阶段高度不提前升级标签', () => {
  for (const mode of ['random', 'fixed']) {
    const engine = createCatClimbEngine({ seed: 92, mode })
    const seen = new Set()
    const crossed = new Set()
    const result = drive(engine, { landings: 24, onFrame(s) {
      const index = s.floor
      const stage = expectedDifficulties[reachedLevel(index)].stage
      assert.equal(s.difficultyStage, stage)
      assert.equal(s.difficultyLabel, CAT_CLIMB_STAGE_LABELS[stage])
      if (s.lastLanding) assert.equal(s.lastLanding.stage, expectedDifficulties[platformLevel(s.lastLanding.index)].stage)
      seen.add(stage)
      if ([4, 9, 14, 19].includes(index)) {
        const future = s.platforms.find((p) => p.index === index + 1 && p.primary)
        if (future && s.cat.y > future.y + 1) crossed.add(index)
      }
    } })
    assert.equal(result.landed.length, 24)
    assert.deepEqual([...seen], ['warmup', 'alternating', 'narrow', 'moving'])
    assert.deepEqual([...crossed], [4, 9, 14, 19])
    const movingStage = engine.snapshot()
    engine.dispatch({ type: 'pause' })
    assert.equal(engine.tick(100).difficultyStage, movingStage.difficultyStage)
    assert.equal(engine.dispatch({ type: 'resume' }).difficultyLabel, movingStage.difficultyLabel)
    assert.equal(engine.dispatch({ type: 'reset' }).difficultyStage, 'warmup')
  }
})

test('1000 种子 × 120 路段：全部 entry、移动两端、因果分叉与一层汇合证明', (t) => {
  let branchCount = 0
  const patterns = new Set()
  for (let seed = 0; seed < 1000; seed++) {
    const generator = createCatClimbGenerator({ seed })
    let previous = generator.start()
    let pending = []
    for (let index = 1; index <= 120; index++) {
      const segment = generator.next()
      const primary = segment.primary
      assert.equal(primary.primary, true)
      assert.equal(primary.type, 'fixed')
      const settings = expectedDifficulties[platformLevel(index)]
      assert.equal(primary.width, settings.width)
      assert.equal(primary.index, index)
      assert.equal(primary.difficultyLevel, platformLevel(index))
      assert.equal(primary.level, primary.difficultyLevel)
      assert.equal(primary.stage, settings.stage)
      assert.ok(primary.y - previous.y >= settings.minGap && primary.y - previous.y <= settings.maxGap)
      assert.ok(pending.length === 0 || segment.branches.length === 0, '汇合层不可连续分叉')
      const links = [previous, ...pending]
      for (const from of links) {
        const proof = proveCatClimbSegment(from, primary)
        assert.ok(proof.valid, `种子 ${seed}、${from.id} → ${primary.id}`)
        assert.ok(proof.gap >= 48 && proof.gap <= 68)
        // 独立检查整个支撑 entry 的两个极端，而非复用 valid 布尔值断言。
        for (const entry of proof.fromEntry) {
          const aim = Math.min(proof.landingInterval[1], Math.max(proof.landingInterval[0], entry))
          assert.ok(Math.abs(aim - entry) + C.horizontalSafety <= C.horizontalSpeed * proof.descendingTime + 1e-7)
          for (const phase of [0, Math.PI / 2, Math.PI, 3 * Math.PI / 2]) {
            const center = primary.baseX + (primary.amplitude ?? 0) * Math.sin(phase)
            const overlap = Math.min(aim + C.catHalfWidth, center + primary.width / 2)
              - Math.max(aim - C.catHalfWidth, center - primary.width / 2)
            assert.ok(overlap >= C.landingMargin - 1e-7)
          }
        }
      }
      for (const branch of segment.branches) {
        branchCount++
        assert.equal(branch.primary, false)
        assert.ok(isForkIndex(index))
        assert.equal(branch.index, index)
        assert.equal(branch.stage, settings.stage)
        assert.equal(branch.width, settings.width)
        assert.equal(branch.difficultyLevel, platformLevel(index))
        assert.ok(branch.baseX > primary.baseX, '固定主路线在左，慢移支路可选')
        assert.ok(branch.amplitude >= 12 && branch.amplitude <= 16 && branch.period >= 3.6 && branch.period <= 5.2)
        assert.equal(branch.y, primary.y)
        assert.equal(branch.rejoinsId, (index + 1) * 2)
        const proof = proveCatClimbSegment(previous, branch)
        assert.ok(proof.valid)
        for (const entry of proof.fromEntry) {
          const aim = Math.min(proof.landingInterval[1], Math.max(proof.landingInterval[0], entry))
          assert.ok(Math.abs(aim - entry) <= proof.horizontalBudget + 1e-7)
          for (const phase of [Math.PI / 2, 3 * Math.PI / 2]) {
            const center = branch.baseX + branch.amplitude * Math.sin(phase)
            const overlap = Math.min(aim + C.catHalfWidth, center + branch.width / 2)
              - Math.max(aim - C.catHalfWidth, center - branch.width / 2)
            assert.ok(overlap >= C.landingMargin - 1e-7)
          }
        }
      }
      pending = segment.branches
      previous = primary
      patterns.add(Math.round(primary.baseX / 20))
      const copy = segment.proofs[0]
      copy.fromEntry[0] = -999
      assert.ok(proveCatClimbSegment(links[0], primary).fromEntry[0] >= 12)
    }
  }
  assert.ok(branchCount >= 18000, `实际移动分支 ${branchCount}`)
  assert.ok(patterns.size >= 10, '主路线不能退化成同一竖线')
  t.diagnostic(`120000 主路线段、${branchCount} 移动分支及其下一层汇合通过全 entry 证明`)
})

test('1000 种子真实物理控制器：至少 42 次升层并实际落到移动分支', (t) => {
  let movingLandings = 0
  let minimumMoving = Infinity
  let maximumPlatforms = 0
  for (let seed = 0; seed < 1000; seed++) {
    const result = drive(createCatClimbEngine({ seed }), { landings: 42 })
    assert.equal(result.state.phase, 'running', `种子 ${seed} 不应掉落`)
    assert.equal(result.landed.length, 42, `种子 ${seed} 必须经过至少七个 chunk`)
    for (let i = 1; i < result.landed.length; i++) assert.ok(result.landed[i].y > result.landed[i - 1].y)
    const count = result.landed.filter((p) => p.type === 'moving').length
    assert.ok(count >= 3, `种子 ${seed} 实际移动落地只有 ${count}`)
    movingLandings += count
    minimumMoving = Math.min(minimumMoving, count)
    maximumPlatforms = Math.max(maximumPlatforms, result.maxPlatforms)
    assert.ok(result.state.height > 200)
  }
  t.diagnostic(`1000 种子共 42000 次真实升层，移动落地 ${movingLandings} 次，单种子最少 ${minimumMoving} 次，最多同时 ${maximumPlatforms} 台`)
})

for (const hz of [30, 60, 120]) {
  test(`${hz} Hz 控制器：40 种子经过多段与移动落点`, () => {
    for (let seed = 1000; seed < 1040; seed++) {
      const result = drive(createCatClimbEngine({ seed }), { landings: 48, hz })
      assert.equal(result.landed.length, 48, `种子 ${seed}`)
      assert.ok(result.landed.some((p) => p.type === 'moving'))
      assert.equal(result.state.phase, 'running')
    }
  })
}

for (const edge of [-1, 1]) {
  test(`固定主台${edge < 0 ? '左' : '右'}侧落点真实控制器仍可继续升层`, () => {
    for (let seed = 0; seed < 20; seed++) {
      const result = drive(createCatClimbEngine({ seed }), { landings: 48, moving: false, edge })
      assert.equal(result.landed.length, 48, `种子 ${seed}`)
      assert.equal(result.state.phase, 'running')
      assert.ok(result.landed.at(-1).y > 2500)
    }
  })
}

test('4000 次真实跳跃：平台剪枝、前瞻空间、镜头与高度持续有界', (t) => {
  let samples = 0
  const result = drive(createCatClimbEngine({ seed: 38721 }), { landings: 4000, onFrame(s, i) {
    if (i % 120 !== 0) return
    samples++
    assert.ok(s.platforms.length <= 60)
    assert.ok(s.platforms.every((p) => p.y >= s.cameraY - C.dropBuffer))
    assert.ok(s.platforms.at(-1).y >= s.cameraY + C.ahead)
    assert.ok(s.platforms.filter((p) => p.y > s.cat.y && p.y < s.cameraY + C.height).length >= 2)
  } })
  assert.equal(result.landed.length, 4000)
  assert.equal(result.state.phase, 'running')
  assert.ok(result.state.height > 20000)
  assert.ok(result.maxPlatforms < 60)
  assert.ok(result.state.platforms[0].id > 1000, '旧台必须被实际剪掉')
  t.diagnostic(`4000 次升层：${samples} 次前瞻检查，平台峰值 ${result.maxPlatforms}，高度 ${Math.floor(result.state.height)}`)
})

test('合法输入时钟可先推进旧方向再切换，暂停不改变本次最高值', () => {
  const e = createCatClimbEngine()
  e.dispatch({ type: 'start' })
  e.dispatch({ type: 'direction', direction: 1 })
  e.tick(0.1)
  const old = e.snapshot()
  e.dispatch({ type: 'direction', direction: -1 })
  assert.equal(e.snapshot().cat.x, old.cat.x)
  const paused = e.dispatch({ type: 'pause' })
  assert.equal(e.tick(100).height, paused.height)
  e.dispatch({ type: 'resume' })
  near(e.tick(0.1).cat.x, old.cat.x)
})

for (const difficultyEvery of [1, 3, 5, 8, 10]) {
  test(`每 ${difficultyEvery} 层：平台档位与实际落台快照分别按前后边界升级`, () => {
    const e = createCatClimbEngine({ seed: 192, difficultyEvery })
    const g = createCatClimbGenerator({ seed: 192, difficultyEvery })
    assert.equal(e.snapshot().difficultyEvery, difficultyEvery)
    assert.equal(e.snapshot().floor, 0)
    assert.equal(e.snapshot().level, 0)
    assert.equal(g.snapshot().difficultyEvery, difficultyEvery)
    let previous = g.start()
    for (let index = 1; index <= difficultyEvery * 5 + 2; index++) {
      const segment = g.next()
      const settings = expectedDifficulties[platformLevel(index, difficultyEvery)]
      const actual = getCatClimbStage(index, { difficultyEvery })
      assert.equal(actual.width, settings.width)
      assert.equal(actual.minGap, settings.minGap)
      assert.equal(actual.maxGap, settings.maxGap)
      assert.equal(actual.minOffset, settings.minOffset)
      assert.equal(actual.maxOffset, settings.maxOffset)
      assert.equal(actual.movingBranchInterval, settings.interval)
      assert.equal(segment.primary.level, platformLevel(index, difficultyEvery))
      assert.equal(segment.primary.width, settings.width)
      assert.ok(proveCatClimbSegment(previous, segment.primary).valid)
      const snapshot = g.snapshot()
      assert.equal(snapshot.index, index)
      assert.equal(snapshot.level, platformLevel(index, difficultyEvery))
      snapshot.last.width = -999
      assert.equal(g.snapshot().last.width, settings.width)
      previous = segment.primary
    }
    const boundaries = new Set()
    const result = drive(e, { landings: difficultyEvery * 5 + 2, onFrame(s) {
      assert.equal(s.difficultyLevel, reachedLevel(s.floor, difficultyEvery))
      if (s.floor > 0 && s.floor <= difficultyEvery * 4 && s.floor % difficultyEvery === 0) {
        assert.equal(s.lastLanding.stage, expectedDifficulties[s.level - 1].stage)
        assert.equal(s.difficultyStage, expectedDifficulties[s.level].stage)
        boundaries.add(s.floor)
      }
    } })
    assert.equal(result.landed.length, difficultyEvery * 5 + 2)
    assert.equal(boundaries.size, 4, '每个升档边界必须经过真实落台')
    assert.equal(result.state.level, 4)
    assert.equal(result.state.difficultyCapped, true)
    const initial = createCatClimbEngine({ seed: 192, difficultyEvery }).snapshot()
    assert.deepEqual(e.dispatch({ type: 'reset' }), initial)
    assert.equal(e.dispatch({ type: 'reset', difficultyEvery: 3 }).difficultyEvery, 3)
  })

  test(`每 ${difficultyEvery} 层 × 1000 种子：120 路段、最窄下一档与分支最坏 gap 有界证明`, (t) => {
    let branches = 0
    const bandCounts = [0, 0, 0, 0, 0]
    const generators = Array.from({ length: 1000 }, (_, seed) => createCatClimbGenerator({ seed, difficultyEvery }))
    for (const [seed, generator] of generators.entries()) {
      let previous = generator.start()
      let pending = []
      for (let index = 1; index <= 120; index++) {
        const segment = generator.next()
        const primary = segment.primary
        const level = platformLevel(index, difficultyEvery)
        const settings = expectedDifficulties[level]
        assert.equal(primary.width, settings.width)
        assert.equal(primary.level, level)
        assert.equal(primary.type, 'fixed')
        assert.ok(segment.proofs.every((p) => p.valid), `N=${difficultyEvery}、seed=${seed}、index=${index}`)
        assert.ok(!pending.length || !segment.branches.length, '必须先汇合，不能连续分叉')
        for (const from of [previous, ...pending]) assert.ok(proveCatClimbSegment(from, primary).valid)
        for (const branch of segment.branches) {
          branches++
          bandCounts[level]++
          assert.ok(isForkIndex(index, difficultyEvery))
          assert.equal(branch.width, settings.width)
          assert.ok(branch.baseX > primary.baseX)
          assert.ok(proveCatClimbSegment(previous, branch).valid)
          const nextSettings = expectedDifficulties[platformLevel(index + 1, difficultyEvery)]
          const origin = (primary.baseX + branch.baseX) / 2
          const worstNext = { ...primary, x: origin, baseX: origin,
            width: nextSettings.width, y: primary.y + nextSettings.maxGap }
          assert.ok(proveCatClimbSegment(primary, worstNext).valid)
          assert.ok(proveCatClimbSegment(branch, worstNext).valid, '汇合证明必须包含下一档宽度、最大 gap 和所有相位')
        }
        for (const p of [primary, ...segment.branches]) {
          assert.ok(p.width >= 60 && p.width <= 95)
          const amplitude = p.amplitude ?? 0
          assert.ok(p.baseX - p.width / 2 - amplitude >= 0)
          assert.ok(p.baseX + p.width / 2 + amplitude <= C.width)
          for (const phase of [0, Math.PI / 2, Math.PI, 3 * Math.PI / 2]) {
            const x = p.baseX + amplitude * Math.sin(phase)
            assert.ok(x - p.width / 2 >= 0 && x + p.width / 2 <= C.width)
          }
        }
        pending = segment.branches
        previous = primary
      }
    }
    assert.ok(branches >= 18000, `N=${difficultyEvery} 的移动分支不能退化为偶发事件：${branches}`)
    assert.ok(bandCounts[2] >= 1000, '每个 seed 的入门慢移档必须出现可达分支')
    assert.ok(bandCounts[4] > 0)
    t.diagnostic(`N=${difficultyEvery}：120000 主路、${branches} 移动分支；分档分支 ${bandCounts.join('/')}`)
  })
}

for (const difficultyEvery of [1, 3, 8, 10]) {
  test(`每 ${difficultyEvery} 层 × 1000 种子：42 次真实控制跳跃与移动分支落台`, (t) => {
    let moving = 0
    let minMoving = Infinity
    let maximumPlatforms = 0
    for (let seed = 0; seed < 1000; seed++) {
      const result = drive(createCatClimbEngine({ seed, difficultyEvery }), { landings: 42 })
      assert.equal(result.landed.length, 42, `N=${difficultyEvery}、seed=${seed}`)
      assert.equal(result.state.floor, 42, '42 次必须全是向上升层而非重复落台')
      assert.equal(result.state.phase, 'running')
      for (let i = 1; i < result.landed.length; i++) assert.ok(result.landed[i].index > result.landed[i - 1].index)
      const count = result.landed.filter((p) => p.type === 'moving').length
      assert.ok(count >= 3, `N=${difficultyEvery}、seed=${seed} 移动落台不足：${count}`)
      moving += count
      minMoving = Math.min(minMoving, count)
      maximumPlatforms = Math.max(maximumPlatforms, result.maxPlatforms)
      assert.ok(result.maxPlatforms <= C.maxPlatforms)
    }
    t.diagnostic(`N=${difficultyEvery}：42000 次真实升层、${moving} 次移动落台，单种子最少 ${minMoving}，平台峰值 ${maximumPlatforms}`)
  })
}

test('无效 difficultyEvery 明确拒绝，最大安全正整数仍可生成并复现', () => {
  for (const bad of [0, -1, 1.5, NaN, Infinity, '5', null, true, Number.MAX_SAFE_INTEGER + 1]) {
    assert.throws(() => createCatClimbEngine({ difficultyEvery: bad }), RangeError)
    assert.throws(() => createCatClimbGenerator({ difficultyEvery: bad }), RangeError)
    assert.throws(() => getCatClimbStage(1, { difficultyEvery: bad }), RangeError)
    const e = createCatClimbEngine({ difficultyEvery: 3 })
    const original = e.snapshot()
    assert.deepEqual(e.dispatch({ type: 'reset', difficultyEvery: bad, seed: 2 }), original)
  }
  for (const every of [undefined, 1, 3, 5, 8, 10, Number.MAX_SAFE_INTEGER]) {
    const a = createCatClimbGenerator({ seed: 0xffffffff, difficultyEvery: every })
    const b = createCatClimbGenerator({ seed: 0xffffffff, difficultyEvery: every })
    for (let i = 0; i < 120; i++) assert.deepEqual(a.next(), b.next())
    const fixedA = createCatClimbGenerator({ seed: 1, mode: 'fixed', difficultyEvery: every })
    const fixedB = createCatClimbGenerator({ seed: 99, mode: 'fixed', difficultyEvery: every })
    for (let i = 0; i < 120; i++) assert.deepEqual(fixedA.next(), fixedB.next())
  }
  assert.equal(getCatClimbStage(Number.MAX_SAFE_INTEGER, { difficultyEvery: Number.MAX_SAFE_INTEGER }).level, 0)
  assert.equal(getCatClimbStage(Number.MAX_SAFE_INTEGER, { difficultyEvery: 1 }).width, 60)
  assert.notDeepEqual(createCatClimbEngine({ mode: 'fixed', difficultyEvery: 1 }).snapshot().platforms,
    createCatClimbEngine({ mode: 'fixed', difficultyEvery: 10 }).snapshot().platforms)
})

for (const difficultyEvery of [1, 3, 5, 8, 10]) {
  test(`每 ${difficultyEvery} 层：同一平台反复跳跃不升级，峰高和跳数不充当层数`, () => {
    const e = createCatClimbEngine({ seed: 39, difficultyEvery })
    const firstResult = drive(e, { landings: 1 })
    assert.equal(firstResult.landed.length, 1)
    const first = firstResult.state.platforms.find((p) => p.id === firstResult.state.lastLanding.id)
    const second = firstResult.state.platforms.find((p) => p.index === 2 && p.primary)
    const side = Math.sign(first.baseX - second.baseX)
    const aim = Math.min(C.width - C.catHalfWidth, Math.max(C.catHalfWidth,
      first.baseX + side * (first.width / 2 + C.catHalfWidth - 8)))
    let s = firstResult.state
    const startJumps = s.jumps
    for (let frame = 0; frame < 1500 && s.jumps < startJumps + 8; frame++) {
      e.dispatch({ type: 'direction', direction: Math.abs(aim - s.cat.x) <= C.horizontalSpeed * C.step / 2 ? 0 : Math.sign(aim - s.cat.x) })
      s = e.tick(C.step)
      assert.equal(s.phase, 'running')
      assert.equal(s.floor, 1)
      assert.equal(s.lastLanding.index, 1)
      assert.equal(s.level, reachedLevel(1, difficultyEvery))
    }
    assert.equal(s.jumps, startJumps + 8)
    assert.ok(s.height > firstResult.state.height)
  })
}

test('回落至较低真实平台：最大 index、难度及暂停时钟不得回退', () => {
  let checked = false
  for (let seed = 0; seed < 40 && !checked; seed++) {
    const e = createCatClimbEngine({ seed })
    const result = drive(e, { landings: 5, moving: false })
    const initial = result.state
    assert.equal(initial.floor, 5)
    const lower = initial.platforms.find((p) => p.index === 2 && p.primary)
    const candidates = [-1, -0.5, 0, 0.5, 1].map((side) => Math.min(C.width - C.catHalfWidth, Math.max(C.catHalfWidth,
      lower.baseX + side * (lower.width / 2 + C.catHalfWidth - 4))))
    const aim = candidates.find((x) => initial.platforms.filter((p) => p.y >= initial.lastLanding.y
      && p.y <= initial.cat.y + C.jumpSpeed ** 2 / (2 * C.gravity))
      .every((p) => Math.abs(x - p.baseX) > p.width / 2 + C.catHalfWidth + (p.amplitude ?? 0) + 2))
    if (aim === undefined) continue
    let s = initial
    for (let i = 0; i < 240 && s.jumps === initial.jumps && s.phase === 'running'; i++) {
      e.dispatch({ type: 'direction', direction: Math.abs(aim - s.cat.x) < C.horizontalSpeed * C.step / 2 ? 0 : Math.sign(aim - s.cat.x) })
      s = e.tick(C.step)
    }
    assert.equal(s.phase, 'running', `seed=${seed} 必须实际落到下层而不是掉落结束`)
    assert.ok(s.lastLanding.index < initial.floor)
    assert.equal(s.floor, 5)
    assert.equal(s.maxLandingIndex, 5)
    assert.equal(s.difficultyLevel, 1)
    assert.ok(reachedLevel(s.lastLanding.index) < s.level, '落入较低档位平台也不可降低当前难度')
    assert.equal(s.difficultyStage, 'alternating')
    const paused = e.dispatch({ type: 'pause' })
    assert.deepEqual(e.tick(1000), paused)
    assert.equal(e.dispatch({ type: 'resume' }).floor, 5)
    checked = true
  }
  assert.ok(checked, '必须成功执行一次真实回落，不接受空循环通过')
})

// 独立控制全 entry 极端：只使用既有水平速度、重力和连续碰撞，不复用 proof.valid 结论。
function controlSegment(from, to, entry, time) {
  const amplitude = to.amplitude ?? 0
  const controlMargin = C.landingMargin + C.horizontalSpeed * C.step
  const lo = Math.max(C.catHalfWidth, to.baseX - to.width / 2 + amplitude - C.catHalfWidth + controlMargin)
  const hi = Math.min(C.width - C.catHalfWidth, to.baseX + to.width / 2 - amplitude + C.catHalfWidth - controlMargin)
  const aim = Math.min(hi, Math.max(lo, entry))
  const cat = { x: entry, y: from.y, vx: 0, vy: C.jumpSpeed }
  for (let elapsed = 0; elapsed < 1; elapsed += C.step) {
    cat.vx = Math.abs(aim - cat.x) <= C.horizontalSpeed * C.step / 2 ? 0 : Math.sign(aim - cat.x) * C.horizontalSpeed
    const hit = sweepCatClimbLanding(cat, [to], C.step, time + elapsed)
    if (hit) {
      const center = getCatClimbPlatformX(to, time + elapsed + hit.time)
      const overlap = Math.min(hit.x + C.catHalfWidth, center + to.width / 2)
        - Math.max(hit.x - C.catHalfWidth, center - to.width / 2)
      assert.ok(overlap >= C.landingMargin - 1e-7, '极端起跳位置也要保留落台余量')
      return hit
    }
    cat.x = Math.min(C.width - C.catHalfWidth, Math.max(C.catHalfWidth, cat.x + cat.vx * C.step))
    cat.y += cat.vy * C.step - C.gravity * C.step ** 2 / 2
    cat.vy -= C.gravity * C.step
  }
  assert.fail(`真实速度无法接住 ${from.id} → ${to.id}，entry=${entry}`)
}

test('N=1/3/5/8/10 模式片段：最坏 entry、下一档宽度、最大 gap、周期和相位实际控制可达', (t) => {
  let samples = 0
  for (const difficultyEvery of [1, 3, 5, 8, 10]) {
    for (let seed = 0; seed < 40; seed++) {
      const g = createCatClimbGenerator({ seed, difficultyEvery })
      let previous = g.start()
      let pending = []
      for (let index = 1; index <= 60; index++) {
        const { primary, branches } = g.next()
        const settings = expectedDifficulties[platformLevel(index, difficultyEvery)]
        for (const from of [previous, ...pending]) {
          for (const target of [primary, ...branches]) {
            const to = { ...target, y: from.y + settings.maxGap }
            const a = from.amplitude ?? 0
            const entries = [Math.max(C.catHalfWidth, from.baseX - from.width / 2 - a - C.catHalfWidth),
              Math.min(C.width - C.catHalfWidth, from.baseX + from.width / 2 + a + C.catHalfWidth)]
            for (const entry of entries) {
              for (const period of [3.6, 5.2]) {
                for (const phase of [0, Math.PI / 2, Math.PI, 3 * Math.PI / 2]) {
                  controlSegment(from, { ...to, period, phase }, entry, 0.73)
                  samples++
                }
              }
            }
          }
        }
        pending = branches
        previous = primary
      }
    }
  }
  t.diagnostic(`${samples} 个极端 entry/周期/相位组合以真实控制与连续碰撞落台`)
})

test('补充安全正整数 N 与固定路线：连续生成不抛错，平台有界且 seed 一致', () => {
  for (const difficultyEvery of [2, 4, 6, 7, 9, 11, 17, 99, 1024, Number.MAX_SAFE_INTEGER]) {
    for (let seed = 0; seed < 100; seed++) {
      const g = createCatClimbGenerator({ difficultyEvery, seed })
      let previous = g.start()
      for (let index = 1; index <= 160; index++) {
        const segment = g.next()
        assert.equal(segment.primary.width, expectedDifficulties[platformLevel(index, difficultyEvery)].width)
        assert.ok(segment.proofs.every((p) => p.valid))
        assert.ok(proveCatClimbSegment(previous, segment.primary).valid)
        for (const p of [segment.primary, ...segment.branches]) {
          const amplitude = p.amplitude ?? 0
          assert.ok(p.baseX - p.width / 2 - amplitude >= 0 && p.baseX + p.width / 2 + amplitude <= 360)
        }
        previous = segment.primary
      }
    }
  }
  for (const difficultyEvery of [1, 3, 5, 8, 10]) {
    const a = createCatClimbEngine({ mode: 'fixed', difficultyEvery, seed: 0 })
    const b = createCatClimbEngine({ mode: 'fixed', difficultyEvery, seed: 0xffffffff })
    const initial = a.snapshot()
    assert.deepEqual(initial.platforms, b.snapshot().platforms)
    const result = drive(a, { landings: 60 })
    assert.equal(result.landed.length, 60)
    assert.equal(result.state.floor, 60)
    assert.equal(result.state.level, 4)
    assert.ok(result.landed.filter((p) => p.type === 'moving').length >= 3)
    assert.deepEqual(a.dispatch({ type: 'reset' }), initial)
  }
})

test('最宽 95 到最窄 60 的极端过渡：最大 gap 夹持有余量，不越界且真实控制能落台', () => {
  for (const side of [-1, 1]) {
    const from = platform(side > 0 ? 120 : 240, 100, { id: 0, width: 95 })
    const unsafe = platform(from.baseX + side * 76, 168, { id: 2, width: 60 })
    assert.equal(proveCatClimbSegment(from, unsafe).valid, false)
    const budget = C.horizontalSpeed * catClimbDescendingTime(68) - C.horizontalSafety
    const offset = budget - (95 - 60) / 2 - C.landingMargin
    const to = platform(from.baseX + side * offset, 168, { id: 2, width: 60 })
    const proof = proveCatClimbSegment(from, to)
    assert.ok(proof.valid)
    assert.ok(to.baseX - 30 >= 0 && to.baseX + 30 <= C.width)
    for (const entry of proof.fromEntry) assert.equal(controlSegment(from, to, entry, 0).id, to.id)
  }
})

// 执行真实编译后的 setup；宿主只替换 DOM/RAF。此处不作为浏览器视觉验收。
async function mountComponent() {
  const source = readFileSync(new URL('./components/CatClimb.vue', import.meta.url), 'utf8')
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
    CatSprite: {}, WORLD: C, createCatClimbEngine }
  runInNewContext(compiled, context)
  const api = context.component.setup({ species: 'black', catName: '小猫', standalone: true },
    { expose() {}, emit: (name) => emitted.push(name) })
  const stage = element('climb-stage')
  const left = element('climb-left')
  const right = element('climb-right', { left: 90, top: 0, right: 170, bottom: 48, width: 80, height: 48 })
  const close = element('climb-close')
  const start = element('climb-start')
  const pause = element('climb-pause')
  const restart = element('climb-restart')
  api.dialog.value = { contains: (target) => [stage, left, right, close, start, pause, restart].includes(target),
    querySelectorAll: () => [stage, start, left, right, pause, close] }
  api.stage.value = stage
  api.scene.value = element('scene', { width: 200, height: 300 })
  api.playfield.value = element('climb-playfield', { left: 0, top: 0, right: 360, bottom: 560, width: 360, height: 560 })
  api.startButton.value = start
  mountedCallbacks.forEach((fn) => fn())
  await Promise.resolve()
  function event(extra = {}) {
    return { preventDefault() { this.prevented = true }, stopPropagation() { this.stopped = true }, ...extra }
  }
  return { api, frames, document, window, targets, emitted, stage, left, right, close, start, pause, restart, external, event,
    setTime: (time) => { now = time },
    fireFrame(time) {
      now = time
      const entry = frames.entries().next().value
      assert.ok(entry, '必须有真实组件登记的 RAF')
      frames.delete(entry[0])
      entry[1](time)
    },
    unmount() { unmountCallbacks.forEach((fn) => fn()) }, isDisconnected: () => disconnected }
}

test('真实 setup：从暂停按钮恢复 running 后聚焦场地且方向键可立即使用', async () => {
  const h = await mountComponent()
  h.api.start()
  h.fireFrame(100)
  h.pause.focus()
  h.api.togglePause()
  assert.equal(h.api.state.value.phase, 'paused')
  assert.equal(h.api.keyboardTarget(h.pause), false)
  const paused = h.api.state.value
  h.setTime(90000)
  h.api.togglePause()
  assert.equal(h.document.activeElement, h.stage)
  assert.equal(h.stage.lastFocusOptions.preventScroll, true)
  assert.equal(h.api.state.value.simTime, paused.simTime)
  h.api.keyboardDown(h.event({ code: 'KeyD', key: 'd', target: h.document.activeElement }))
  h.fireFrame(90100)
  near(h.api.state.value.cat.x, paused.cat.x + 16)
  h.api.keyboardUp(h.event({ code: 'KeyD', key: 'd' }))
  const stopped = h.api.state.value.cat.x
  h.fireFrame(90200)
  near(h.api.state.value.cat.x, stopped)
  for (const button of [h.pause, h.restart, h.close, h.start]) assert.equal(h.api.keyboardTarget(button), false)
  h.unmount()
})

test('真实 setup：随机重开每局换种子，固定重开重复，切回随机继续换种子', async () => {
  const h = await mountComponent()
  const seeds = new Set([h.api.state.value.seed])
  const originalRoute = h.api.state.value.platforms
  for (let i = 0; i < 20; i++) {
    h.api.start()
    const previous = h.api.state.value.seed
    await h.api.restart()
    assert.notEqual(h.api.state.value.seed, previous)
    assert.equal(seeds.has(h.api.state.value.seed), false)
    seeds.add(h.api.state.value.seed)
    assert.equal(h.api.state.value.phase, 'ready')
    assert.equal(h.frames.size, 0)
    assert.notDeepEqual(h.api.state.value.platforms, originalRoute)
  }
  await h.api.restart('fixed')
  const fixed = h.api.state.value
  for (let i = 0; i < 5; i++) {
    h.api.start()
    await h.api.restart()
    assert.equal(h.api.state.value.seed, fixed.seed)
    assert.deepEqual(h.api.state.value.platforms, fixed.platforms)
  }
  await h.api.restart('random')
  assert.notEqual(h.api.state.value.seed, fixed.seed)
  assert.equal(seeds.has(h.api.state.value.seed), false)
  assert.equal(h.api.state.value.mode, 'random')
  h.unmount()
})

test('真实 setup：ready 尺寸暂停后恢复仍等待开始，暂停回合重开也不自动启动', async () => {
  const h = await mountComponent()
  const initial = h.api.state.value
  h.api.resize()
  await Promise.resolve()
  assert.equal(h.api.state.value.phase, 'paused')
  h.pause.focus()
  h.api.togglePause()
  assert.equal(h.api.state.value.phase, 'ready')
  assert.equal(h.api.state.value.simTime, 0)
  assert.equal(h.api.state.value.seed, initial.seed)
  assert.equal(h.frames.size, 0)
  assert.notEqual(h.document.activeElement, h.stage)
  h.api.keyboardDown(h.event({ code: 'KeyD', key: 'd', target: h.stage }))
  assert.equal(h.api.state.value.cat.vx, 0)
  h.api.start()
  h.api.suspend()
  await h.api.restart()
  assert.equal(h.api.state.value.phase, 'ready')
  assert.equal(h.document.activeElement, h.start)
  assert.equal(h.frames.size, 0)
  assert.equal(h.api.state.value.simTime, 0)
  h.api.start()
  assert.equal(h.api.state.value.phase, 'running')
  assert.equal(h.frames.size, 1)
  h.unmount()
})

test('真实 setup：指针取消后不仅 vx 为零，后续物理帧也确实不再横移', async () => {
  const h = await mountComponent()
  h.api.start()
  h.api.pointerDown(h.event({ currentTarget: h.right, pointerId: 2, button: 0 }), 1)
  h.fireFrame(100)
  near(h.api.state.value.cat.x, 196)
  h.api.cancelPointer(h.event({ pointerId: 2 }))
  const stopped = h.api.state.value
  assert.equal(stopped.cat.vx, 0)
  h.fireFrame(200)
  h.fireFrame(300)
  near(h.api.state.value.cat.x, stopped.cat.x)
  assert.ok(h.api.state.value.simTime > stopped.simTime)
  assert.notEqual(h.api.state.value.cat.y, stopped.cat.y)
  h.unmount()
})

test('真实 setup：场地按可用区域完整缩放，尺寸变化只暂停且保持世界与回合', async () => {
  const h = await mountComponent()
  const original = h.api.state.value
  // 代表扩宽横屏与紧凑竖屏的可用区域；不是浏览器测量结果。
  for (const [width, height] of [[367, 304], [284, 272]]) {
    h.api.scene.value.getBoundingClientRect = () => ({ width, height })
    h.api.fitStage()
    const size = h.api.stageSize.value
    assert.ok(size.width <= width && size.height <= height)
    assert.ok(size.width >= 170)
    near(size.width / size.height, 360 / 560)
    assert.equal(h.api.state.value.phase, 'paused')
    assert.equal(h.api.state.value.seed, original.seed)
    assert.deepEqual(h.api.state.value.cat, original.cat)
    h.api.togglePause()
    assert.equal(h.api.state.value.phase, 'ready')
    assert.equal(h.frames.size, 0)
  }
  h.api.start()
  h.fireFrame(100)
  const running = h.api.state.value
  h.api.scene.value.getBoundingClientRect = () => ({ width: 350, height: 300 })
  h.api.fitStage()
  assert.equal(h.api.state.value.phase, 'paused')
  assert.equal(h.api.state.value.height, running.height)
  assert.equal(h.api.state.value.simTime, running.simTime)
  h.api.togglePause()
  assert.equal(h.api.state.value.phase, 'running')
  assert.equal(h.document.activeElement, h.stage)
  const source = readFileSync(new URL('./components/CatClimb.vue', import.meta.url), 'utf8')
  const style = parse(source).descriptor.styles[0].content
  assert.match(style, /\.cat-climb \{[\s\S]*?display: flex; flex-direction: column;/)
  assert.match(style, /\.climb-scene \{[\s\S]*?width: calc\(100% - 2 \* clamp\(116px, 20vw, 190px\)\)/)
  assert.match(style, /@media \(orientation: portrait\) and \(max-width: 600px\)/)
  assert.match(style, /user-select: none; -webkit-user-select: none; -webkit-touch-callout: none;/)
  assert.ok(!style.includes('.climb-panel'))
  h.unmount()
})

test('真实 setup：ready 无 RAF，开始后有一条 RAF，暂停与结束停止循环', async () => {
  const h = await mountComponent()
  assert.equal(h.frames.size, 0)
  h.api.start()
  assert.equal(h.frames.size, 1)
  h.fireFrame(100)
  near(h.api.state.value.simTime, 0.1)
  h.api.togglePause()
  assert.equal(h.frames.size, 0)
  const paused = h.api.state.value
  h.setTime(90000)
  h.api.togglePause()
  assert.equal(h.api.state.value.simTime, paused.simTime)
  assert.equal(h.frames.size, 1)
  h.fireFrame(90100)
  near(h.api.state.value.simTime, 0.2)
  for (let i = 1; i <= 20 && h.frames.size; i++) h.fireFrame(90100 + i * 100)
  assert.equal(h.api.state.value.phase, 'ended')
  assert.equal(h.frames.size, 0)
  h.unmount()
})

test('真实 setup：双指反方向抵消，取消单指只释放自己的捕获', async () => {
  const h = await mountComponent()
  h.api.start()
  h.api.pointerDown(h.event({ currentTarget: h.left, pointerId: 1, button: 0 }), -1)
  assert.equal(h.api.state.value.cat.vx, -160)
  h.api.pointerDown(h.event({ currentTarget: h.right, pointerId: 2, button: 0, isPrimary: false }), 1)
  assert.equal(h.api.state.value.cat.vx, 0)
  assert.equal(h.api.pointers.size, 2)
  h.api.cancelPointer(h.event({ pointerId: 1 }))
  assert.equal(h.api.state.value.cat.vx, 160)
  assert.equal(h.left.captures.size, 0)
  assert.equal(h.right.captures.size, 1)
  h.api.cancelPointer(h.event({ pointerId: 2 }))
  assert.equal(h.api.state.value.cat.vx, 0)
  assert.equal(h.api.pointers.size, 0)
  h.unmount()
})

test('真实 setup：触点移出热区与 lost capture 清除对应输入', async () => {
  const h = await mountComponent()
  h.api.start()
  h.api.pointerDown(h.event({ currentTarget: h.right, pointerId: 2, button: 0 }), 1)
  h.api.pointerMove(h.event({ pointerId: 2, clientX: 200, clientY: 20 }))
  assert.equal(h.api.state.value.cat.vx, 0)
  assert.equal(h.api.pointers.size, 0)
  h.api.pointerDown(h.event({ currentTarget: h.left, pointerId: 3, button: 0 }), -1)
  h.left.releasePointerCapture(3)
  h.api.cancelPointer(h.event({ pointerId: 3 }))
  assert.equal(h.api.state.value.cat.vx, 0)
  h.unmount()
})

test('真实 setup：取消指针补算至 ended 时，不读取已清除的 pointer map', async () => {
  const h = await mountComponent()
  h.api.start()
  h.api.pointerDown(h.event({ currentTarget: h.left, pointerId: 1, button: 0 }), -1)
  for (let i = 1; i <= 10; i++) h.fireFrame(i * 100)
  assert.equal(h.api.state.value.phase, 'running')
  h.setTime(1200)
  h.api.cancelPointer(h.event({ pointerId: 1 }))
  assert.equal(h.api.state.value.phase, 'running')
  h.api.pointerDown(h.event({ currentTarget: h.left, pointerId: 1, button: 0 }), -1)
  h.setTime(1300)
  assert.doesNotThrow(() => h.api.cancelPointer(h.event({ pointerId: 1 })))
  assert.equal(h.api.state.value.phase, 'ended')
  assert.equal(h.api.pointers.size, 0)
  assert.equal(h.frames.size, 0)
  h.unmount()
})

test('真实 setup：键盘输入先归旧时间，Tab/焦点切换释放方向且不吞功能按钮', async () => {
  const h = await mountComponent()
  h.api.start()
  h.api.keyboardDown(h.event({ code: 'ArrowRight', key: 'ArrowRight', target: h.stage }))
  h.setTime(100)
  h.api.keyboardDown(h.event({ code: 'ArrowLeft', key: 'ArrowLeft', target: h.stage }))
  near(h.api.state.value.cat.x, 196)
  assert.equal(h.api.state.value.cat.vx, 0)
  h.api.keyboardUp(h.event({ code: 'ArrowRight', key: 'ArrowRight' }))
  assert.equal(h.api.state.value.cat.vx, -160)
  h.api.keyboardDown(h.event({ key: 'Tab', target: h.stage }))
  assert.equal(h.api.state.value.cat.vx, 0)
  assert.equal(h.api.keys.size, 0)
  h.api.keyboardDown(h.event({ code: 'KeyD', key: 'd', target: h.close }))
  assert.equal(h.api.state.value.cat.vx, 0)
  h.api.keyboardDown(h.event({ code: 'KeyA', key: 'a', target: h.stage }))
  h.api.focusOut(h.event({ target: h.stage }))
  assert.equal(h.api.state.value.cat.vx, 0)
  h.unmount()
})

test('真实 setup：resize/blur/visibility 保留高度与回合，继续不补后台', async () => {
  const h = await mountComponent()
  h.api.start()
  h.api.keyboardDown(h.event({ code: 'ArrowRight', key: 'ArrowRight', target: h.stage }))
  h.fireFrame(100)
  const running = h.api.state.value
  h.api.resize()
  await Promise.resolve()
  assert.equal(h.api.state.value.phase, 'paused')
  assert.equal(h.api.state.value.height, running.height)
  assert.equal(h.api.state.value.simTime, running.simTime)
  assert.equal(h.api.state.value.cat.vx, 0)
  assert.equal(h.api.keys.size, 0)
  assert.equal(h.frames.size, 0)
  h.setTime(90000)
  h.api.togglePause()
  assert.equal(h.api.state.value.height, running.height)
  h.api.suspend()
  assert.equal(h.api.state.value.phase, 'paused')
  h.document.hidden = true
  h.api.visibilityChanged()
  h.api.togglePause()
  assert.equal(h.api.state.value.phase, 'paused')
  h.document.hidden = false
  h.api.togglePause()
  assert.equal(h.api.state.value.phase, 'running')
  h.unmount()
})

test('真实 setup：退出/Escape、focus trap、重开与卸载彻底清理并恢复焦点', async () => {
  const h = await mountComponent()
  h.document.activeElement = h.restart
  const tab = h.event({ key: 'Tab', target: h.restart })
  h.api.keyboardDown(tab)
  assert.equal(tab.prevented, true)
  assert.equal(h.document.activeElement, h.stage)
  h.api.start()
  await h.api.restart('fixed')
  assert.equal(h.api.state.value.mode, 'fixed')
  assert.equal(h.api.state.value.phase, 'ready')
  assert.equal(h.frames.size, 0)
  assert.equal(h.document.activeElement, h.start)
  h.api.start()
  h.api.pointerDown(h.event({ currentTarget: h.left, pointerId: 3, button: 0 }), -1)
  h.api.keyboardDown(h.event({ key: 'Escape' }))
  assert.deepEqual(h.emitted, ['close'])
  assert.equal(h.frames.size, 0)
  assert.equal(h.api.pointers.size, 0)
  h.api.requestClose()
  assert.deepEqual(h.emitted, ['close'])
  h.unmount()
  assert.equal(h.document.activeElement, h.external)
  assert.equal(h.isDisconnected(), true)
  assert.ok(h.targets.every((target) => [...target.listeners.values()].every((set) => set.size === 0)))
})

test('真实 setup：整屏左右半区兜底热区按住生效，按按钮或非运行态不抢输入', async () => {
  const h = await mountComponent()
  h.api.start()
  const field = h.api.playfield.value
  const at = (x, target = null, pointerId = 7) => h.event({ currentTarget: field, target: target || field,
    pointerId, button: 0, clientX: x, clientY: 300 })
  h.api.playfieldPointerDown(at(60, { closest: () => ({}) }))
  assert.equal(h.api.pointers.size, 0, '按在按钮上由按钮自己处理')
  h.api.playfieldPointerDown(at(60))
  assert.equal(h.api.pointers.get(7).direction, -1, '左半屏向左')
  h.api.cancelPointer(h.event({ pointerId: 7 }))
  assert.equal(h.api.pointers.size, 0)
  h.api.playfieldPointerDown(at(300, null, 8))
  assert.equal(h.api.pointers.get(8).direction, 1, '右半屏向右')
  h.api.cancelPointer(h.event({ pointerId: 8 }))
  h.api.suspend()
  h.api.playfieldPointerDown(at(60, null, 9))
  assert.equal(h.api.pointers.size, 0, '非运行态不产生方向输入')
  h.unmount()
})

test('全屏模板只在运行中显示方向键与暂停，其余按钮收进状态层', () => {
  const source = readFileSync(new URL('./components/CatClimb.vue', import.meta.url), 'utf8')
  const template = parse(source).descriptor.template.content
  assert.match(template, /@selectstart\.prevent @dragstart\.prevent/)
  assert.match(template, /@pointerdown\.stop="pointerDown\(\$event, -1\)"/)
  assert.match(template, /ref="playfield"/)
  assert.match(template, /v-if="state\.phase === 'running'"/)
  assert.match(template, /v-else-if="state\.phase === 'ready'"/)
  assert.match(template, /v-else-if="state\.phase === 'paused'"/)
  assert.match(template, /v-else-if="state\.phase === 'ended'"/)
  assert.doesNotMatch(template, /climb-panel|climb-sidebar|climb-directions/)
  const runningDirs = template.match(/<template v-if="state\.phase === 'running'">([\s\S]*?)<\/template>/)[1]
  assert.equal((runningDirs.match(/class="climb-dir/g) ?? []).length, 2, '运行中只有两个大方向键')
  assert.equal((runningDirs.match(/climb-start|climb-restart|climb-close/g) ?? []).length, 0)
  assert.match(template, /class="climb-pause" @click="togglePause"/)
  assert.match(template, /<span class="climb-label">/, '按钮文字不接收指针事件')
})

test('真实 setup：层数单值、难度前沿与封顶 caption 响应当下快照，不显示重复高度', async () => {
  const h = await mountComponent()
  assert.equal(h.api.difficultyCaption.value, '每5层升档')
  const levels = new Set()
  const result = drive(h.api.engine, { landings: 24, onFrame(s) {
    h.api.state.value = s
    assert.equal(h.api.state.value.floor, s.floor)
    assert.equal(h.api.state.value.difficultyLevel, reachedLevel(s.floor))
    assert.equal(h.api.difficultyCaption.value, s.floor >= 20 ? '每5层升档 · 已到难度上限' : '每5层升档')
    levels.add(s.level)
  } })
  assert.equal(result.landed.length, 24)
  assert.deepEqual([...levels], [0, 1, 2, 3, 4])
  const source = readFileSync(new URL('./components/CatClimb.vue', import.meta.url), 'utf8')
  const hud = source.match(/<div class="climb-hud">([\s\S]*?)<\/div>/)[1]
  assert.equal((hud.match(/state\.floor/g) ?? []).length, 1, 'HUD 只有一个实际层数，不重复挤屏')
  assert.match(hud, /第 <strong>\{\{ state\.floor \}\}<\/strong> 层/)
  assert.match(hud, /难度 \{\{ state\.difficultyLevel \}\}/)
  assert.match(hud, /difficultyCaption/)
  assert.doesNotMatch(hud, /displayHeight|difficultyLabel/)
  h.api.state.value = h.api.engine.dispatch({ type: 'reset', difficultyEvery: 3 })
  assert.equal(h.api.difficultyCaption.value, '每3层升档')
  assert.equal(h.api.state.value.floor, 0)
  assert.equal(h.api.state.value.level, 0)
  h.unmount()
})

test('每层升档的无限生成仍剪枝有界，移动平台暂停时钟完全冻结', () => {
  const e = createCatClimbEngine({ seed: 38721, difficultyEvery: 1 })
  const result = drive(e, { landings: 4000, onFrame(s, frame) {
    if (frame % 120) return
    assert.ok(s.platforms.length <= C.maxPlatforms)
    assert.ok(s.platforms.every((p) => p.y >= s.cameraY - C.dropBuffer && p.width >= 60))
    assert.ok(s.platforms.at(-1).y >= s.cameraY + C.ahead)
    if (s.floor >= 4) assert.equal(s.level, 4)
  } })
  assert.equal(result.landed.length, 4000)
  assert.equal(result.state.floor, 4000)
  assert.equal(result.state.phase, 'running')
  assert.ok(result.state.platforms[0].index > 3900)
  assert.ok(result.state.platforms.some((p) => p.type === 'moving'))
  const paused = e.dispatch({ type: 'pause' })
  for (const dt of [C.step / 2, 0.1, 1000]) assert.deepEqual(e.tick(dt), paused)
  const resumed = e.dispatch({ type: 'resume' })
  assert.equal(resumed.simTime, paused.simTime)
  assert.deepEqual(resumed.platforms, paused.platforms)
  assert.equal(resumed.floor, paused.floor)
  assert.equal(resumed.level, 4)
  const next = e.tick(C.step)
  near(next.simTime - resumed.simTime, C.step)
  assert.equal(next.cat.vx, 0)
  assert.deepEqual(e.dispatch({ type: 'reset' }), createCatClimbEngine({ seed: 38721, difficultyEvery: 1 }).snapshot())
})

test('Vue 独立组件脚本、模板、样式实际编译，无 API 或持久化依赖', () => {
  const filename = new URL('./components/CatClimb.vue', import.meta.url)
  const source = readFileSync(filename, 'utf8')
  const parsed = parse(source, { filename: filename.pathname })
  assert.deepEqual(parsed.errors, [])
  const script = compileScript(parsed.descriptor, { id: 'cat-climb-test' })
  const template = compileTemplate({ source: parsed.descriptor.template.content, filename: filename.pathname,
    id: 'cat-climb-test', compilerOptions: { bindingMetadata: script.bindings } })
  assert.deepEqual(template.errors, [])
  const style = compileStyle({ source: parsed.descriptor.styles[0].content, filename: filename.pathname,
    id: 'cat-climb-test', scoped: true })
  assert.deepEqual(style.errors, [])
  assert.ok(script.content.includes('createCatClimbEngine'))
  assert.equal(/localStorage|sessionStorage|axios|fetch\(|setInterval|console\.info/.test(source), false)
  for (const selector of ['cat-climb', 'climb-stage', 'climb-cat', 'climb-platform', 'climb-left', 'climb-right',
    'climb-start', 'climb-pause', 'climb-restart', 'climb-close', 'climb-height', 'climb-difficulty', 'climb-result']) assert.ok(source.includes(selector))
  assert.ok(source.includes(':data-difficulty="state.difficultyStage"'))
  assert.ok(source.includes(':data-stage="platform.stage"'))
  assert.ok(source.includes(':data-index="platform.index"'))
})
