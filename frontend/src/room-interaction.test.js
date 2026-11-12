import test from 'node:test'
import assert from 'node:assert/strict'
import {
  buildAdjacency, shortestNodePath, planInteractionSession, pickInteractionNode,
  dwellForNode, ROOM_INTERACTION_PROFILES, ROOM_INTERACTION_NODES,
  socialActorEligible, socialPair, socialRoute, socialFormationRoute, socialStage, socialSegmentBlocked, yarnPawContact, socialObstacles, actorFloorRoute, planChaseEntry, floorFurnitureSnapshot,
} from './room-interaction.js'

const TREE_EDGES = [
  ['ground', 'grip'], ['grip', 'mid'], ['grip', 'nest'], ['mid', 'top'],
]

test('社交候选只接受有力idle猫，睡眠/走动/聊天/占用/已参与均排除', () => {
  const idle = { hunger: 30, anim: 'sit' }
  assert.equal(socialActorEligible(idle), true)
  for (const patch of [{ hunger: 29.9 }, { anim: 'sleep' }, { anim: 'eat' }, { walking: true }, { chat: true }, { occupied: true }, { social: true }]) assert.equal(socialActorEligible({ ...idle, ...patch }), false)
})
test('毛球保留上轮接球猫作为拍球猫，更多候选按参与次数轮换', () => {
  const ids = ['1', '2', '3', '4'], counts = new Map([['1', 1], ['2', 1]])
  assert.deepEqual(socialPair(ids, '2', counts), ['2', '3'])
  counts.set('2', 2); counts.set('3', 1)
  assert.deepEqual(socialPair(ids, '3', counts), ['3', '4'])
  assert.deepEqual(socialPair(['1']), [])
  assert.deepEqual(socialPair(['1', '2'], '2'), ['2', '1'])
})
test('社交路线连续避开世界障碍，双猫平移始终128px且足点在地板内', () => {
  const rect = { left: 550, right: 650, top: 560, bottom: 600 }
  const from = { x: 340, y: 580 }, to = { x: 850, y: 580 }
  const route = socialFormationRoute(from, to, 64, [rect])
  assert.ok(route?.length > 1)
  let previous = from
  for (const point of route) {
    const expanded = { left: rect.left - 119, right: rect.right + 119, top: rect.top - 18, bottom: rect.bottom + 18 }
    assert.equal(socialSegmentBlocked(previous, point, expanded), false)
    for (let step = 0; step <= 20; step++) {
      const center = { x: previous.x + (point.x - previous.x) * step / 20, y: previous.y + (point.y - previous.y) * step / 20 }
      const cats = [-64, 64].map(offset => ({ x: center.x + offset, y: center.y }))
      assert.equal(cats[1].x - cats[0].x, 128)
      assert.ok(cats.every(p => p.x >= 70 && p.x <= 1190 && p.y >= 446.4 && p.y <= 700))
    }
    previous = point
  }
  assert.equal(socialRoute({ x: 70, y: 440 }, to, []), null)
  const blocked = [{ left: 0, right: 1280, top: 400, bottom: 720 }]
  assert.equal(socialStage(from, 64, blocked), null)
})
test('正常盆栽互动脚点可从自身安全边缘短撤离，后续仍绕开完整家具', () => {
  const plant = { left: 452, right: 548, top: 568, bottom: 608 }
  const from = { x: 552, y: 600 }, to = { x: 735, y: 590 }
  const route = actorFloorRoute(from, to, [{ x: 900, y: 600 }], [plant])
  assert.ok(route); assert.deepEqual(route[0], { x: 604, y: 600 })
  assert.ok(Math.hypot(route[0].x - from.x, route[0].y - from.y) <= 56)
  let previous = from
  route.forEach((point, index) => {
    assert.equal(socialSegmentBlocked(previous, point, plant), false)
    if (index) assert.equal(socialSegmentBlocked(previous, point, { left: 397, right: 603, top: 550, bottom: 626 }), false)
    previous = point
  })
})
test('起点撤离不能穿家具实体、忽略其他家具或取消猫占位保护', () => {
  const plant = { left: 452, right: 548, top: 568, bottom: 608 }, from = { x: 552, y: 600 }, target = { x: 800, y: 590 }
  assert.equal(actorFloorRoute({ x: 500, y: 600 }, target, [], [plant]), null)
  assert.equal(actorFloorRoute(from, { x: 590, y: 590 }, [], [plant]), null)
  assert.equal(actorFloorRoute(from, target, [{ x: 620, y: 600 }], [plant]), null)
  const wall = { left: 580, right: 620, top: 446.4, bottom: 700 }
  assert.equal(actorFloorRoute(from, target, [], [plant, wall]), null)
})
test('同列追逐入场必须先移动下方占位猫，不排除active障碍骗取可达', () => {
  const actors = [{ id: '1', x: 720, y: 480 }, { id: '2', x: 720, y: 620 }]
  assert.equal(actorFloorRoute(actors[0], { x: 656, y: 620 }, [actors[1]]), null)
  const plan = planChaseEntry(actors, { x: 720, y: 620 })
  assert.ok(plan); assert.equal(plan.moves[0].id, '2')
  for (const move of plan.moves) {
    const actor = actors.find(a => a.id === move.id), other = actors.find(a => a.id !== move.id)
    for (const point of move.route) {
      const from = { ...actor }
      for (let step = 0; step <= 100; step++) {
        const x = from.x + (point.x - from.x) * step / 100, y = from.y + (point.y - from.y) * step / 100
        assert.ok(Math.hypot(x - other.x, y - other.y) >= 110)
      }
      Object.assign(actor, { x: point.x, y: point.y })
    }
  }
  assert.equal(Math.abs(actors[0].x - actors[1].x), 128)
})
test('毛球stage预留85餐边+10退步，左右边缘退步仍在70到1190', () => {
  for (const x of [70, 1190]) {
    const stage = socialStage({ x, y: 590 }, 95)
    assert.ok(stage.x - 85 - 10 >= 70); assert.ok(stage.x + 85 + 10 <= 1190)
  }
})
test('家具快照只跟踪地面障碍，不因球每帧移动或墙面刷新而变化', () => {
  const art = key => ({ surface: key === 'fish' ? 'wall' : 'floor' })
  const items = [{ id: 1, item_key: 'yarn', x: 720, y: 600 }, { id: 2, item_key: 'plant', x: 500, y: 600 }, { id: 3, item_key: 'fish', x: 600, y: 300 }]
  const snapshot = floorFurnitureSnapshot(items, art)
  items[0].x++; items[2].x++
  assert.equal(floorFurnitureSnapshot(items, art), snapshot)
  items[1].x++; assert.notEqual(floorFurnitureSnapshot(items, art), snapshot)
  assert.notEqual(floorFurnitureSnapshot(items.filter(it => it.id !== 2), art), snapshot)
})
test('catchB真实爪像素触球侧下方，球底仍在脚底附近，不悬空', () => {
  for (const facing of [false, true]) {
    const result = yarnPawContact({ x: 720, y: 600 }, facing)
    assert.equal(Math.abs(result.paw.x - result.ball.x), 16)
    const center = { x: result.ball.x, y: result.ball.y - 21 }
    assert.ok(Math.abs(Math.hypot(center.x - result.paw.x, center.y - result.paw.y) - 18) < .2)
    assert.ok(Math.abs((result.ball.y - 3) - (600 - 17)) < 2)
  }
  assert.deepEqual(socialObstacles([{ id: 1, item_key: 'yarn' }, { id: 2, item_key: 'tree', x: 400, y: 600 }], () => ({ surface: 'floor', size: 192 }), 1), [{ left: 304, right: 496, top: 556, bottom: 608 }])
})
test('无向图邻接表双向可达且不重复', () => {
  const adj = buildAdjacency([...TREE_EDGES, ['grip', 'mid']])
  assert.deepEqual(adj.get('ground'), ['grip'])
  assert.deepEqual(adj.get('grip').sort(), ['ground', 'mid', 'nest'])
  assert.deepEqual(adj.get('top'), ['mid'])
})

