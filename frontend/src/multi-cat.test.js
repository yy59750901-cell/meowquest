import test from 'node:test'
import assert from 'node:assert/strict'
import { createReservations, createMealTransaction, mealPositions, mergeSnapshot, mealGeometry, mealMouthLocal, actorSeat, mealBatches, mealWaitingPositions, planMealArrival, mealPlacement } from './multi-cat.js'
import { planActorSeats } from './room-interaction.js'

const storage = () => {
  const items = new Map()
  return { getItem: key => items.get(key), setItem: (key, value) => items.set(key, value), removeItem: key => items.delete(key) }
}
test('家具 reservation 按实例和猫身份排他，释放其他猫不能偷走锁', () => {
  const locks = createReservations()
  assert.equal(locks.acquire(8, 1), true)
  assert.equal(locks.acquire('8', '1'), true)
  assert.equal(locks.acquire(8, 2), false)
  locks.release(8, 2)
  assert.equal(locks.owner(8), '1')
  assert.equal(locks.acquire(9, 2), true)
  locks.releaseCat(1)
  assert.equal(locks.owner(8), undefined)
  assert.equal(locks.owner(9), '2')
  locks.clear(); assert.equal(locks.owner(9), undefined)
})
test('旧单 pet snapshot 不把选中猫心情覆盖给其他猫，只投影共享成长', () => {
  const previous = [{ id: 1, mood: 20, hunger: 30 }, { id: 2, mood: 90, hunger: 80 }]
  const result = mergeSnapshot({ pet: { mood: 25, hunger: 60, balance: 40, level: 3 } }, previous, 1)
  assert.deepEqual(result.pets.map(p => [p.id, p.mood, p.hunger, p.balance]), [[1, 25, 60, 40], [2, 90, 80, 40]])
  assert.equal(mergeSnapshot({}), null)
})
test('账户的附加 id/name 字段不能覆盖猫身份或每猫心情', () => {
  const result = mergeSnapshot({ pet: { id: 1 }, pets: [{ id: 1, name: '橘子', mood: 20 }, { id: 2, name: '小黑', mood: 90 }], account: { id: 7, name: '孩子账户', mood: 1, balance: 40, level: 3 } })
  assert.deepEqual(result.pets.map(p => [p.id, p.name, p.mood, p.balance]), [[1, '橘子', 20, 40], [2, '小黑', 90, 40]])
  assert.equal(result.account.id, 7)
})
test('餐位在 1280 世界左右边界内，所有猫脚点间隔至少 110px', () => {
  for (const x of [70, 600, 1190]) for (const y of [446.4, 600, 700]) {
    const positions = mealPositions(x, y, 2)
    assert.ok(positions.every(p => p.x >= 70 && p.x <= 1190 && p.y >= 446.4 && p.y <= 700))
    assert.ok(positions[1].x - positions[0].x >= 110)
  }
})
test('共享盘两边原吃帧嘴中心精确落在食物接触点，不缩猫、不悬空、手机包围盒小于290', () => {
  const mouth = mealMouthLocal()
  for (const x of [70, 720, 1190]) for (const y of [446.4, 600, 700]) {
    const g = mealGeometry(x, y)
    assert.equal(g.width, 104); assert.ok(g.bboxWidth <= 290)
    assert.ok(g.contacts[0].y < g.floorY && g.floorY - g.contacts[0].y < 22)
    g.seats.forEach((seat, i) => {
      const world = { x: seat.x - 55 + (seat.flipped ? 110 - mouth.x : mouth.x), y: seat.y - 100 + mouth.y }
      assert.ok(Math.hypot(world.x - g.contacts[i].x, world.y - g.contacts[i].y) < .000001)
      assert.equal(seat.flipped, i === 0)
      assert.deepEqual(actorSeat(g, i), { ...seat, seat: i })
    })
    const waits = mealWaitingPositions(g, 2)
    assert.ok(waits.every(p => p.y <= 700 && p.y >= 446.4))
    assert.ok(waits.every(p => g.seats.every(s => Math.hypot(s.x - p.x, s.y - p.y) >= 110)))
  }
})
function verifyFloorMoves(plan, positions) {
  assert.ok(plan, '餐位计划必须可达')
  for (const move of plan.moves) {
    const actor = positions.find(p => p.id === move.id)
    for (const to of move.route) {
      const from = { ...actor }, dx = to.x - from.x, dy = to.y - from.y, length2 = dx * dx + dy * dy
      for (const other of positions.filter(p => p !== actor)) {
        const t = length2 ? Math.max(0, Math.min(1, ((other.x - from.x) * dx + (other.y - from.y) * dy) / length2)) : 0
        const distance = Math.hypot(from.x + dx * t - other.x, from.y + dy * t - other.y)
        assert.ok(distance >= 109.99, `移动${move.id}到${JSON.stringify(to)}与${other.id}最近${distance}`)
      }
      for (let step = 0; step <= 40; step++) {
        const point = { x: from.x + dx * step / 40, y: from.y + dy * step / 40 }
        assert.ok(point.x >= 70 && point.x <= 1190 && point.y >= 446.4 && point.y <= 700)
      }
      Object.assign(actor, { x: to.x, y: to.y })
    }
  }
}
const permutations = values => values.length ? values.flatMap(value => permutations(values.filter(v => v !== value)).map(rest => [value, ...rest])) : [[]]
test('反序餐位按实际左右分配，连续路线不会在x720交换穿过', () => {
  const positions = [{ id: '1', x: 900, y: 590 }, { id: '2', x: 540, y: 590 }]
  const plan = planMealArrival(mealGeometry(720, 590), positions)
  assert.deepEqual(plan.seats, [{ id: '1', seat: 1 }, { id: '2', seat: 0 }])
  verifyFloorMoves(plan, positions)
  assert.ok(positions[0].x > positions[1].x)
})
for (const count of [3, 4]) {
  test(`${count}猫所有身份排序从入场到候位轮换，连续线段不交叉且只复用两口位`, () => {
    const g = mealGeometry(720, 600)
    for (const xs of permutations([540, 720, 900, 1080].slice(0, count))) {
      const positions = xs.map((x, i) => ({ id: String(i), x, y: 590 }))
      verifyFloorMoves(planMealArrival(g, positions), positions)
      const previous = positions.slice(0, 2), next = positions.slice(2)
      const exits = g.seats.map(p => ({ x: p.x, y: g.center.y - 110 }))
      verifyFloorMoves(planActorSeats(previous, exits, [], next), positions)
      const seated = planActorSeats(next, g.seats.slice(0, next.length), [], previous)
      verifyFloorMoves(seated, positions)
      assert.ok(next.every(p => g.seats.some(s => Math.hypot(p.x - s.x, p.y - s.y) < .001)))
    }
  })
}
test('同列餐猫先移开占位猫，再绕行入场，所有路段保持脚点间距', () => {
  const positions = [{ id: '1', x: 720, y: 480 }, { id: '2', x: 720, y: 620 }]
  const plan = planMealArrival(mealGeometry(720, 590), positions)
  assert.equal(plan.moves[0].id, '2'); verifyFloorMoves(plan, positions)
})
test('餐盘点击家具位置时就近避开实体与安全间隙，不能把口位放到障碍里', () => {
  const obstacles = [{ left: 452, right: 548, top: 568, bottom: 608 }]
  const positions = [{ id: '1', x: 552, y: 600 }, { id: '2', x: 900, y: 600 }]
  const placement = mealPlacement(500, 590, positions, obstacles)
  assert.ok(placement); assert.notEqual(placement.geometry.center.x, 500)
  assert.ok(placement.geometry.seats.every(p => p.x > 603))
  const g = placement.geometry
  assert.ok(g.center.x - 52 > obstacles[0].right)
  verifyFloorMoves(placement.arrival, positions)
  assert.equal(mealPlacement(800, 590, positions, [{ left: 0, right: 1280, top: 400, bottom: 720 }]), null)
})
test('3/4猫只产生2餐位动画批次，批次不创建新的喂食事务', async () => {
  assert.deepEqual(mealBatches([1, 2, 3]), [[1, 2], [3]])
  assert.deepEqual(mealBatches([1, 2, 3, 4]), [[1, 2], [3, 4]])
  let requests = 0
  const client = createMealTransaction({ storage: storage(), key: () => 'batch', makeID: () => 'one-id', post: async () => {
    requests++; return { ok: true, status: 200, data: { pet: { id: 1, balance: 60, hunger: 60 }, fed: true, all_full: false } }
  } })
  await client.run(1)
  for (const batch of mealBatches([1, 2, 3, 4])) assert.ok(batch.length <= 2)
  assert.equal(requests, 1)
})
test('请求在途 doubleclick 复用 promise；未知结果在页面重建后复用持久化 ID', async () => {
  const store = storage(), bodies = []
  let reject
  const first = createMealTransaction({ storage: store, key: () => 'kid-7', makeID: () => 'stable-7', post: body => { bodies.push({ ...body }); return new Promise((_, r) => { reject = r }) } })
  const a = first.run(1), b = first.run(2)
  assert.equal(a, b); assert.equal(bodies.length, 1); assert.equal(first.state.phase, 'sending')
  reject(new Error('断网')); await assert.rejects(a)
  assert.equal(first.state.phase, 'unknown')
  const restored = createMealTransaction({ storage: store, key: () => 'kid-7', makeID: () => '不能生成新ID', post: async body => { bodies.push({ ...body }); return { ok: true, status: 200, data: { pet: { id: 1, balance: 60, hunger: 100 }, fed: true, all_full: false } } } })
  await restored.run(2)
  assert.deepEqual(bodies[1], { request_id: 'stable-7', pet_id: 1 })
  assert.equal(restored.state.phase, 'succeeded'); assert.equal(store.getItem('kid-7'), undefined)
})
test('全猫饱 no-op 与中文 400 均结束事务，不残留待重试订单', async () => {
  for (const result of [{ ok: true, status: 200, data: { pet: { id: 1, balance: 70, hunger: 100 }, fed: false, all_full: true } }, { ok: false, status: 400, data: { error: '星星不够啦' } }]) {
    const store = storage()
    const transaction = createMealTransaction({ storage: store, key: () => 'kid-7', makeID: () => 'meal-7', post: async () => result })
    assert.deepEqual(await transaction.run(1), result)
    assert.equal(transaction.state.phase, result.ok ? 'succeeded' : 'rejected')
    assert.equal(store.getItem('kid-7'), undefined)
  }
})

