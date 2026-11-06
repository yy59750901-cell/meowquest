import test from 'node:test'
import assert from 'node:assert/strict'
import { existsSync, readFileSync, statSync } from 'node:fs'
import {
  NEST_CATALOG, NEST_KEYS, NEST_ENTRY_TIME, NEST_WALK_FRAME_MS, NEST_MOTION, NEST_PAD,
  CAT_W, CAT_H, WORLD_W, isJumpNest, nestProfile, nestArt, nestRoute, nestFlightPoint,
  nestLandingPoint, nestOpeningClip, nestHandoff, nestSleepPos, nestLocalFoot,
} from './nest-catalog.js'
import { DECOR_ART } from './scene-art.js'
import { getShopDisplay } from './shop-display.js'

const text = path => readFileSync(new URL(path, import.meta.url), 'utf8')
const home = text('./views/KidHome.vue')
const sprite = text('./components/CatSprite.vue')
const registry = text('../../backend/internal/api/api_wish.go')
const artPath = name => new URL(`../public/art/nests/${name}`, import.meta.url)
const png = name => {
  const p = artPath(name)
  assert.equal(existsSync(p), true, `缺素材 ${name}`)
  const buf = readFileSync(p)
  assert.equal(buf.subarray(1, 4).toString(), 'PNG', `${name} 不是 PNG`)
  return { width: buf.readUInt32BE(16), height: buf.readUInt32BE(20), bytes: buf.length }
}

test('15 款参数齐全，苹果窝不在新表内，key 与价格均已注册', () => {
  assert.equal(NEST_KEYS.length, 15)
  assert.equal(isJumpNest('nest-apple'), false) // 苹果窝维持上一代实现
  assert.equal(isJumpNest('nest-cloud'), false) // 上一轮删掉的 16 款没有复活
  const kinds = { open: 0, cave: 0, half: 0 }
  for (const key of NEST_KEYS) {
    const p = NEST_CATALOG[key]
    assert.equal(p.name.length > 0, true)
    assert.ok(p.price >= 25 && p.price <= 48, `${key} 价格越界`)
    assert.equal(p.display[0] > 170 && p.display[1] > 100, true)
    assert.equal(p.sprite[0] > 800 && p.sprite[1] > 500, true)
    assert.equal(p.catCenter.length, 2)
    assert.equal(DECOR_ART[key].size, p.display[0])
    assert.equal(getShopDisplay({ key }).icon, `/art/nests/${p.demoKey}-thumb.png`)
    assert.match(registry, new RegExp(`"key": "${key}"`))
    kinds[p.kind]++
    if (p.kind === 'open') {
      assert.equal(typeof p.rim, 'number', `${key} 开放垫缺前沿高度`)
      assert.equal(p.opening.length, 0)
    } else {
      assert.ok(p.opening.length >= 3, `${key} 洞口缺 opening 多边形`)
      assert.ok(p.opening.every(([x, y]) => x <= p.sprite[0] && y <= p.sprite[1]))
    }
  }
  // 全 16 款是 open6/cave8/half2，其中 cave 的 cut-apple 即线上苹果窝，新表排除后剩 7。
  assert.deepEqual(kinds, { open: 6, cave: 7, half: 2 })
})

test('每款 4 张压缩素材存在：back/front 保持原分辨率，阴影与缩略图轻量', () => {
  let total = 0
  for (const key of NEST_KEYS) {
    const p = NEST_CATALOG[key]
    for (const part of ['back', 'front']) {
      const f = png(`${p.demoKey}-${part}.png`)
      assert.deepEqual([f.width, f.height], p.sprite, `${key}-${part} 分辨率被改动`)
      assert.ok(f.bytes < 200 * 1024, `${key}-${part} 未压缩：${f.bytes}B`)
      total += f.bytes
    }
    const shadow = png(`${p.demoKey}-shadow.png`)
    assert.ok(shadow.bytes < 60 * 1024, `${key} 阴影过大`)
    const thumb = png(`${p.demoKey}-thumb.png`)
    assert.deepEqual([thumb.width, thumb.height], [64, 64])
  }
  assert.ok(total < 3 * 1024 * 1024, `15 款双片合计 ${total}B 仍过大`)
  assert.ok(statSync(artPath('tent-back.png')).size > 0)
})

