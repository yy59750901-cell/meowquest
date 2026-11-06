import test from 'node:test'
import assert from 'node:assert/strict'
import { existsSync, readFileSync, statSync } from 'node:fs'
import { DECOR_ART, decorArt } from './scene-art.js'
import { getShopDisplay } from './shop-display.js'
import { NEST_KEYS, NEST_CATALOG } from './nest-catalog.js'

const text = path => readFileSync(new URL(path, import.meta.url), 'utf8')
const home = text('./views/KidHome.vue')
const room = text('./components/PixelRoom.vue')
const shop = text('./components/PixelShop.vue')
const registry = text('../../backend/internal/api/api_wish.go')
const readPng = name => {
  const file = new URL(`../public/art/${name}`, import.meta.url)
  const buf = readFileSync(file)
  assert.equal(buf.subarray(1, 4).toString(), 'PNG', `${name} 不是 PNG`)
  return { width: buf.readUInt32BE(16), height: buf.readUInt32BE(20), buf }
}

test('A 档房间：窗户落在真实相机可见区、地板纹理 2x，不贴视口上沿', () => {
  assert.match(room, /top: 165px;/)
  assert.doesNotMatch(room, /top: 98px;/)
  assert.match(room, /background-size: 640px 160px;/)
  assert.match(room, /left: 535px;/)
  assert.match(room, /width: 210px;/)
  assert.match(room, /height: 196px;/)
})

test('底部工具栏容器不吃点击，.actions 按钮整块可点', () => {
  // 8ad80f1 把 .room-tools 改成全宽透明带（z=18）后横压住 z=17 的 .actions 按钮中段，
  // 只剩顶部 6px / 底部 8px 能点。容器只做布局，命中必须交回内部真按钮。
  assert.match(home, /\.room-tools \{[^}]*z-index: 18;[^}]*pointer-events: none; \}/)
  assert.match(home, /\.room-tools button \{ pointer-events: auto; \}/)
  assert.match(home, /\.actions \{[^}]*z-index: 17; \}/)
  assert.doesNotMatch(home, /\.room-tools button \{ pointer-events: none; \}/)
})