for (const result of [
  { ok: false, status: 500, data: { error: '开饭完成但快照失败' } },
  { ok: false, status: 502, data: {} },
  { ok: false, status: 503, data: { error: '服务暂时忙碌' } },
  { ok: false, status: 401, data: { error: '请重新登录' } },
  { ok: false, status: 408, data: { error: '请求超时' } },
  { ok: false, status: 429, data: { error: '请稍后重试' } },
  { ok: true, status: 200, data: { fed: true, all_full: false } },
  { ok: true, status: 200, data: { pet: {} } },
  { ok: true, status: 200, data: { pet: { balance: 60 } } },
  { ok: true, status: 200, data: { pet: { hunger: 50 } } },
  { ok: true, status: 200, data: { pet: { balance: 60, hunger: 50 }, error: '快照不完整' } },
  { ok: true, status: 200, data: { pet: { balance: 60, hunger: 50, mood: 999 } } },
  { ok: true, status: 200, data: { pet: { balance: 60, hunger: 50 }, fed: false, all_full: false } },
  { ok: true, status: 200, data: { pet: { id: 1, hunger: 50 }, account: { balance: 60 }, pets: [{ id: 1, hunger: 50 }] } },
]) {
  test(`${result.status} 不确定或无效结果保留 pending，相同 ID 重试才可结束`, async () => {
    const store = storage(), bodies = []
    let attempt = 0
    const client = createMealTransaction({ storage: store, key: () => 'kid-7', makeID: () => 'same-id', post: async body => {
      bodies.push({ ...body })
      return ++attempt === 1 ? result : { ok: true, status: 200, data: { pet: { id: 1, balance: 60, hunger: 100 }, fed: true, all_full: false } }
    } })
    await assert.rejects(client.run(1))
    assert.equal(client.state.phase, 'unknown')
    assert.equal(JSON.parse(store.getItem('kid-7')).request_id, 'same-id')
    await client.run(2)
    assert.deepEqual(bodies[1], bodies[0])
    assert.equal(client.state.phase, 'succeeded'); assert.equal(store.getItem('kid-7'), undefined)
  })
}