test('最短路径从任意当前节点出发，不再绕回地面', () => {
  assert.deepEqual(shortestNodePath(TREE_EDGES, 'ground', 'top'), ['grip', 'mid', 'top'])
  assert.deepEqual(shortestNodePath(TREE_EDGES, 'mid', 'top'), ['top'])
  assert.deepEqual(shortestNodePath(TREE_EDGES, 'grip', 'top'), ['mid', 'top'])
  assert.deepEqual(shortestNodePath(TREE_EDGES, 'nest', 'top'), ['grip', 'mid', 'top'])
  assert.deepEqual(shortestNodePath(TREE_EDGES, 'top', 'nest'), ['mid', 'grip', 'nest'])
  assert.deepEqual(shortestNodePath(TREE_EDGES, 'nest', 'mid'), ['grip', 'mid'])
  assert.deepEqual(shortestNodePath(TREE_EDGES, 'top', 'mid'), ['mid'])
})

test('同点返回空数组（原地重播），不可达或非法输入返回 null', () => {
  assert.deepEqual(shortestNodePath(TREE_EDGES, 'mid', 'mid'), [])
  assert.equal(shortestNodePath(TREE_EDGES, '', 'top'), null)
  assert.equal(shortestNodePath(TREE_EDGES, 'mid', ''), null)
  assert.equal(shortestNodePath(TREE_EDGES, 'mid', 'unknown'), null)
  assert.equal(shortestNodePath([['a', 'b']], 'a', 'z'), null)
  assert.equal(shortestNodePath([['a', 'b'], ['c', 'd']], 'a', 'd'), null)
})

