import test from 'node:test'
import assert from 'node:assert/strict'
import {
  LITTER_KEY, LITTER_MAX_STATE, LITTER_STATE_LABELS, LITTER_CLUMP_POSITIONS,
  LITTER_MOOD_TIERS, clampLitterState, litterClumpPositions, litterMoodTier,
  litterMiniMarkup, litterClumpMarkup, inLitterBin, LITTER_BIN_TARGET,
} from './litter-box.js'

test('商品 key 与档位上限保持约定', () => {
  assert.equal(LITTER_KEY, 'litter-box')
  assert.equal(LITTER_MAX_STATE, 3)
  assert.equal(LITTER_STATE_LABELS.length, 4)
  assert.equal(LITTER_CLUMP_POSITIONS.length, 3)
})

test('档位钳制：负数、小数、越界、非数字都落进 0-3', () => {
  const cases = [[-5, 0], [0, 0], [1.9, 1], [2, 2], [3, 3], [4, 3], [99, 3], [NaN, 0], [undefined, 0], ['2', 2]]
  for (const [input, want] of cases) {
    assert.equal(clampLitterState(input), want, `clampLitterState(${input})`)
  }
})

test('小图四档：结团数量逐档递增，气味线与亮晶晶只出现在对应档', () => {
  for (let n = 0; n <= 3; n++) {
    const svg = litterMiniMarkup(n)
    const clumps = svg.split('fill="#B98F63"').length - 1
    assert.equal(clumps, n, `第 ${n} 档应有 ${n} 个结团`)
    assert.match(svg, /^<svg viewBox="0 0 48 40"/)
    assert.equal(svg.includes('stroke="#C7B283"'), n === 3, `第 ${n} 档气味线`)
    assert.equal(svg.includes('fill="#FFFFFF"'), n === 0, `第 ${n} 档亮晶晶`)
  }
  // 越界输入也要落在满档，不能生成第 4 个结团
  assert.equal(litterMiniMarkup(9).split('fill="#B98F63"').length - 1, 3)
  assert.equal(litterMiniMarkup(-1).split('fill="#B98F63"').length - 1, 0)
})

test('覆层砂块数量等于小图档位，坐标来自固定预设点', () => {
  assert.deepEqual(litterClumpPositions(0), [])
  assert.deepEqual(litterClumpPositions(2), [
    { x: 152, y: 268 }, { x: 252, y: 292 },
  ])
  assert.equal(litterClumpPositions(3).length, 3)
  assert.equal(litterClumpPositions(9).length, 3)
  assert.match(litterClumpMarkup(), /^<svg viewBox="0 0 44 40"/)
})

test('心情分档只改变表现，不产生数值奖励或惩罚', () => {
  const cases = [[-1, 'low'], [0, 'low'], [39, 'low'], [40, 'ok'], [69, 'ok'], [70, 'good'], [100, 'good'], [NaN, 'low']]
  for (const [mood, want] of cases) {
    assert.equal(litterMoodTier(mood), want, `mood=${mood}`)
  }
  for (const tier of Object.values(LITTER_MOOD_TIERS)) {
    for (const key of ['anim', 'catX', 'catY', 'start', 'pick', 'drop', 'done', 'back']) {
      assert.ok(key in tier, `心情档缺少 ${key}`)
    }
    assert.equal(typeof tier.grayscale, 'boolean')
  }
  assert.equal(LITTER_MOOD_TIERS.low.grayscale, true)
  assert.equal(LITTER_MOOD_TIERS.good.grayscale, false)
  // 桶左沿 540、猫宽 110：任何档位都不能让猫压到桶上（窄屏等比缩放后同理）。
  for (const [name, tier] of Object.entries(LITTER_MOOD_TIERS)) {
    assert.ok(tier.catX + 110 <= 540 - 8, `${name} 档猫位压住了小桶`)
  }
})

test('倾倒判定：桶口范围内才算铲掉，边界包含', () => {
  assert.equal(inLitterBin(LITTER_BIN_TARGET), true)
  assert.equal(inLitterBin({ x: 534, y: 190 }), true)
  assert.equal(inLitterBin({ x: 656, y: 296 }), true)
  assert.equal(inLitterBin({ x: 533, y: 232 }), false)
  assert.equal(inLitterBin({ x: 590, y: 297 }), false)
  assert.equal(inLitterBin({ x: 300, y: 268 }), false)
})