test('服务端已扣费但生成快照 500，重试通过原 ID 回放，不会扣第二次', async () => {
  const store = storage(), processed = new Set()
  let balance = 70, attempts = 0
  const client = createMealTransaction({ storage: store, key: () => 'kid-7', makeID: () => `id-${attempts}`, post: async body => {
    if (!processed.has(body.request_id)) { processed.add(body.request_id); balance -= 10 }
    if (++attempts === 1) return { ok: false, status: 500, data: { error: '快照生成失败' } }
    return { ok: true, status: 200, data: { pet: { id: 1, balance, hunger: 100 }, fed: true, all_full: false } }
  } })
  await assert.rejects(client.run(1)); await client.run(1)
  assert.equal(balance, 60); assert.equal(processed.size, 1)
})

test('事务存储键捕获原 kid，切换后迟到错误不能把 pending 写进新 kid', async () => {
  const store = storage()
  let kid = 7, reject
  const client = createMealTransaction({ storage: store, key: () => `kid-${kid}`, makeID: () => 'kid-a-id', post: () => new Promise((_, r) => { reject = r }) })
  const pending = client.run(1)
  kid = 8; reject(new Error('迟到断网')); await assert.rejects(pending)
  assert.equal(JSON.parse(store.getItem('kid-7')).request_id, 'kid-a-id')
  assert.equal(store.getItem('kid-8'), undefined)
})
