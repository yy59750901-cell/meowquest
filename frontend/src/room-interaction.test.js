import test from 'node:test'
import assert from 'node:assert/strict'
import {
  buildAdjacency, shortestNodePath, planInteractionSession, pickInteractionNode,
  dwellForNode, ROOM_INTERACTION_PROFILES, ROOM_INTERACTION_NODES,
} from './room-interaction.js'

const TREE_EDGES = [
  ['ground', 'grip'], ['grip', 'mid'], ['grip', 'nest'], ['mid', 'top'],
]

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