test('demo 七阶段时序与动作帧常量完全对齐', () => {
  assert.deepEqual(NEST_ENTRY_TIME, { walk: 900, prepare: 150, openJump: 650, doorJump: 480, land: 220 })
  assert.equal(NEST_WALK_FRAME_MS, 180)
  assert.deepEqual(NEST_MOTION, { anticipate: 'crouchA', takeoff: 'pounceA', entering: 'pounceB', land: 'crouchA' })
  assert.equal(NEST_PAD, 8)
  assert.equal(CAT_W, 110)
  assert.equal(CAT_H, 83)
  // 复用游戏 CatSprite 已有帧；坐标仍是 demo 的 110 口径，等比缩小只在渲染层 transform 发生
  assert.equal(CAT_W, 110)
  assert.match(home, /const NEST_CAT_SCALE = 0\.8/)
  assert.equal(home.includes("const facing = (route.direction === 1 ? 'scaleX(-1) ' : '') + `scale(${NEST_CAT_SCALE})`"), true)
  assert.match(home, /\.nest-entry-cat \{ position: absolute; width: 110px;/)
  for (const frame of ['crouchA', 'pounceA', 'pounceB', 'walkA', 'walkB']) {
    assert.match(sprite, new RegExp(`\\b${frame}: \\[`))
  }
  assert.match(sprite, /poseFrame: \{ type: String, default: '' \}/)
  assert.match(sprite, /if \(props\.poseFrame && POSE_FRAMES\[props\.poseFrame\]\)/)
  assert.match(home, /:pose-frame="nestEntry\.frame" :size="110"/)
})

test('route：两类方向、门槛/前沿、起跳边界保护；抛物线真实先升后降', () => {
  const it = { x: 700, y: 620 }
  for (const key of NEST_KEYS) {
    const p = NEST_CATALOG[key], r = nestRoute(p, it)
    assert.equal(r.open, p.kind === 'open')
    assert.equal(r.direction, r.open ? 1 : -1)
    assert.equal(r.jumpTime, r.open ? NEST_ENTRY_TIME.openJump : NEST_ENTRY_TIME.doorJump)
    assert.ok(r.launch >= 6 && r.launch <= WORLD_W - 116)
    assert.ok(r.start >= 6 && r.start <= WORLD_W - 116)
    assert.ok(r.apexFoot < r.groundFoot, `${key} 起跳没有上升`)
    assert.ok(r.apexFoot < r.landFoot, `${key} 最高点不低于落点`)
    assert.ok(r.groundFoot <= r.h, `${key} 起跳脚点落到地面以下`)
    const at0 = nestFlightPoint(r, 0), at60 = nestFlightPoint(r, .6), at1 = nestFlightPoint(r, 1)
    assert.ok(Math.abs(at0.foot - r.groundFoot) < 1e-6)
    assert.ok(Math.abs(at60.foot - r.apexFoot) < 1e-6, `${key} 60% 不是最高点`)
    assert.ok(Math.abs(at1.foot - r.landFoot) < 1e-6)
    assert.ok(at60.top < at0.top, `${key} 最高点没有上移`)
    assert.ok(Math.abs(at1.left - r.landLeft) < 1e-6)
    const land0 = nestLandingPoint(r, 0), land1 = nestLandingPoint(r, 1)
    assert.ok(Math.abs(land0.foot - r.landFoot) < 1e-6)
    assert.ok(Math.abs(land1.foot - r.sleepFoot) < 1e-6)
    // 睡眠脚点与 demo catCenter 换算一致（猫底 + 17px）
    const pos = nestSleepPos(p, it), local = nestLocalFoot(p)
    assert.ok(Math.abs(pos.x - (it.x - p.display[0] / 2 + local.left + CAT_W / 2)) < 1e-6)
    assert.ok(Math.abs(pos.y - (it.y - p.display[1] + local.top + CAT_H + 17)) < 1e-6)
  }
  // 贴世界左边界也不崩
  const edge = nestRoute(NEST_CATALOG['nest-tent'], { x: 60, y: 620 })
  assert.ok(edge.launch >= 6 && edge.start >= 6)
})

test('opening 多边形裁切与内外双副本补集 clip 与 demo 一致', () => {
  const it = { x: 700, y: 620 }
  for (const key of NEST_KEYS) {
    const p = NEST_CATALOG[key]
    const clip = nestOpeningClip(p)
    if (p.kind === 'open') {
      assert.equal(clip, 'none')
    } else {
      assert.match(clip, /^polygon\(/)
      assert.equal((clip.match(/%/g) || []).length, p.opening.length * 2)
    }
    const r = nestRoute(p, it)
    const early = nestHandoff(r, 0), mid = nestHandoff(r, .7), late = nestHandoff(r, 1)
    if (r.open) {
      assert.equal(early.outerHidden, false)
      assert.equal(early.innerHidden, true)
      assert.equal(mid.outerHidden, true) // .64 越沿后整只交接
      assert.equal(mid.innerHidden, false)
      assert.equal(mid.outerClip, 'none')
    } else {
      assert.equal(early.outerClip, 'inset(0 0 0 0px)')
      assert.equal(early.innerHidden, true)
      assert.equal(mid.outerHidden, false) // 头先进入，内外同时可见
      assert.equal(mid.innerHidden, false)
      assert.equal(late.outerHidden, true)
      assert.equal(late.innerHidden, false)
      const out = Number(mid.outerClip.match(/([\d.]+)px/)[1])
      const inside = Number(mid.innerClip.match(/([\d.]+)px/)[1])
      assert.ok(Math.abs(out + inside - CAT_W) < 1e-6, `${key} 补集 clip 不互补`)
    }
  }
})

test('房间模板：每款专属 PNG 阴影、前片、双副本动画层与三层 Z 序', () => {
  assert.match(home, /class="nest-shadow-img"/)
  assert.match(home, /top: it\.y \+ NEST_PAD \+ 'px', zIndex: 9 \+ Math\.floor\(it\.y\)/)
  assert.match(home, /width: nestProfile\(it\.item_key\)\.display\[0\] \+ 2 \* NEST_PAD \+ 'px'/)
  assert.match(home, /height: nestProfile\(it\.item_key\)\.display\[1\] \+ 2 \* NEST_PAD \+ 'px'/)
  assert.match(home, /\.nest-shadow-img \{[^}]*background-size: 100% 100%;/)
  assert.match(home, /nestArt\(nestProfile\(it\.item_key\), 'back'\)/)
  assert.match(home, /nestArt\(nestProfile\(it\.item_key\), 'front'\)/)
  assert.match(home, /zIndex: 12 \+ Math\.floor\(it\.y\)/)
  assert.match(home, /clipPath: nestEntry\.planeClip, zIndex: 11 \+ Math\.floor\(nestEntry\.it\.y\)/)
  assert.match(home, /zIndex: 13 \+ Math\.floor\(nestEntry\.it\.y\)/)
  assert.match(home, /visibility: nestEntry\.value \? 'hidden' : 'visible'/)
  // 阴影 9+y < back 10+y < 窝内猫 11+y < front 12+y < walker 13+y
  assert.match(home, /: 10 \+ Math\.floor\(it\.y\)/)
  assert.match(home, /if \(nestActive\.value\) return 11 \+ Math\.floor\(nestActive\.value\.y\)/)
})

test('七阶段渲染、抢占与卸载清理都在，苹果窝旧链路未被改写', () => {
  for (const phase of ['walking', 'anticipating', 'takeoff', 'airborne', 'entering', 'landing']) {
    assert.ok(home.includes(`'${phase}'`), `缺阶段 ${phase}`)
  }
  assert.match(home, /function settleNest\(p, it, token\)[\s\S]*?catAnim\.value = 'sleep'[\s\S]*?say\('呼…呼…'\)/)
  // 关掉 walkTo 的过渡，否则趴睡时猫还在从起跳点滑向软垫、挂在窝外
  assert.match(home, /function settleNest\(p, it, token\)[\s\S]*?catEl\.value\.style\.transition = 'none'[\s\S]*?const pos = nestSleepPos\(p, it\)/)
  assert.match(home, /nestRAF = requestAnimationFrame\(step\)/)
  assert.match(home, /function stopNestEntry\(\) \{[\s\S]*?cancelAnimationFrame\(nestRAF\)/)
  assert.match(home, /function cancelJumpNest\(\)[\s\S]*?stopNestEntry\(\)[\s\S]*?clearTimeout\(nestEntryTimer\)/)
  assert.match(home, /function cancelNest\(opts = \{\}\)[\s\S]*?const sleepingIn = nestActive\.value[\s\S]*?cancelJumpNest\(\)/)
  // 出窝链路不能改写七阶段进场：dwell 到期统一走 cancelNest 播出窝动画
  assert.match(home, /cancelNest\(\{ onDone: \(\) => behaviorNext\(\) \}\)/)
  assert.match(home, /onBeforeUnmount\(\(\) => \{[\s\S]*?cancelNest\(\)/)
  assert.match(home, /if \(isJumpNest\(it\.item_key\)\) return jumpIntoNest\(it\)/)
  assert.match(home, /function enterNest\(it\) \{[\s\S]*?const pad = appleNestFoot\(it\)/)
  assert.match(home, /\{\s*speed: walkDist \/ 1\.35, keepCamera: true\s*\}/)
  assert.match(home, /class="nest-shade"/)
  assert.match(home, /src="\/art\/nest-apple\.png"/)
  assert.match(home, /src="\/art\/nest-apple-front\.png"/)
})