test('商店含老窝、切面苹果窝与 15 款新窝，前后端 key/名称/价格一致', () => {
  const keys = [...registry.matchAll(/\{"key": "(nest[^"]*)"/g)].map(m => m[1])
  assert.equal(keys[0], 'nest')
  assert.equal(keys[1], 'nest-apple')
  assert.deepEqual(keys.slice(2), NEST_KEYS)
  assert.match(registry, /\{"key": "nest-apple", "name": "切面苹果窝", "price": 40, "type": "furniture"\}/)
  for (const key of NEST_KEYS) {
    const p = NEST_CATALOG[key]
    assert.match(registry, new RegExp(`\\{"key": "${key}", "name": "${p.name}", "price": ${p.price}, "type": "furniture"\\}`))
    assert.equal(DECOR_ART[key].size, p.display[0])
  }
  assert.equal(DECOR_ART['nest-apple'].size, 192)
  assert.equal(decorArt('nest-apple').name, '切面苹果窝')
  assert.equal(decorArt('nest-apple').surface, 'floor')
})

test('商店卡片用 PNG 图标，描述不再提随机露出姿态', () => {
  const display = getShopDisplay({ key: 'nest-apple', price: 40, type: 'furniture' })
  assert.equal(display.icon, '/art/nest-apple-thumb.png')
  assert.equal(display.category, 'furniture')
  assert.equal(display.name, '切面苹果窝')
  assert.doesNotMatch(display.description, /随机|露屁股|尾巴/)
  assert.match(shop, /class="shop-item-image" :src="display\.icon"/)
})

test('确认稿双片素材存在，尺寸一致且不是同一张图', () => {
  const back = readPng('nest-apple.png')
  const front = readPng('nest-apple-front.png')
  assert.deepEqual([back.width, back.height], [939, 783])
  assert.deepEqual([front.width, front.height], [939, 783])
  assert.notDeepEqual(back.buf, front.buf)
})

// 运行时只用压缩图：苹果窝双片 256 色量化、商店走 64px 缩略图、15 款保持压缩版，
// 美术原稿仍留在 meowquest-cat-nest-design/assets/ 与 gallery assets，不参与运行时。
test('所有猫窝运行时素材都是压缩图，商店卡片不拉全尺寸大图', () => {
  const bytes = name => statSync(new URL(`../public/art/${name}`, import.meta.url)).size
  assert.ok(bytes('nest-apple.png') < 80 * 1024, '苹果窝 back 不应回到 600KB 原图')
  assert.ok(bytes('nest-apple-front.png') < 80 * 1024, '苹果窝 front 不应回到 600KB 原图')
  const thumb = readPng('nest-apple-thumb.png')
  assert.deepEqual([thumb.width, thumb.height], [64, 64])
  assert.ok(thumb.buf.length < 12 * 1024)
  for (const key of NEST_KEYS) {
    const p = NEST_CATALOG[key]
    assert.ok(bytes(`nests/${p.demoKey}-back.png`) < 120 * 1024, `${key} back 过大`)
    assert.ok(bytes(`nests/${p.demoKey}-front.png`) < 120 * 1024, `${key} front 过大`)
  }
})

test('房间按 192px 宽渲染双片，三层 Z 序严格递增', () => {
  assert.match(home, /class="nest-img" src="\/art\/nest-apple\.png"/)
  assert.match(home, /class="nest-img" src="\/art\/nest-apple-front\.png"/)
  assert.match(home, /const appleNests = computed\(\(\) => placedDecor\.value\.filter\(it => it\.item_key === APPLE_NEST_KEY\)\)/)
  assert.match(home, /: 10 \+ Math\.floor\(it\.y\)/)
  assert.match(home, /if \(nestActive\.value\) return 11 \+ Math\.floor\(nestActive\.value\.y\)/)
  assert.match(home, /zIndex: 12 \+ Math\.floor\(it\.y\)/)
  assert.match(home, /:style="\{ width: decorArt\(it\.item_key\)\.size \+ 'px' \}/)
})

test('进窝只有固定趴睡；地面猫 110px、进窝猫按 0.8 等比缩小且不裁切、不翻身', () => {
  assert.match(home, /const treeCatSize = computed\(\(\) => treeRendered\.value \? TREE_CAT_SIZE : 110\)/)
  assert.match(home, /const NEST_CAT_SCALE = 0\.8/)
  assert.match(home, /const nestCatActive = computed\(\(\) => Boolean\(nestWalking\.value \|\| nestActive\.value \|\| nestEntry\.value \|\| nestExiting\.value\)\)/)
  assert.match(home, /transform: nestCatActive\.value && !treeRendered\.value \?/)
  assert.match(home, /catAnim\.value = 'sleep'/)
  assert.match(home, /const scale = APPLE_NEST_WIDTH \/ 939/)
  // 苹果窝自身不裁切、不翻身；新 15 款的 clipPath 属于另一条链路，不在此检查。
  assert.doesNotMatch(home, /flipChance|pickReveal|nestWiggle|laterNest|nestClip/)
  assert.doesNotMatch(home, /nest-profiles|nest-sprites/)
})

test('按 demo 三段进场：地板线、左侧 170px 起走、1.35s 匀速，走路时猫在最上层', () => {
  assert.match(home, /const groundY = it\.y \+ 17/)
  assert.match(home, /const approachX = Math\.max\(55, pad\.x - 170\)/)
  assert.match(home, /const walkDist = Math\.max\(1, pad\.x - approachX\)/)
  assert.match(home, /\{ speed: walkDist \/ 1\.35, keepCamera: true \}/)
  // 只有进场三段固定镜头（苹果窝两段 + 新 15 款走到 demo start 一段）；出窝必须跟随相机。
  assert.equal((home.match(/keepCamera: true/g) || []).length, 3)
  assert.match(home, /walkTo\(route\.groundPos,[\s\S]*?keepCamera: true/)
  assert.match(home, /camMode\.value === 'follow' && !opts\.keepCamera/)
  assert.match(home, /nestWalking\.value = it/)
  assert.match(home, /if \(nestWalking\.value\) return 13 \+ Math\.floor\(nestWalking\.value\.y\)/)
  assert.match(home, /catEl\.value\.style\.transition = 'none'/)
  assert.match(home, /nestWalking\.value = null[\s\S]*?nestExiting\.value = null[\s\S]*?nestActive\.value = it/)
})

test('按 demo 补上窝底椭圆投影，且压在窝体下层不遮窝', () => {
  assert.match(home, /class="nest-shade"/)
  assert.match(home, /v-for="it in appleNests" :key="'nest-shade-' \+ it.id"/)
  assert.match(home, /top: it\.y \+ 5 \+ 'px', zIndex: 9 \+ Math\.floor\(it\.y\)/)
  assert.match(home, /\.nest-shade \{[^}]*width: 176px; height: 10\.4px;/)
  // demo 纯色地板用 #886747/.28；游戏木纹地板需加深柔化才等效贴地，几何与层级不变
  assert.match(home, /\.nest-shade \{[^}]*background: #5b3d22; opacity: \.45; filter: blur\(2px\);/)
  // 投影层 9+y < 窝 back 10+y < 窝内猫 11+y < 前片 12+y < 走路猫 13+y
  assert.match(home, /: 10 \+ Math\.floor\(it\.y\)/)
  assert.match(home, /zIndex: 12 \+ Math\.floor\(it\.y\)/)
})

test('猫精灵自带落地投影，与 demo 的 cat drop-shadow 一致', () => {
  assert.match(text('./components/CatSprite.vue'), /filter: drop-shadow\(2px 2px 0 rgba\(43,43,43,\.[0-9]+\)\)/)
})

test('窝里与出窝说话气泡都在独立最高层，不压窝也不被窝盖住', () => {
  assert.match(home, /const APPLE_NEST_HEIGHT = APPLE_NEST_WIDTH \* 783 \/ 939/)
  assert.match(home, /class="bubble" v-if="showBubble && !nestActive && !nestExiting"/)
  assert.match(home, /v-if="showBubble && \(nestActive \|\| nestExiting\)" class="bubble nest-bubble"/)
  // 苹果窝高度仍为 200.13，新窝按各自 display 高度。
  assert.match(home, /if \(it\.item_key === APPLE_NEST_KEY\) return APPLE_NEST_HEIGHT/)
  assert.match(home, /const topBase = it\.y - nestBubbleHeight\(it\) - 10/)
  assert.match(home, /const zIndex = 13 \+ Math\.floor\(it\.y\)/)
  assert.match(home, /\.nest-bubble \{ bottom: auto; transform: translate\(-50%, -100%\)/)
  assert.match(home, /\.nest-bubble-follow \{ transition: left \.8s linear, top \.8s linear; \}/)
})

test('出窝会话：趴睡退出保持最高层、沿地板走出洞口，并串接自主行为', () => {
  assert.match(home, /const nestExiting = ref\(null\)/)
  assert.match(home, /if \(nestExiting\.value\) return 13 \+ Math\.floor\(nestExiting\.value\.y\)/)
  assert.match(home, /function beginNestExit\(it, animate = true, onDone\)/)
  assert.match(home, /const groundY = it\.y \+ 17/)
  assert.match(home, /\{ speed: dist \/ 0\.8 \}/)
  // 出窝不能固定镜头：窄屏相机只裁出窝周围一小块世界，固定镜头会让猫走出屏幕
  assert.doesNotMatch(home, /\{ speed: dist \/ 0\.8, keepCamera: true \}/)
  assert.match(home, /出窝相反，要让 follow 相机跟着猫/)
  // 两条 dwell 到期都必须把 behaviorNext 串在出窝完成回调里，避免共用 stateTimer 被抢占
  assert.equal((home.match(/cancelNest\(\{ onDone: \(\) => behaviorNext\(\) \}\)/g) || []).length, 2)
  assert.match(home, /cancelNest\(\{ animate: false \}\)/)
  assert.match(home, /:class="\{ 'nest-bubble-follow': nestExiting \}" :style="nestBubbleStyle"/)
  assert.match(home, /nestExiting\.value = null\s*\n\s*nestActive\.value = it/)
})

test('抢占与卸载都还原层级，自主睡觉仍可挑新旧两种窝', () => {
  assert.match(home, /function cancelNest\(opts = \{\}\)[\s\S]*?nestActive\.value = null[\s\S]*?clearTimeout\(nestTimer\)/)
  assert.match(home, /onBeforeUnmount\(\(\) => \{[\s\S]*?cancelNest\(\)/)
  assert.match(home, /it\.item_key === 'nest' \|\| it\.item_key === APPLE_NEST_KEY/)
  assert.match(home, /\['plant', 'nest', 'yarn', APPLE_NEST_KEY\]\.includes/)
  assert.match(home, /else if \(target && isJumpNest\(target\.item_key\)\) \{\s*jumpIntoNest\(target\)/)
})

test('物品栏全平台按需展开，且用商店真实缩略图而不是礼物 fallback', () => {
  assert.match(home, /import \{ getShopDisplay \} from '\.\.\/shop-display\.js'/)
  assert.match(home, /<div v-if="inventory\.length && invOpen" class="inv-bar">/)
  assert.doesNotMatch(home, /inventory\.length && \(!isNarrow/)
  assert.equal((home.match(/invOpen\.value = true/g) || []).length >= 2, true)
  assert.doesNotMatch(home, /if \(isNarrow\.value\) \{ sheet\.value = ''; invOpen\.value = true/)
  assert.match(home, /if \(!drag\.id\) invOpen\.value = false/)
  assert.match(home, /class="inv-item-image"/)
  assert.match(home, /getShopDisplay\(it\.key\)\.icon/)
  assert.match(home, /\.inv-item-image \{ display: block; width: 40px; height: 40px;/)
  // 已购未摆放的猫窝都有 64px 缩略图，物品栏不会再退化成礼物盒
  assert.equal(getShopDisplay('nest-apple').icon, '/art/nest-apple-thumb.png')
  for (const key of NEST_KEYS) {
    assert.equal(String(getShopDisplay(key).icon).startsWith('/art/nests/'), true, `${key} 缺缩略图`)
  }
})

test('16 款配套文件已删除，宽屏布局与 invOpen 修复未误删', () => {
  for (const name of ['nest-profiles.js', 'nest-sprites.js', 'nest-profiles.test.js']) {
    assert.equal(existsSync(new URL(`./${name}`, import.meta.url)), false, `${name} 应已删除`)
  }
  for (const marker of ['--content-left', 'invOpen', 'max-width: 1400px', ':has(.panel-body > .empty:only-child)', 'z-index: 19']) {
    assert.ok(home.includes(marker), `布局/物品栏修复丢失：${marker}`)
  }
})