test('会话计划的次数与时长都落在配置边界内，随机可注入', () => {
  for (const key of Object.keys(ROOM_INTERACTION_PROFILES)) {
    const p = ROOM_INTERACTION_PROFILES[key]
    for (const r of [0, 0.3, 0.5, 0.9, 0.999999, 1]) {
      const plan = planInteractionSession(key, () => r)
      assert.ok(plan.repeats >= p.repeats[0] && plan.repeats <= p.repeats[1], key)
      assert.ok(plan.dwell >= p.dwell[0] && plan.dwell <= p.dwell[1], key)
      assert.ok(plan.longDwell >= p.longDwell[0] && plan.longDwell <= p.longDwell[1], key)
      assert.equal(plan.maxDuration, 25000)
    }
  }
  assert.equal(planInteractionSession('unknown', () => 0.5), null)
  const low = planInteractionSession('cat-tree', () => 0)
  assert.equal(low.repeats, 2)
  assert.equal(low.dwell, 2500)
  assert.equal(low.longDwell, 6000)
  const high = planInteractionSession('cat-tree', () => 0.999999)
  assert.equal(high.repeats, 4)
  assert.equal(high.dwell, 5000)
  assert.equal(high.longDwell, 12000)
})

test('睡觉是一次长停留而不是连续多次互动', () => {
  const plan = planInteractionSession('nest', () => 0.5)
  assert.equal(plan.repeats, 1)
  assert.ok(plan.dwell >= 6000 && plan.dwell <= 12000)
  assert.equal(dwellForNode(plan, 'nest'), plan.longDwell)
  assert.equal(dwellForNode(planInteractionSession('cat-tree', () => 0.5), 'top'),
    planInteractionSession('cat-tree', () => 0.5).dwell)
})

test('自动连玩只挑不同节点，单节点道具没有下一个目标', () => {
  assert.equal(pickInteractionNode('nest', 'nest', () => 0.5), null)
  assert.equal(pickInteractionNode('yarn', 'yarn', () => 0), null)
  assert.equal(pickInteractionNode('unknown', 'nest', () => 0), null)
  const seen = new Set()
  for (const r of [0, 0.25, 0.5, 0.75, 0.999999]) {
    const next = pickInteractionNode('cat-tree', 'top', () => r)
    assert.ok(ROOM_INTERACTION_NODES['cat-tree'].includes(next))
    assert.notEqual(next, 'top')
    seen.add(next)
  }
  assert.ok(seen.size >= 2, '随机应能覆盖多个不同目标')
})
