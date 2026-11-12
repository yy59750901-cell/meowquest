import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'
import * as Vue from 'vue'
import { parse, compileScript, compileTemplate } from '@vue/compiler-sfc'
import * as tree from './cat-tree-room.js'
import * as interaction from './room-interaction.js'
import * as catalog from './nest-catalog.js'
import * as litter from './litter-box.js'
import * as multiCat from './multi-cat.js'
import { decorArt, getSceneLighting, isWallpaper } from './scene-art.js'
import { getShopDisplay } from './shop-display.js'

const source = readFileSync(new URL('./views/KidHome.vue', import.meta.url), 'utf8')
const { descriptor } = parse(source)
const bindings = Object.keys(compileScript(descriptor, { id: 'room-regression' }).bindings)
const template = compileTemplate({ source: descriptor.template.content, filename: 'KidHome.vue', id: 'room-regression' })
assert.deepEqual(template.errors, [])
const renderCode = template.code.replace(/import \{([^}]+)\} from "vue"/, (_, names) => `const {${names.replace(/ as /g, ':')}} = Vue`).replace('export function render', 'function render')
const render = new Function('Vue', `${renderCode}; return render`)({ ...Vue,
  resolveComponent: name => name,
  withDirectives: (vnode, directives) => { vnode.testDirectives = directives; return vnode },
})
const plain = value => JSON.parse(JSON.stringify(value))

function harness() {
  const timers = new Map(), frames = new Map(), listeners = new Map(), hooks = {}, watchers = [], toasts = [], requests = [], storage = []
  let now = 0, id = 0, ui, nextResponse = null
  const saved = new Map(), viewportCallbacks = new Set()
  const host = prefix => ({ addEventListener: (key, fn) => listeners.set(`${prefix}:${key}`, fn), removeEventListener: key => listeners.delete(`${prefix}:${key}`) })
  const document = { hidden: false, activeElement: null, ...host('document'), body: { appendChild() {} }, createElement: () => ({ style: {}, appendChild() {}, remove() {} }) }
  const focusable = name => Vue.markRaw({ name, isConnected: true, focus() { document.activeElement = this }, getBoundingClientRect: () => ({ left: 350, top: 260, width: 192, height: 192 }), querySelector: () => null })
  const trigger = focusable('爬架'), close = focusable('关闭'), action = focusable('动作')
  const context = {
    ...tree, ...interaction, ...catalog, ...litter, ...multiCat,
    planInteractionSession: key => interaction.planInteractionSession(key, () => .9),
    pickInteractionNode: (key, current) => interaction.pickInteractionNode(key, current, () => 0),
    AbortController, decorArt, getSceneLighting, isWallpaper, getShopDisplay, ref: Vue.ref, computed: Vue.computed, reactive: Vue.reactive, nextTick: Vue.nextTick,
    onMounted: fn => { hooks.mount = fn }, onBeforeUnmount: fn => { hooks.cleanup = fn }, watch: (get, fn) => watchers.push([get, fn]),
    showToast: text => toasts.push(text), closeToast() {}, showDialog() {}, showConfirmDialog: async () => {}, reviewPending: () => false, reviewStages: {}, evidenceSteps: () => [],
    document, window: { innerWidth: 1280, innerHeight: 720, matchMedia: () => ({ matches: false }), ...host('window') },
    localStorage: { setItem: (key, value) => { storage.push([key, value]); saved.set(key, value) }, removeItem: key => { storage.push([key]); saved.delete(key) }, getItem: key => saved.get(key) || null },
    performance: { now: () => now }, Blob, FormData, URL, TextDecoder,
    Math: Object.assign(Object.create(Math), { random: () => .9 }),
    navigator: { mediaDevices: { getUserMedia: async () => { throw new Error('未授权') } } },
    MediaRecorder: class { static isTypeSupported() { return true } start() {} stop() {} },
    ResizeObserver: class { constructor(fn) { this.callback = fn } observe() { viewportCallbacks.add(this.callback) } disconnect() { viewportCallbacks.delete(this.callback) } },
    getComputedStyle: el => el.world ? { transform: 'none' } : { marginLeft: (el.actor || ui.primaryActor).treeRendered.value ? '-32px' : '-55px', marginTop: (el.actor || ui.primaryActor).treeRendered.value ? '-48px' : '-100px' },
    setTimeout: (fn, ms) => { timers.set(++id, { fn, at: now + ms }); return id }, clearTimeout: key => timers.delete(key),
    setInterval: (fn, ms) => { timers.set(++id, { fn, at: now + ms }); return id }, clearInterval: key => timers.delete(key),
    requestAnimationFrame: fn => { frames.set(++id, fn); return id }, cancelAnimationFrame: key => frames.delete(key),
    fetch: async (url, options) => {
      requests.push({ url, body: typeof options?.body === 'string' ? JSON.parse(options.body) : options?.body })
      if (nextResponse) { const fn = nextResponse; nextResponse = null; return fn(url, options) }
      const body = url.endsWith('/scene') ? { items: plain(ui.sceneItems.value) } : url.endsWith('/shop') ? { items: plain(ui.shopItems.value) } : {}
      return { ok: true, status: 200, json: async () => body }
    },
  }
  const script = descriptor.scriptSetup.content.replace(/^import .*$/gm, '')
  ui = runInNewContext(`${script}\n;({${bindings.map(key => `get ${key}() { return typeof ${key} === 'undefined' ? undefined : ${key} }`).join(',')}})`, context)
  const world = { world: true, getBoundingClientRect: () => ({ left: 0, top: 0 }) }
  ui.worldEl.value = world
  ui.roomEl.value = { clientWidth: 640, clientHeight: 400, clientLeft: 0, clientTop: 0, getBoundingClientRect: () => ({ left: 0, top: 0 }) }
  const attachActor = actor => {
    actor.catEl.value = Vue.markRaw({ actor, style: {}, offsetWidth: 110, getBoundingClientRect: () => ({ left: actor.catPos.x - (actor.treeRendered.value ? 32 : 55), top: actor.catPos.y - (actor.treeRendered.value ? 48 : 100) }), querySelector: () => null })
  }
  attachActor(ui.primaryActor)
  ui.adopted.value = true
  ui.pet.value = { name: '同一只猫', species: 'cow', balance: 70, hunger: 90, mood: 90 }
  ui.species.value = 'cow'
  ui.catPos.x = 250; ui.catPos.y = 600
  ui.shopItems.value = [{ key: 'cat-tree', owned: true, name: '猫爬架', price: 50 }, { key: 'plant', owned: true, name: '盆栽', price: 20 }]
  ui.sceneItems.value = [{ id: 8, item_key: 'cat-tree', x: 500, y: 600 }]
  ui.gamePickerClose.value = close
  ui.gamePickerEl.value = { querySelectorAll: () => [close, action] }
  const ctx = new Proxy(ui, { get: (target, key) => Vue.isRef(target[key]) ? target[key].value : target[key] })
  async function flush() { for (let i = 0; i < 10; i++) await Promise.resolve() }
  return {
    ...ui, document, trigger, close, action, toasts, requests, frames, timers, listeners, storage, watchers,
    seedMulti(snapshot = multiSnapshot()) { ui.applyPet(snapshot); let index = 0; for (const actor of ui.actors.values()) { attachActor(actor); actor.cancel(); Object.assign(actor.catPos, { x: 704 + index++ * 150, y: 590.4 }) } },
    attachActor, saved,
    resizeObserved: () => { for (const callback of viewportCallbacks) callback() },
    random: value => { context.Math.random = () => value; context.planInteractionSession = key => interaction.planInteractionSession(key, () => value) },
    cancelDrag: () => ui.cancelDecorDrag?.(),
    render: () => render(ctx, []),
    respond: fn => { nextResponse = fn },
    microphone: fn => { context.navigator.mediaDevices.getUserMedia = fn },
    async advance(ms) {
      const until = now + ms
      await flush()
      while (now < until) {
        now = Math.min(until, now + 20)
        const queued = [...frames]; frames.clear()
        for (const [, fn] of queued) fn(now)
        for (const [key, timer] of [...timers]) if (timer.at <= now) { timers.delete(key); timer.fn() }
        await flush()
      }
    },
    cleanup() { hooks.cleanup() }, flush,
  }
}
function nodes(vnode, result = []) {
  if (!vnode) return result
  if (Array.isArray(vnode)) { for (const child of vnode) nodes(child, result); return result }
  if (typeof vnode !== 'object') return result
  result.push(vnode)
  if (Array.isArray(vnode.children)) nodes(vnode.children, result)
  return result
}
function byId(ui, id) { return nodes(ui.render()).find(node => node.props?.id === id) }
function visiblePanels(ui) {
  return ['taskPanel', 'wishPanel', 'shopPanel'].filter(id => byId(ui, id).testDirectives[0][1])
}
function pointEvent(target, x = 500, y = 600) { return { currentTarget: target, target, button: 0, pointerId: 1, clientX: x, clientY: y, preventDefault() {}, stopPropagation() {} } }

test('48 像素画布四脚点准确，尺寸与原房间足点补偿一致', () => {
  assert.equal(tree.CAT_TREE_SIZE, 192)
  assert.equal(tree.TREE_CAT_SIZE, 64)
  assert.deepEqual(tree.TREE_NODES, { grip: { x: -52, y: -20 }, nest: { x: 36, y: -44 }, mid: { x: -40, y: -100 }, top: { x: 36, y: -152 } })
  assert.equal(tree.GROUND_CAT_FOOT_GAP, 100 - Math.round(110 * .75))
  for (const [node, pixel] of Object.entries({ grip: [11, 43], nest: [33, 37], mid: [14, 23], top: [33, 10] })) {
    const point = tree.treePoint({ x: 500, y: 600 }, node)
    assert.deepEqual(point, { x: 500 - 96 + pixel[0] * 4, y: 600 - 192 + pixel[1] * 4 })
    assert.equal(point.y - 48 + Math.round(tree.TREE_CAT_SIZE * .75), point.y)
  }
})

test('登架必须经过抓柱、中层；下架按安全节点反向，小步插值不瞬移', () => {
  assert.deepEqual(tree.treeRoute('top'), ['grip', 'mid', 'top'])
  assert.deepEqual(tree.treeRoute('nest'), ['grip', 'nest'])
  assert.deepEqual(tree.treeDownRoute('top'), ['mid', 'grip'])
  assert.deepEqual(tree.treeDownRoute('nest'), ['grip'])
  // 任意节点作为起点的最短路径：不绕回地面，同点不跳。
  assert.deepEqual(tree.treePath('mid', 'top'), ['top'])
  assert.deepEqual(tree.treePath('grip', 'top'), ['mid', 'top'])
  assert.deepEqual(tree.treePath('nest', 'top'), ['grip', 'mid', 'top'])
  assert.deepEqual(tree.treePath('top', 'nest'), ['mid', 'grip', 'nest'])
  assert.deepEqual(tree.treePath('top', 'top'), [])
  assert.equal(tree.treePath('top', 'unknown'), null)
  assert.deepEqual(tree.treeDownRoute('ground'), [])
  const anchor = { x: 500, y: 600 }
  let from = tree.treeGroundExit(anchor)
  for (const node of tree.treeRoute('top')) {
    const to = tree.treePoint(anchor, node)
    assert.ok(Math.hypot(to.x - from.x, to.y - from.y) < 120)
    let previous = from
    for (let i = 1; i <= 38; i++) {
      const next = tree.treeHopPoint(from, to, i / 38)
      assert.ok(Math.hypot(next.x - previous.x, next.y - previous.y) < 6)
      previous = next
    }
    assert.deepEqual(tree.treeHopPoint(from, to, 1), to)
    from = to
  }
  for (const x of [96, 1184]) for (const y of [446.4, 700]) for (const side of [-1, 1]) {
    const p = tree.treeGroundExit({ x, y }, side)
    assert.ok(p.x >= 70 && p.x <= 1190)
    assert.equal(p.y, y)
  }
  const view = { left: 200, top: 220, width: 640, height: 400 }
  const base = tree.treeVisibleBase(view)
  assert.ok(base.x - 96 >= view.left && base.x + 96 <= view.left + view.width)
  assert.ok(base.y - 192 >= view.top && base.y <= view.top + view.height)
})

test('点击热区覆盖四个互动点，空白归最近节点，越界夹取，分区不重叠', () => {
  assert.equal(tree.TREE_SPRITE_UNITS, 48)
  for (const [x, y, mode] of [[33, 10, 'top'], [14, 23, 'mid'], [11, 43, 'grip'], [33, 37, 'nest']]) {
    assert.equal(tree.treeHitNode(x, y), mode)
  }
  assert.equal(tree.treeHitNode(-5, -5), 'mid')
  assert.equal(tree.treeHitNode(99, 99), 'nest')
  assert.equal(tree.treeHitNode(NaN, undefined), 'mid')
  for (let x = 0; x <= 48; x += 3) for (let y = 0; y <= 48; y += 3) {
    assert.ok(['grip', 'nest', 'mid', 'top'].includes(tree.treeHitNode(x, y)))
  }
  for (const a of tree.TREE_HOT_ZONES) for (const b of tree.TREE_HOT_ZONES) {
    if (a === b) continue
    const overlap = Math.min(a.x2, b.x2) > Math.max(a.x1, b.x1) && Math.min(a.y2, b.y2) > Math.max(a.y1, b.y1)
    assert.equal(overlap, false, `${a.node} 与 ${b.node} 热区重叠`)
  }
  assert.deepEqual(tree.treeRoute('mid'), ['grip', 'mid'])
  assert.deepEqual(tree.treeDownRoute('mid'), ['grip'])
})

test('游戏解锁只认购买，房间互动仍要求摆放', () => {
  const base = { adopted: true, owned: true, placed: false, busy: false }
  assert.equal(tree.gameEntryReason(base), '')
  assert.equal(tree.gameEntryReason({ ...base, owned: false }), '先在商店解锁猫爬架吧')
  assert.equal(tree.gameEntryReason({ ...base, busy: true }), '先完成正在进行的操作，再和猫玩吧')
  assert.equal(tree.gameEntryReason({ ...base, adopted: false }), '先领养一只小猫吧')
  assert.match(tree.treeEntryReason(base), /摆进房间/)
})

test('SFC 真实 setup 和编译模板：桌面只一个面板，手机只跟抽屉选择，导航不重复', async () => {
  const ui = harness()
  assert.deepEqual(visiblePanels(ui), ['taskPanel'])
  ui.selectDesktopPanel('shop')
  assert.deepEqual(visiblePanels(ui), ['shopPanel'])
  const all = nodes(ui.render())
  assert.equal(all.filter(node => node.props?.role === 'tablist').length, 1)
  assert.equal(byId(ui, 'shopTab').props['aria-selected'], true)
  const shop = all.find(node => node.type === 'PixelShop')
  assert.equal(shop.props.balance, 70)
  assert.equal(shop.props.items.find(it => it.key === 'cat-tree').actionLabel, '收回')
  assert.equal(shop.props.items.find(it => it.key === 'cat-tree').placed, true)
  ui.isNarrow.value = true
  assert.deepEqual(visiblePanels(ui), [])
  ui.openSheet('shop')
  assert.deepEqual(visiblePanels(ui), ['shopPanel'])
  assert.equal(nodes(ui.render()).filter(node => node.props?.role === 'tablist').length, 0)
  assert.equal(nodes(ui.render()).find(node => node.props?.class === 'sheet-nav').children.length, 4)
  ui.openSheet('wish')
  assert.deepEqual(visiblePanels(ui), ['wishPanel'])
  await ui.flush()
  ui.cleanup()
})

test('游戏入口只认购买、不要求摆放；无参/MouseEvent 入口仍是羽毛，同猫品种传入', async () => {
  const ui = harness()
  ui.shopItems.value[0].owned = false
  await ui.openTreeGame()
  assert.equal(ui.playOpen.value, false)
  assert.match(ui.toasts.at(-1), /解锁/)
  ui.shopItems.value[0].owned = true
  ui.sceneItems.value = [] // 收回爬架后游戏仍在列表里、仍可进入
  await ui.openTreeGame()
  assert.equal(ui.gameKind.value, 'climb')
  assert.equal(ui.playOpen.value, true)
  assert.equal(nodes(ui.render()).find(node => node.type === 'CatClimb').props.species, 'cow')
  ui.closePlayground()
  await ui.openPlayground({ type: 'click' })
  assert.equal(ui.gameKind.value, 'feather')
  assert.equal(ui.playOpen.value, true)
  const feather = nodes(ui.render()).find(node => node.type === 'CatPlayground')
  assert.equal(feather.props.species, 'cow')
  assert.equal(feather.props['cat-name'], '同一只猫')
  // 游戏打开期间 resize 不能把冻结的相机拉回默认位置。
  ui.cam.value = { x: -80, y: -110 }
  ui.onViewportResize()
  await ui.flush()
  assert.deepEqual(plain(ui.cam.value), { x: -80, y: -110 })
  ui.cleanup()
})

test('和猫玩列表：未购买只有羽毛，购买后出现无限爬高；打开列表不冻结房间', async () => {
  const ui = harness()
  ui.shopItems.value[0].owned = false
  ui.openGamePicker(ui.trigger)
  await ui.flush()
  assert.equal(ui.gamePicker.value, true)
  assert.equal(ui.playOpening.value, false)
  assert.equal(ui.playOpen.value, false)
  const cards = nodes(ui.render()).filter(node => node.props?.class?.includes?.('game-card'))
  assert.equal(cards.length, 1)
  assert.equal(nodes(ui.render()).find(node => node.props?.class?.includes?.('playground-entry'))?.props['aria-expanded'], true)
  ui.closeGamePicker()
  ui.shopItems.value[0].owned = true
  ui.openGamePicker(ui.trigger)
  await ui.flush()
  assert.equal(nodes(ui.render()).filter(node => node.props?.class?.includes?.('game-card')).length, 2)
  await ui.startGame('climb')
  assert.equal(ui.gamePicker.value, false)
  assert.equal(ui.gameKind.value, 'climb')
  assert.equal(ui.playOpen.value, true)
  ui.cleanup()
})

test('和猫玩在忙碌时只提示不展开列表；选中羽毛不误进爬高', async () => {
  const ui = harness()
  ui.wizard.value = { task: { id: 1 } }
  ui.openGamePicker(ui.trigger)
  assert.equal(ui.gamePicker.value, false)
  assert.match(ui.toasts.at(-1), /先完成/)
  ui.wizard.value = null
  ui.openGamePicker(ui.trigger)
  assert.equal(ui.gamePicker.value, true)
  await ui.startGame('feather')
  assert.equal(ui.gamePicker.value, false)
  assert.equal(ui.gameKind.value, 'feather')
  assert.equal(ui.playOpen.value, true)
  ui.cleanup()
})

test('游戏列表焦点、Escape、Tab 循环与关闭后归还焦点', async () => {
  const ui = harness()
  ui.openGamePicker(ui.trigger)
  await ui.flush()
  assert.equal(ui.document.activeElement, ui.close)
  const tab = { key: 'Tab', preventDefault() {}, stopPropagation() {} }
  ui.gamePickerKeydown(tab)
  assert.equal(ui.document.activeElement, ui.action)
  ui.gamePickerKeydown({ key: 'Escape', preventDefault() {}, stopPropagation() {} })
  assert.equal(ui.gamePicker.value, false)
  assert.equal(ui.document.activeElement, ui.trigger)
  ui.cleanup()
})

for (const busy of ['startingTask', 'submitting', 'evidenceUploading', 'timerTask', 'wizard', 'wizardUploading', 'taskRecStarting', 'chatStreaming', 'wallpaperSaving', 'shopActionBusy', 'treeLeaving']) {
  test(`房间入口不抢占 ${busy}；正在开游戏时任务、喂食、商店不能穿透`, async () => {
    const ui = harness()
    ui[busy].value = true
    await ui.openTreeGame()
    assert.equal(ui.playOpen.value, false)
    ui[busy].value = false
    const opening = ui.openPlayground()
    assert.equal(ui.playOpening.value, true)
    await ui.feed(); await ui.shopAction(ui.shopItems.value[0]); await ui.startTask({ id: 1 })
    assert.equal(ui.placingFeed.value, false)
    assert.equal(ui.requests.length, 0)
    await opening
    assert.equal(ui.playOpen.value, true)
    ui.cleanup()
  })
}

test('上架按连续节点抵达，脚点与 zIndex 对齐，不保存高度、不改变余额', async () => {
  const ui = harness()
  const action = ui.playTree(ui.sceneItems.value[0], 'top')
  await ui.advance(2200)
  assert.equal(ui.treeSession.value.currentnode, 'top')
  assert.equal(ui.treeRendered.value, true)
  assert.deepEqual(plain(ui.catPos), tree.treePoint({ x: 500, y: 600 }, 'top'))
  assert.equal(ui.catRoomStyle.value.marginTop, '-48px')
  assert.equal(ui.catRoomStyle.value.zIndex, 611)
  assert.equal(ui.catRoomStyle.value.width, '64px')
  assert.equal(ui.requests.length, 0)
  assert.equal(ui.storage.length, 0)
  assert.equal(ui.pet.value.balance, 70)
  ui.cleanup()
  await action
  assert.equal(ui.frames.size, 0)
  assert.equal(ui.timers.size, 0)
})

test('进游戏先走回地面，旧登架异步回调不能写冻结的房间；结束无悬空', async () => {
  const ui = harness()
  const action = ui.playTree(ui.sceneItems.value[0], 'top')
  await ui.advance(2200)
  const opening = ui.openTreeGame()
  assert.equal(ui.playOpening.value, true)
  assert.equal(ui.playOpen.value, false)
  await ui.advance(1600)
  await opening
  assert.equal(ui.playOpen.value, true)
  assert.equal(ui.treeSession.value, null)
  assert.equal(ui.treeRendered.value, false)
  assert.equal(ui.catPos.y, 600)
  const floor = plain(ui.catPos)
  await ui.advance(10000)
  assert.deepEqual(plain(ui.catPos), floor)
  ui.closePlayground()
  assert.equal(ui.catPos.y, 600)
  await action
  ui.cleanup()
})

test('收回必须等待下架，重复操作不发两次 API；失败释放 busy', async () => {
  const ui = harness()
  const action = ui.playTree(ui.sceneItems.value[0], 'nest')
  await ui.advance(1800)
  assert.equal(ui.treeSession.value.currentnode, 'nest')
  const removal = ui.shopAction(ui.shopItems.value[0])
  await ui.shopAction(ui.shopItems.value[0])
  assert.equal(ui.requests.length, 0)
  assert.equal(ui.treeLeaving.value, true)
  await ui.advance(1300)
  await removal
  const removes = ui.requests.filter(r => r.url.endsWith('/scene/remove'))
  assert.equal(removes.length, 1)
  assert.deepEqual(removes[0].body, { id: 8 })
  assert.equal(ui.catPos.y, 600)
  assert.equal(ui.shopActionBusy.value, false)
  ui.respond(() => { throw new Error('断网') })
  await ui.shopAction(ui.shopItems.value[0])
  assert.equal(ui.shopActionBusy.value, false)
  assert.match(ui.toasts.at(-1), /收回没有保存/)
  await action
  ui.cleanup()
})

for (const [x, y, mode] of [[33, 10, 'top'], [33, 37, 'nest'], [11, 43, 'grip'], [14, 23, 'mid']]) {
  test(`点爬架 (${x},${y}) 触发 ${mode} 反应，单击不弹菜单也不写摆放接口`, async () => {
    const ui = harness()
    const px = 350 + x / 48 * 192, py = 260 + y / 48 * 192
    const down = pointEvent(ui.trigger, px, py)
    ui.onItemPointerDown(down, ui.sceneItems.value[0], 8)
    assert.equal(ui.requests.length, 0)
    await ui.listeners.get('document:pointerup')({ ...down, type: 'pointerup' })
    await ui.flush()
    assert.equal(ui.gamePicker.value, false)
    assert.equal(ui.treeSession.value.mode, mode)
    assert.equal(ui.requests.length, 0)
    ui.cleanup()
  })
}

test('拖动松手才等待旧位置下架后保存，单击不误触摆放', async () => {
  const ui = harness()
  const action = ui.playTree(ui.sceneItems.value[0], 'top')
  await ui.advance(2200)
  assert.equal(ui.treeSession.value.currentnode, 'top')
  const down = pointEvent(ui.trigger)
  ui.onItemPointerDown(down, ui.sceneItems.value[0], 8)
  assert.equal(ui.treeSession.value.currentnode, 'top')
  ui.listeners.get('document:pointermove')({ pointerId: 1, clientX: 560, clientY: 300 })
  const persist = ui.listeners.get('document:pointerup')({ pointerId: 1, clientX: 560, clientY: 300, type: 'pointerup' })
  assert.equal(ui.treeLeaving.value, true)
  assert.equal(ui.requests.length, 0)
  await ui.advance(1600)
  await persist
  const placement = ui.requests.find(r => r.url.endsWith('/scene/place'))
  assert.equal(placement.body.id, 8)
  assert.equal(placement.body.key, 'cat-tree')
  assert.equal(ui.catPos.y, 600)
  assert.equal(ui.shopActionBusy.value, false)
  await action
  ui.cleanup()
})

test('爬架仍可键盘互动、位置提示更新；收回爬架后游戏入口不消失', async () => {
  const ui = harness()
  const furniture = nodes(ui.render()).find(node => node.props?.role === 'button' && node.props?.tabindex === 0)
  assert.match(furniture.props.title, /不同反应/)
  assert.equal(furniture.props['aria-expanded'], undefined)
  assert.match(furniture.props['aria-label'], /点不同位置/)
  // 键盘分支扩展为爬架 / 猫砂盆两条，语义不变，断言口径同步更新。
  const keyExpr = "it.item_key === 'cat-tree' ? playTree(it, 'top') : it.item_key === LITTER_KEY && openLitter(it)"
  assert.ok(source.includes(`@keydown.enter.prevent.stop="${keyExpr}"`), '爬架仍可键盘互动，猫砂盆也可点开铲屎')
  assert.ok(source.includes(`@keydown.space.prevent.stop="${keyExpr}"`), '空格键同样覆盖两条分支')
  ui.sceneItems.value = []
  assert.equal(ui.shopViewItems.value[0].actionLabel, '摆放')
  await ui.openTreeGame()
  assert.equal(ui.playOpen.value, true)
  ui.cleanup()
})

test('向导上传期间关闭/提交不能解除互斥或让旧上传复活新游戏', async () => {
  const ui = harness()
  let complete
  ui.wizard.value = { task: { id: 1 }, steps: [{ kind: 'audio', required: true }], done: {} }
  ui.wizardPick({ kind: 'audio' }, 0)
  ui.respond(() => new Promise(resolve => { complete = resolve }))
  const upload = ui.onWizardFile({ target: { files: [new Blob(['录音'], { type: 'audio/wav' })], value: 'file' } })
  await ui.flush()
  assert.equal(ui.wizardUploading.value, true)
  ui.closeWizard()
  await ui.wizardSubmit()
  await ui.openTreeGame()
  assert.ok(ui.wizard.value)
  assert.equal(ui.playOpen.value, false)
  complete({ ok: true, status: 200, json: async () => ({ path: '/uploads/audio.wav' }) })
  await upload
  assert.equal(ui.wizardUploading.value, false)
  assert.equal(ui.wizard.value.done[0].length, 1)
  ui.closeWizard()
  await ui.openTreeGame()
  assert.equal(ui.playOpen.value, true)
  ui.cleanup()
})

test('麦克风授权尚未返回时任务录音/聊天录音均阻止游戏；卸载后释放迟到的流', async () => {
  for (const kind of ['task', 'chat']) {
    const ui = harness()
    let grant, stopped = false
    ui.microphone(() => new Promise(resolve => { grant = resolve }))
    if (kind === 'task') ui.wizard.value = { steps: [{ kind: 'audio' }], done: {} }
    const pending = kind === 'task' ? ui.startTaskRec(0) : ui.recStart()
    if (kind === 'task') ui.closeWizard()
    await ui.openTreeGame()
    assert.equal(ui.playOpen.value, false)
    ui.cleanup()
    grant({ getTracks: () => [{ stop: () => { stopped = true } }] })
    await pending
    assert.equal(stopped, true)
    assert.equal(ui.playOpen.value, false)
  }
})

for (const state of ['recording', 'uploading']) {
  test(`聊天录音 ${state} 不允许进游戏`, async () => {
    const ui = harness()
    ui.recState.value = state
    await ui.openTreeGame()
    assert.equal(ui.playOpen.value, false)
    assert.equal(ui.requests.length, 0)
    ui.cleanup()
  })
}

test('已在架上点新目标走最短路：不回地面、不重放整条路线，姿态随目标切换', async () => {
  const ui = harness()
  const first = ui.playTree(ui.sceneItems.value[0], 'top')
  await ui.advance(2200)
  assert.equal(ui.treeSession.value.currentnode, 'top')
  assert.equal(ui.treeRendered.value, true)
  const plan = ui.treeSession.value.plan
  assert.ok(plan.repeats >= 2 && plan.maxDuration === 25000)

  // 从 top 到 grip 应为 top→mid→grip 两跳，而不是先下架回地面再 grip→mid→top。
  const next = ui.playTree(ui.sceneItems.value[0], 'grip')
  assert.equal(ui.treeLeaving.value, false, '不应触发下架')
  assert.equal(ui.treeRendered.value, true, '猫应留在架上')
  assert.equal(ui.treeSession.value.requested, 'grip')
  await ui.advance(1400)
  assert.equal(ui.treeSession.value.currentnode, 'grip')
  assert.equal(ui.catAnim.value, 'crouch')
  assert.equal(ui.flipped.value, false, '离开窝后不应保留钻窝朝向')
  assert.equal(ui.requests.length, 0)
  assert.equal(ui.storage.length, 0)
  assert.equal(ui.pet.value.balance, 70)
  ui.cleanup()
  await first; await next
})

test('从窝里点最高处按 nest→grip→mid→top 前进，全程不落地', async () => {
  const ui = harness()
  const first = ui.playTree(ui.sceneItems.value[0], 'nest')
  await ui.advance(2200)
  assert.equal(ui.treeSession.value.currentnode, 'nest')
  assert.equal(ui.flipped.value, true)
  const next = ui.playTree(ui.sceneItems.value[0], 'top')
  assert.equal(ui.treeLeaving.value, false)
  await ui.advance(2000)
  assert.equal(ui.treeSession.value.currentnode, 'top')
  assert.equal(ui.treeRendered.value, true)
  assert.equal(ui.flipped.value, false)
  // 仍在架上最高处，没有被 finishTreeFloor 拉回地面出口
  assert.deepEqual(plain(ui.catPos), tree.treePoint({ x: 500, y: 600 }, 'top'))
  ui.cleanup()
  await first; await next
})

test('点击当前所在节点原地重播动作，不产生跳跃也不下架', async () => {
  const ui = harness()
  const action = ui.playTree(ui.sceneItems.value[0], 'top')
  await ui.advance(2200)
  const at = plain(ui.catPos)
  const again = ui.playTree(ui.sceneItems.value[0], 'top')
  assert.equal(ui.treeSession.value.requested, 'top')
  await ui.advance(300)
  assert.deepEqual(plain(ui.catPos), at)
  assert.equal(ui.treeSession.value.currentnode, 'top')
  assert.equal(ui.treeLeaving.value, false)
  assert.equal(ui.requests.length, 0)
  ui.cleanup()
  await action; await again
})

test('自动连玩会按 profile 换目标，不会一直停在同一个节点', async () => {
  const ui = harness()
  const action = ui.playTree(ui.sceneItems.value[0], 'top')
  await ui.advance(2200)
  const seen = new Set([ui.treeSession.value.currentnode])
  for (let i = 0; i < 14; i++) {
    await ui.advance(2000)
    if (!ui.treeSession.value) break
    seen.add(ui.treeSession.value.currentnode)
  }
  assert.ok(seen.size >= 2, `自动连玩应换过目标，实得 ${[...seen]}`)
  ui.cleanup()
  await action
})

test('玩球按 profile 连玩多轮，仍只在结束时保存一次球的新位置', async () => {
  const ui = harness()
  ui.sceneItems.value = [
    { id: 8, item_key: 'cat-tree', x: 500, y: 600 },
    { id: 10, item_key: 'yarn', x: 300, y: 600 },
  ]
  ui.interactWith(ui.sceneItems.value[1])
  await ui.advance(20000)
  const places = ui.requests.filter(r => r.url.endsWith('/scene/place'))
  assert.equal(places.length, 1, '连玩多轮不能重复写摆放')
  assert.equal(places[0].body.key, 'yarn')
  assert.equal(places[0].body.id, 10)
  ui.cleanup()
})

test('连续互动用完或到达总时长兜底后自动下架并恢复房间行为', async () => {
  const ui = harness()
  const action = ui.playTree(ui.sceneItems.value[0], 'top')
  await ui.advance(30000)
  assert.equal(ui.treeSession.value, null)
  assert.equal(ui.treeRendered.value, false)
  assert.equal(ui.catPos.y, 600)
  assert.equal(ui.requests.length, 0)
  assert.equal(ui.storage.length, 0)
  await action
  ui.cleanup()
})

test('普通商品摆放仍提示拖库存，爬架摆放特例调用既有 API', async () => {
  const ui = harness()
  await ui.shopAction(ui.shopItems.value[1])
  assert.equal(ui.requests.length, 0)
  assert.match(ui.toasts.at(-1), /物品栏拖进房间/)
  ui.sceneItems.value = []
  await ui.shopAction(ui.shopItems.value[0])
  const place = ui.requests.find(r => r.url.endsWith('/scene/place'))
  assert.equal(place.body.key, 'cat-tree')
  assert.ok(place.body.x >= 96 && place.body.x <= 1184)
  assert.ok(place.body.y >= 446.4 && place.body.y <= 700)
  assert.equal(ui.shopActionBusy.value, false)
  ui.cleanup()
})

test('不足 192px 的取景框不自动摆放裁切的爬架，明确引导库存拖入', async () => {
  const ui = harness()
  ui.sceneItems.value = []
  ui.roomEl.value.clientHeight = 140
  await ui.shopAction(ui.shopItems.value[0])
  assert.equal(ui.requests.length, 0)
  assert.match(ui.toasts.at(-1), /物品栏拖入/)
  assert.equal(ui.shopActionBusy.value, false)
  ui.cleanup()
})

test('猫在去架途中取消不跳到爬架地基，而停在实际地面位置', async () => {
  const ui = harness()
  const action = ui.playTree(ui.sceneItems.value[0], 'top')
  await ui.flush()
  assert.equal(ui.treeRendered.value, false)
  ui.catPos.x = 300; ui.catPos.y = 575
  await ui.leaveTree()
  await action
  assert.deepEqual(plain(ui.catPos), { x: 300, y: 575 })
  assert.equal(ui.treeSession.value, null)
  ui.cleanup()
})

test('没有爬架会话时重置树不打断普通 walkTo 计时', async () => {
  const ui = harness()
  let arrived = false
  await ui.walkTo({ x: 300, y: 600 }, () => { arrived = true })
  const timers = [...ui.timers.keys()]
  ui.resetTreeToFloor()
  assert.deepEqual([...ui.timers.keys()], timers)
  await ui.advance(600)
  assert.equal(arrived, true)
  ui.cleanup()
})

function multiSnapshot({ fed = false, full = false } = {}) {
  const account = { balance: fed ? 60 : 70, earned_total: 100, level: 3, streak: 2 }
  const pets = [{ id: 1, kid_id: 7, name: '橘子', species: 'orange', hunger: full ? 100 : fed ? 50 : 20, mood: fed ? 35 : 30 }, { id: 2, kid_id: 7, name: '小黑', species: 'black', hunger: full ? 100 : fed ? 100 : 85, mood: fed ? 99 : 94 }].map(p => ({ ...p, ...account }))
  return { pet: pets[0], pets, account, kid: { id: 7 }, pet_limit: 2, fed, all_full: full }
}
const feedRequests = ui => ui.requests.filter(r => r.url === '/api/pet/feed')
const response = data => ({ ok: true, status: 200, json: async () => data })

test('放碗前隐藏是未发送事务，取消全部猫且不扣费', async () => {
  const ui = harness(); ui.seedMulti()
  await ui.feed()
  ui.document.hidden = true; ui.onDocumentVisibility()
  assert.equal(feedRequests(ui).length, 0)
  assert.equal(ui.placingFeed.value, false)
  assert.equal(ui.timers.size, 0); assert.equal(ui.frames.size, 0)
  assert.equal(ui.pet.value.balance, 70)
  ui.cleanup()
})

test('一起开饭只发一次 API，动画前已结算；每猫不同饱腹与心情不串值、餐位不重叠', async () => {
  const ui = harness(); ui.seedMulti(); ui.selectCat(2)
  ui.respond(() => response(multiSnapshot({ fed: true })))
  await Promise.all([ui.feed(), ui.feed()])
  await Promise.all([ui.placeBowlAndGo(720, 600), ui.placeBowlAndGo(720, 600)])
  assert.equal(feedRequests(ui).length, 1)
  assert.ok(feedRequests(ui)[0].body.request_id)
  assert.equal(feedRequests(ui)[0].body.pet_id, 2)
  assert.equal(ui.selectedPetID.value, '2')
  assert.equal(ui.pet.value.balance, 60)
  assert.deepEqual(plain(ui.pets.value.map(p => [p.hunger, p.mood])), [[50, 35], [100, 99]])
  assert.equal(ui.showBowl.value, true)
  assert.ok(Math.abs(ui.mealSpots.value[0].x - ui.mealSpots.value[1].x) >= 110)
  await ui.advance(1600)
  assert.equal(ui.actors.get('1').catAnim.value, 'eat'); assert.equal(ui.actors.get('2').catAnim.value, 'eat')
  ui.cleanup()
})

test('全猫饱前端提示不请求；服务端 full 成功 no-op 也不播进食', async () => {
  const ui = harness(); ui.seedMulti(multiSnapshot({ full: true }))
  await ui.feed()
  assert.equal(feedRequests(ui).length, 0); assert.equal(ui.placingFeed.value, false)
  ui.applyPet(multiSnapshot()); ui.respond(() => response(multiSnapshot({ full: true })))
  await ui.feed(); await ui.placeBowlAndGo(720, 600)
  assert.equal(feedRequests(ui).length, 1); assert.equal(ui.pet.value.balance, 70)
  assert.equal(ui.showBowl.value, false); assert.equal(ui.gameBusy(), false)
  ui.cleanup()
})

for (const boundary of ['hidden', 'unmount']) {
  test(`结算成功后的全猫动画在 ${boundary} 取消；旧 timer 无法复活或再次扣费`, async () => {
    const ui = harness(); ui.seedMulti(); ui.respond(() => response(multiSnapshot({ fed: true })))
    await ui.feed(); await ui.placeBowlAndGo(720, 600); await ui.advance(1600)
    const stale = [...ui.timers.values()]
    if (boundary === 'hidden') { ui.document.hidden = true; ui.onDocumentVisibility() } else ui.cleanup()
    for (const timer of stale) await timer.fn()
    await ui.advance(10000)
    assert.equal(ui.pet.value.balance, 60); assert.equal(feedRequests(ui).length, 1)
    assert.equal(ui.showBowl.value, false); assert.equal(ui.gameBusy(), false)
    assert.equal(ui.frames.size, 0); assert.equal(ui.timers.size, 0)
    if (boundary === 'hidden') ui.cleanup()
  })
}

test('断网后不自动补发；显式重试使用相同 request_id 并只更新一次结果', async () => {
  const ui = harness(); ui.seedMulti()
  await ui.feed(); ui.respond(() => { throw new Error('断网') }); await ui.placeBowlAndGo(720, 600)
  const originalID = feedRequests(ui)[0].body.request_id
  assert.equal(ui.mealPhase.value, 'unknown'); assert.equal(ui.gameBusy(), false)
  await ui.advance(1000); assert.equal(feedRequests(ui).length, 1)
  await ui.feed(); ui.respond(() => response(multiSnapshot({ fed: true }))); await ui.placeBowlAndGo(720, 600)
  assert.equal(feedRequests(ui)[1].body.request_id, originalID)
  assert.equal(ui.pet.value.balance, 60)
  ui.cleanup()
})

for (const outcome of ['success', 'failure']) {
  test(`请求在途隐藏仍互斥，迟到 ${outcome} 不重复结算、不播放动画`, async () => {
    const ui = harness(); ui.seedMulti()
    let resolve, reject
    await ui.feed(); ui.respond(() => new Promise((a, b) => { resolve = a; reject = b }))
    const pending = ui.placeBowlAndGo(720, 600)
    await ui.flush(); ui.selectCat(2)
    ui.document.hidden = true; ui.onDocumentVisibility()
    await ui.feed(); assert.equal(feedRequests(ui).length, 1); assert.equal(ui.gameBusy(), true)
    if (outcome === 'success') resolve(response(multiSnapshot({ fed: true }))); else reject(new Error('断网'))
    await pending
    assert.equal(ui.pet.value.balance, outcome === 'success' ? 60 : 70)
    assert.equal(ui.selectedPetID.value, '2'); assert.equal(ui.showBowl.value, false)
    assert.equal(ui.gameBusy(), false); assert.equal(ui.timers.size, 0)
    ui.cleanup()
  })
}

test('POST 在途卸载后迟到成功不写宠物、不说话、不创建计时', async () => {
  const ui = harness()
  let resolvePost
  ui.respond(() => new Promise(resolve => { resolvePost = resolve }))
  await ui.feed()
  ui.placeBowlAndGo(308, 600)
  await ui.advance(2700)
  assert.equal(ui.requests.filter(r => r.url === '/api/pet/feed').length, 1)
  ui.cleanup()
  assert.equal(ui.timers.size, 0)
  resolvePost({ ok: true, status: 200, json: async () => ({ pet: { balance: 60 } }) })
  await ui.flush()
  await ui.advance(10000)
  assert.equal(ui.pet.value.balance, 70)
  assert.equal(ui.showBowl.value, false)
  assert.equal(ui.showBubble.value, false)
  assert.equal(ui.gameBusy(), false)
  assert.equal(ui.timers.size, 0)
  assert.equal(ui.frames.size, 0)
  assert.equal(ui.requests.filter(r => r.url === '/api/pet/feed').length, 1)
})

for (const boundary of ['hidden', 'removed', 'unmount']) {
  test(`${boundary} 取消登架、停止 RAF/树计时；恢复不复用高度`, async () => {
    const ui = harness()
    const action = ui.playTree(ui.sceneItems.value[0], 'top')
    await ui.advance(1000)
    if (boundary === 'hidden') { ui.document.hidden = true; ui.onDocumentVisibility() }
    else if (boundary === 'removed') {
      ui.sceneItems.value = []
      for (const [source, callback] of ui.watchers) if (/sceneItems|floorFurnitureSnapshot/.test(String(source))) callback()
    } else ui.cleanup()
    await ui.flush()
    assert.equal(ui.treeRendered.value, false)
    assert.equal(ui.treeSession.value, null)
    assert.equal(ui.catPos.y, 600)
    assert.equal(ui.frames.size, 0)
    assert.equal(ui.storage.length, 0)
    await action
    if (boundary !== 'unmount') ui.cleanup()
  })
}

test('猫砂盆：干净时不开覆层，有结团时点开，铲完按数量发一次 scoop', async () => {
  const ui = harness()
  const box = { id: 91, item_key: 'litter-box', x: 400, y: 600, state: 0 }
  ui.sceneItems.value = [box]

  ui.openLitter(box)
  assert.equal(ui.litterItem.value, null, '干净时点小图不该弹覆层')
  assert.ok(ui.toasts.some(text => text.includes('干干净净')), '应给出等待小猫用过的提示')

  box.state = 2
  ui.openLitter(box)
  // ref 会把对象包成 reactive proxy，按 id/字段核对而不是引用相等。
  assert.equal(ui.litterItem.value?.id, 91, '脏时点小图打开同一个摆放实例')
  assert.equal(ui.litterItem.value?.state, 2)

  await ui.onLitterScooped(2)
  const scoop = ui.requests.find(req => req.url === '/api/litter/scoop')
  assert.ok(scoop, '关闭覆层要按铲掉的数量结算')
  assert.deepEqual(scoop.body, { id: 91, delta: 2 })
  assert.equal(box.state, 0, '后端返回 state=0 要立刻回写，小图变干净')

  ui.onLitterClose()
  assert.equal(ui.litterItem.value, null)
  ui.cleanup()
})

test('猫砂盆：状态只由猫在线用过触发，不按时间或离线累积', () => {
  const ui = harness()
  const box = { id: 92, item_key: 'litter-box', x: 420, y: 600, state: 0 }
  ui.sceneItems.value = [box]
  // Math.random 桩恒为 .9，behaviorTick 的 litter 分支 (r<0.12) 不会命中，
  // 但源码里不能出现按时间差累加状态的写法。
  assert.match(source, /litterUsedThisSession/)
  assert.match(source, /jpost\('\/litter\/use', \{ id: box\.id \}\)/)
  assert.doesNotMatch(source, /Date\.now\(\).*state\s*\+\+/, '不允许按离线时间攒脏污')
  assert.match(source, /clampLitterState\(it\.state\) < 3/, '满 3 团后不再让猫继续使用')
  ui.cleanup()
})

test('真实多猫 DOM 与全部运动状态隔离，选择不取消另一只爬架会话', async () => {
  const ui = harness(); ui.seedMulti()
  const one = ui.actors.get('1'), two = ui.actors.get('2')
  assert.notEqual(one.catPos, two.catPos); assert.notEqual(one.catEl.value, two.catEl.value)
  one.chat.input = '橘子的输入'; two.chat.input = '小黑的输入'
  one.say('橘子的气泡'); two.say('小黑的气泡')
  const action = one.playTree(ui.sceneItems.value[0], 'top'); await ui.advance(3000)
  const session = one.treeSession.value, position = plain(one.catPos), timers = [...ui.timers.keys()]
  ui.selectCat(2)
  assert.equal(one.treeSession.value, session); assert.deepEqual(plain(one.catPos), position)
  assert.deepEqual([...ui.timers.keys()], timers)
  assert.equal(ui.chatInput.value, '小黑的输入'); assert.equal(ui.pet.value.mood, 94)
  const cats = nodes(ui.render()).filter(n => n.props?.class?.includes?.('cat-wrap'))
  assert.equal(cats.length, 2)
  assert.deepEqual(cats.map(n => n.props['data-cat-id']), ['1', '2'])
  assert.equal(cats[0].props.style.width, '64px'); assert.equal(cats[1].props.style.transform, undefined)
  ui.cleanup(); await action
  assert.equal(ui.timers.size, 0); assert.equal(ui.frames.size, 0)
})

test('家具锁覆盖全猫：同爬架不能抢占，同窝不能抢占，移走非选中猫家具恢复地面', async () => {
  const ui = harness(); ui.seedMulti()
  const one = ui.actors.get('1'), two = ui.actors.get('2')
  const nest = { id: 11, item_key: 'nest-apple', x: 830, y: 600 }
  ui.sceneItems.value.push(nest)
  const first = one.playTree(ui.sceneItems.value[0], 'top'); await ui.advance(3000)
  ui.selectCat(2); await two.playTree(ui.sceneItems.value[0], 'top')
  assert.equal(two.treeSession.value, null); assert.equal(ui.reservations.owner(8), '1')
  two.interactWith(nest)
  one.interactWith(nest)
  assert.equal(ui.reservations.owner(11), '2')
  await ui.advance(3000)
  ui.selectCat(1)
  await ui.releaseFurniture(11)
  assert.equal(two.nestActive.value, null); assert.equal(two.nestWalking.value, null)
  assert.equal(ui.reservations.owner(11), undefined); assert.ok(two.catPos.y >= 446.4)
  const removal = ui.releaseFurniture(8); await ui.advance(1600); await removal
  assert.equal(one.treeRendered.value, false); assert.equal(ui.reservations.owner(8), undefined)
  ui.cleanup(); await first
})

test('非选中猫也自动进窝，七阶段进窝层只用自己的外观', async () => {
  const ui = harness(); ui.seedMulti()
  ui.pets.value[0].hunger = 60
  const nest = { id: 12, item_key: 'nest-tent', x: 704, y: 620 }
  ui.sceneItems.value = [nest]
  ui.selectCat(2); ui.random(.7)
  const one = ui.actors.get('1'); one.behaviorTick(); await ui.advance(1800)
  assert.equal(ui.reservations.owner(12), '1')
  const copies = nodes(ui.render()).filter(n => n.type === 'CatSprite' && n.props?.['pose-frame'])
  assert.ok(copies.length >= 1)
  assert.equal(copies[0].props.species, 'orange')
  assert.notEqual(one.nestEntry, ui.actors.get('2').nestEntry)
  await ui.advance(1700)
  assert.equal(one.nestActive.value?.id, 12); assert.equal(one.catRoomStyle.value.transform, 'scale(0.8)')
  ui.cleanup(); assert.equal(ui.frames.size, 0); assert.equal(ui.timers.size, 0)
})

for (const boundary of ['hidden', 'unmount']) {
  test(`两猫同时爬架和进窝在 ${boundary} 全部取消，旧回调不能重建计时`, async () => {
    const ui = harness(); ui.seedMulti()
    const one = ui.actors.get('1'), two = ui.actors.get('2')
    const nest = { id: 12, item_key: 'nest-tent', x: 854, y: 620 }; ui.sceneItems.value.push(nest)
    const first = one.playTree(ui.sceneItems.value[0], 'top'); two.interactWith(nest)
    await ui.advance(1000)
    const staleTimers = [...ui.timers.values()], staleFrames = [...ui.frames.values()]
    if (boundary === 'hidden') { ui.document.hidden = true; ui.onDocumentVisibility() } else ui.cleanup()
    for (const timer of staleTimers) await timer.fn()
    for (const frame of staleFrames) frame(4000)
    await ui.flush()
    assert.equal(one.treeSession.value, null); assert.equal(two.nestEntry.value, null); assert.equal(two.nestActive.value, null)
    assert.equal(ui.timers.size, 0); assert.equal(ui.frames.size, 0)
    assert.equal(ui.reservations.owner(8), undefined); assert.equal(ui.reservations.owner(12), undefined)
    await first
    if (boundary === 'hidden') ui.cleanup()
  })
}

test('小游戏冻结全屋，退出恢复两猫自主行为，选中品种作为游戏对象', async () => {
  const ui = harness(); ui.seedMulti(); ui.selectCat(2)
  const one = ui.actors.get('1'), two = ui.actors.get('2')
  await one.walkTo({ x: 600, y: 600 }, () => one.behaviorNext())
  await two.walkTo({ x: 900, y: 600 }, () => two.behaviorNext())
  await ui.openPlayground('feather')
  const positions = [plain(one.catPos), plain(two.catPos)]
  await ui.advance(10000)
  assert.deepEqual([plain(one.catPos), plain(two.catPos)], positions)
  assert.equal(ui.timers.size, 0); assert.equal(ui.frames.size, 0)
  assert.equal(nodes(ui.render()).find(n => n.type === 'CatPlayground').props.species, 'black')
  ui.closePlayground(); assert.equal(ui.timers.size, 2)
  ui.cleanup()
})

test('刷新共享进度不切回原猫，单 pet 旧返回只合并捕获 ID，记忆按 kid 隔离', async () => {
  const ui = harness(); ui.saved.set('meow_selected_cat_7', '2'); ui.seedMulti()
  assert.equal(ui.selectedPetID.value, '2')
  let resolve
  ui.respond(() => new Promise(r => { resolve = r }))
  const pending = ui.refreshPet('1'); ui.selectCat(2)
  resolve(response({ pet: { id: 1, kid_id: 7, hunger: 60, mood: 33, balance: 80, earned_total: 110, level: 4, streak: 3 } })); await pending
  assert.equal(ui.selectedPetID.value, '2'); assert.equal(ui.pet.value.mood, 94)
  assert.equal(ui.pet.value.balance, 80); assert.equal(ui.pets.value[0].mood, 33)
  const newKid = { ...multiSnapshot(), kid: { id: 8 }, pet: { id: 3, kid_id: 8, name: '白白', species: 'white', hunger: 99, mood: 77 }, pets: [{ id: 3, kid_id: 8, name: '白白', species: 'white', hunger: 99, mood: 77 }] }
  ui.applyPet(newKid)
  assert.equal(ui.selectedPetID.value, '3'); assert.equal(ui.kidID.value, '8'); assert.equal(ui.actors.has('1'), false)
  ui.cleanup()
})

test('迟到聊天记录与 SSE 绑定发起猫，切猫不串聊天、心情或气泡', async () => {
  const ui = harness(); ui.seedMulti()
  const one = ui.actors.get('1'), two = ui.actors.get('2')
  let history
  ui.respond(() => new Promise(resolve => { history = resolve }))
  const opening = ui.openChat(); await ui.flush(); ui.selectCat(2)
  history(response({ messages: [{ role: 'assistant', content: '橘子的历史' }], left_today: 20 })); await opening
  assert.equal(one.chat.messages[0].content, '橘子的历史'); assert.equal(two.chat.messages.length, 0)
  ui.selectCat(1); ui.chatInput.value = '你好'
  let chunk, reads = 0
  ui.respond(() => ({ ok: true, status: 200, body: { getReader: () => ({ read: () => ++reads === 1 ? new Promise(resolve => { chunk = resolve }) : Promise.resolve({ done: true }), cancel: async () => {} }) } }))
  const streaming = ui.sendChat(); await ui.flush(); ui.selectCat(2)
  chunk({ done: false, value: new TextEncoder().encode('data: {"d":"橘子的回复"}\n\n') }); await streaming
  assert.equal(one.chat.messages.at(-1).content, '橘子的回复'); assert.equal(two.chat.messages.length, 0)
  assert.equal(ui.pet.value.mood, 94); assert.equal(ui.chatStreaming.value, false)
  assert.equal(ui.requests.find(r => r.url === '/api/chat').body.pet_id, 1)
  ui.cleanup()
})

test('摸摸发送原生数值 pet_id，切猫后迟到回调只影响发起猫', async () => {
  const ui = harness(); ui.seedMulti()
  let resolve
  ui.respond(() => new Promise(r => { resolve = r }))
  const pending = ui.petCat(); await ui.flush(); ui.selectCat(2)
  resolve(response({ touch_left_today: 2 })); await pending
  assert.equal(ui.requests.find(r => r.url === '/api/pet/touch').body.pet_id, 1)
  assert.equal(ui.selectedPetID.value, '2'); assert.equal(ui.pet.value.mood, 94)
  assert.match(ui.requests.find(r => r.url.startsWith('/api/pet?')).url, /pet_id=1/)
  assert.equal(ui.actors.get('2').showBubble.value, false)
  ui.cleanup()
})

test('追加领养免费且只发一次、可选名字外观，不新增礼包字段', async () => {
  const ui = harness()
  const snapshot = multiSnapshot(); snapshot.pets = [snapshot.pets[0]]
  ui.seedMulti(snapshot); ui.openAdditionalAdopt()
  ui.additionalName.value = '团团'; ui.additionalSpecies.value = 'cow'
  ui.respond(() => response({ ...multiSnapshot(), adopted: true, new_pet_id: 2 }))
  await Promise.all([ui.adoptAdditionalCat(), ui.adoptAdditionalCat()])
  const requests = ui.requests.filter(r => r.url === '/api/pet/adopt')
  assert.equal(requests.length, 1)
  assert.deepEqual(requests[0].body, { name: '团团', species: 'cow' })
  assert.equal(ui.pets.value.length, 2); assert.equal(ui.pet.value.balance, 70)
  assert.equal(ui.selectedPetID.value, '2'); assert.equal(ui.additionalAdoptOpen.value, false)
  ui.openAdditionalAdopt(); assert.equal(ui.additionalAdoptOpen.value, false)
  ui.cleanup()
})

test('记住第二只猫的首次房间初始化不会把两猫重置到同一脚点，手机仍四导航且 HUD/陪伴收起', () => {
  const ui = harness(); ui.saved.set('meow_selected_cat_7', '2'); ui.seedMulti()
  ui.isNarrow.value = true; ui.initCat()
  const one = ui.actors.get('1'), two = ui.actors.get('2')
  assert.ok(Math.abs(one.catPos.x - two.catPos.x) >= 110)
  assert.equal(ui.selectedPetID.value, '2'); assert.equal(ui.hudOpen.value, false); assert.equal(ui.toolsOpen.value, false)
  assert.equal(nodes(ui.render()).find(n => n.props?.class === 'sheet-nav').children.length, 4)
  ui.cleanup()
})

test('kid 切换后旧宠物请求迟到不会污染新身份、共享进度或选择', async () => {
  const ui = harness(); ui.seedMulti()
  let resolve
  ui.respond(() => new Promise(r => { resolve = r }))
  const pending = ui.refreshPet('1')
  const next = { ...multiSnapshot(), kid: { id: 8 }, pet: { id: 3, kid_id: 8, name: '白白', species: 'white', mood: 77, hunger: 80 }, pets: [{ id: 3, kid_id: 8, name: '白白', species: 'white', mood: 77, hunger: 80 }] }
  ui.applyPet(next)
  resolve(response(multiSnapshot({ fed: true }))); await pending
  assert.equal(ui.kidID.value, '8'); assert.equal(ui.selectedPetID.value, '3')
  assert.equal(ui.pet.value.name, '白白'); assert.equal(ui.pet.value.mood, 77); assert.equal(ui.pet.value.balance, 70)
  ui.cleanup()
})

function kidBSnapshot() {
  const snapshot = multiSnapshot()
  const pets = snapshot.pets.map(p => ({ ...p, id: p.id + 2, kid_id: 8, name: 'B-' + p.name }))
  return { ...snapshot, kid: { id: 8 }, pet: pets[0], pets }
}
function adoptWatcher(ui) { return ui.watchers.find(([get]) => get === ui.adopted)[1] }

for (const method of ['jget', 'refreshScene', 'refreshMessages', 'syncMail', 'openMailbox']) {
  test(`${method} 丢弃正常 200 的旧 kid 家具/私信，不能覆盖或播报新 kid`, async () => {
    const ui = harness(); ui.seedMulti()
    let resolve
    ui.respond(() => new Promise(r => { resolve = r }))
    const pending = method === 'jget' ? ui.jget('/scene') : ui[method]()
    await ui.flush()
    ui.applyPet(kidBSnapshot())
    ui.sceneItems.value = [{ id: 42, item_key: 'plant', x: 500, y: 600 }]
    ui.messages.value = [{ id: 43, content: 'B 的私信' }]; ui.unread.value = 1
    resolve(response({ items: [{ id: 99, item_key: 'nest', content: 'A 的私信' }], unread: 8 }))
    const result = await pending
    if (method === 'jget') assert.equal(result, null)
    assert.equal(ui.sceneItems.value[0].id, 42)
    assert.equal(ui.messages.value[0].content, 'B 的私信'); assert.equal(ui.unread.value, 1)
    assert.equal(ui.showBubble.value, false)
    assert.equal(ui.requests.filter(r => r.url === '/api/kid/messages/read').length, 0)
    ui.cleanup()
  })
}

for (const method of ['refreshScene', 'refreshMessages']) {
  test(`${method} 的 caller guard 覆盖 jget 已解析完成、await 续接前切 kid 的竞态`, async () => {
    const ui = harness(); ui.seedMulti()
    ui.respond(() => ({ ok: true, status: 200, json: () => ({ then(resolve) {
      resolve({ items: [{ id: 99, content: 'A 的旧私信' }], unread: 9 })
      Promise.resolve().then(() => {
        ui.applyPet(kidBSnapshot())
        ui.sceneItems.value = [{ id: 42, item_key: 'plant', x: 500, y: 600 }]
        ui.messages.value = [{ id: 43, content: 'B 的私信' }]; ui.unread.value = 1
      })
    } }) }))
    await ui[method]()
    assert.equal(ui.sceneItems.value[0].id, 42)
    assert.equal(ui.messages.value[0].content, 'B 的私信'); assert.equal(ui.unread.value, 1)
    ui.cleanup()
  })
}

test('jget 已收到 headers 但 JSON 解析仍在途，切 kid 后正文也被丢弃', async () => {
  const ui = harness(); ui.seedMulti()
  let resolveBody
  ui.respond(() => ({ ok: true, status: 200, json: () => new Promise(r => { resolveBody = r }) }))
  const pending = ui.jget('/scene'); await ui.flush(); ui.applyPet(kidBSnapshot())
  resolveBody({ items: [{ id: 99 }] })
  assert.equal(await pending, null)
  ui.cleanup()
})

test('信箱已读 POST 在途跨 kid，不能标记 B 的私信已读或清 B 的红点', async () => {
  const ui = harness(); ui.seedMulti(); ui.unread.value = 2
  let resolve
  ui.respond(() => new Promise(r => { resolve = r }))
  const pending = ui.openMailbox(); await ui.flush(); ui.applyPet(kidBSnapshot())
  ui.messages.value = [{ id: 44, content: 'B 的未读信' }]; ui.unread.value = 1
  resolve(response({ unread: 0 })); await pending
  assert.equal(ui.unread.value, 1); assert.equal(ui.messages.value[0].read_at, undefined)
  ui.cleanup()
})

test('放碗与准备开饭阶段不能换人，取消后换 B 不会由普通点击触发 meal', async () => {
  const ui = harness(); ui.seedMulti()
  await ui.feed(); await ui.switchKid()
  assert.equal(ui.kidID.value, '7'); assert.equal(ui.adopted.value, true)
  assert.match(ui.toasts.at(-1), /先完成/)
  ui.cancelFeeding(); await ui.switchKid()
  assert.equal(ui.placingFeed.value, false); assert.equal(ui.feedPreparing.value, false)
  ui.applyPet(kidBSnapshot()); ui.adopted.value = true
  for (const actor of ui.actors.values()) ui.attachActor(actor)
  const down = { button: 0, pointerId: 1, clientX: 200, clientY: 600, target: { closest: () => null } }
  ui.onRoomTap(down)
  ui.listeners.get('document:pointerup')({ ...down, type: 'pointerup' }); await ui.flush()
  assert.equal(feedRequests(ui).length, 0); assert.equal(ui.mealPhase.value, 'idle')
  ui.cleanup()
})

test('下架等待阶段有 identity 和取消 token；旧 finally 不能清 B 的新准备状态', async () => {
  const ui = harness(); ui.seedMulti()
  let finishA, finishB
  ui.actors.get('1').leaveTree = () => new Promise(r => { finishA = r })
  const first = ui.feed(); await ui.flush()
  assert.equal(ui.feedPreparing.value, true)
  await ui.switchKid(); assert.equal(ui.kidID.value, '7')
  ui.applyPet(kidBSnapshot())
  assert.equal(ui.feedPreparing.value, false); assert.equal(ui.placingFeed.value, false)
  for (const actor of ui.actors.values()) ui.attachActor(actor)
  ui.actors.get('3').leaveTree = () => new Promise(r => { finishB = r })
  const second = ui.feed(); await ui.flush()
  finishA(); await first
  assert.equal(ui.feedPreparing.value, true); assert.equal(ui.placingFeed.value, false)
  finishB(); await second
  assert.equal(ui.placingFeed.value, true); assert.equal(ui.kidID.value, '8')
  assert.equal(feedRequests(ui).length, 0)
  ui.cleanup()
})

test('同 kid 取消下架等待后，迟到 continuation 不能重新进入放碗模式', async () => {
  const ui = harness(); ui.seedMulti()
  let finish
  ui.actors.get('1').leaveTree = () => new Promise(r => { finish = r })
  const pending = ui.feed(); await ui.flush(); ui.cancelFeeding()
  finish(); await pending
  assert.equal(ui.feedPreparing.value, false); assert.equal(ui.placingFeed.value, false)
  assert.equal(feedRequests(ui).length, 0)
  ui.cleanup()
})

test('adopted=false watcher 清理开饭 UI，旧 pending 仍在原 kid 的存储中', async () => {
  const ui = harness(); ui.seedMulti()
  await ui.feed(); ui.respond(() => { throw new Error('断网') }); await ui.placeBowlAndGo(720, 600)
  const original = ui.saved.get('meow_meal_7')
  assert.equal(ui.mealPhase.value, 'unknown')
  ui.adopted.value = false; adoptWatcher(ui)(false)
  assert.equal(ui.mealPhase.value, 'idle'); assert.equal(ui.showBowl.value, false)
  assert.equal(ui.feedPreparing.value, false); assert.equal(ui.placingFeed.value, false)
  assert.equal(ui.saved.get('meow_meal_7'), original)
  ui.cleanup(); assert.equal(ui.timers.size, 0)
})

for (const outcome of ['success', 'error']) {
  test(`A 开饭请求在途换 B 后迟到 ${outcome} 不影响 B UI；事务结果只写 A 存储键`, async () => {
    const ui = harness(); ui.seedMulti()
    let resolve
    await ui.feed(); ui.respond(() => new Promise(r => { resolve = r }))
    const pending = ui.placeBowlAndGo(720, 600); await ui.flush()
    ui.applyPet(kidBSnapshot())
    assert.equal(ui.gameBusy(), false); assert.equal(ui.mealPhase.value, 'idle')
    assert.ok(ui.saved.get('meow_meal_7'))
    resolve(outcome === 'success' ? response(multiSnapshot({ fed: true })) : { ok: false, status: 500, json: async () => ({ error: '快照生成失败' }) })
    await pending
    assert.equal(ui.kidID.value, '8'); assert.equal(ui.pet.value.balance, 70)
    assert.equal(ui.mealPhase.value, 'idle'); assert.equal(ui.showBowl.value, false)
    assert.equal(ui.saved.has('meow_meal_8'), false)
    assert.equal(ui.saved.has('meow_meal_7'), outcome === 'error')
    ui.cleanup()
  })
}

test('快照 500 后即使刷新成全猫饱，unknown 仍同 ID 重试、不退款、不新扣费', async () => {
  const ui = harness(); ui.seedMulti()
  await ui.feed(); ui.respond(() => ({ ok: false, status: 500, json: async () => ({ error: '结算后快照失败' }) })); await ui.placeBowlAndGo(720, 600)
  assert.equal(ui.mealPhase.value, 'unknown')
  const id = feedRequests(ui)[0].body.request_id
  const full = multiSnapshot({ full: true }); full.account.balance = 60; full.pets.forEach(p => { p.balance = 60 })
  ui.applyPet(full)
  await ui.feed(); assert.equal(ui.placingFeed.value, true)
  ui.respond(() => response(full)); await ui.placeBowlAndGo(720, 600)
  assert.equal(feedRequests(ui)[1].body.request_id, id)
  assert.equal(ui.pet.value.balance, 60); assert.equal(ui.showBowl.value, false)
  assert.equal(ui.saved.has('meow_meal_7'), false)
  ui.cleanup()
})

for (const state of ['shopActionBusy', 'litterItem', 'timerTask', 'wizard', 'wallpaperSaving']) {
  test(`${state} 结束后两猫继续自动行为，busy tick 只保留每猫一个重试 timer`, async () => {
    const ui = harness(); ui.seedMulti()
    ui[state].value = true
    for (const actor of ui.actors.values()) actor.behaviorNext(20)
    await ui.advance(3000)
    assert.equal(ui.timers.size, 2); assert.equal(ui.frames.size, 0)
    assert.equal(ui.reservations.owner(8), undefined)
    ui[state].value = false; ui.random(.5); await ui.advance(1300)
    assert.equal(ui.actors.get('1').walking, true); assert.equal(ui.actors.get('2').walking, true)
    ui.cleanup(); assert.equal(ui.timers.size, 0); assert.equal(ui.frames.size, 0)
  })
}

test('猫砂覆层和商店恢复点重启全部空闲猫，重复恢复不叠加 timer', async () => {
  const ui = harness(); ui.seedMulti(); await ui.flush(); ui.cancelAllCats()
  ui.litterItem.value = { id: 91 }; ui.onLitterClose()
  assert.equal(ui.timers.size, 2)
  ui.onLitterClose(); assert.equal(ui.timers.size, 2)
  ui.cancelAllCats(); await ui.shopAction(ui.shopItems.value[1])
  assert.equal(ui.timers.size, 2)
  ui.cleanup()
})

test('普通窝的自动睡眠保持 reservation 到 dwell 结束，恢复其它猫不能取消睡眠', async () => {
  const ui = harness(); ui.seedMulti(); ui.pets.value[0].hunger = 60; ui.random(.7)
  const one = ui.actors.get('1'), two = ui.actors.get('2')
  const nest = { id: 11, item_key: 'nest', x: 704, y: 590.4 }; ui.sceneItems.value = [nest]
  one.behaviorTick(); await ui.advance(300)
  assert.equal(one.catAnim.value, 'sleep'); assert.equal(ui.reservations.owner(11), '1')
  two.interactWith(nest); ui.resumeAllCats(1200); ui.resumeAllCats(1200)
  await ui.advance(10000)
  assert.equal(one.catAnim.value, 'sleep'); assert.equal(ui.reservations.owner(11), '1')
  assert.equal(two.nestActive.value, null)
  await ui.advance(200)
  assert.equal(one.catAnim.value, 'sit'); assert.equal(ui.reservations.owner(11), undefined)
  two.interactWith(nest); await ui.advance(1600)
  assert.equal(ui.reservations.owner(11), '2')
  two.cancel(); assert.equal(ui.reservations.owner(11), undefined)
  ui.cleanup()
})

for (const limit of [3, 4, 0, -1, 2.5, '3', null]) {
  test(`领养上限 ${limit} 按服务端正整数校验，标题与上限文案动态`, () => {
    const ui = harness(); ui.seedMulti({ ...multiSnapshot(), pet_limit: limit })
    assert.equal(ui.petLimit.value, Number.isSafeInteger(limit) && limit > 0 ? limit : 2)
    ui.openAdditionalAdopt()
    if (ui.petLimit.value > 2) {
      assert.equal(ui.additionalAdoptOpen.value, true)
      assert.match(byId(ui, 'additional-adopt-title').children, /第 3 只猫/)
      const paragraphs = nodes(ui.render()).filter(n => n.type === 'p').map(n => n.children)
      assert.ok(paragraphs.some(text => typeof text === 'string' && text.includes(`最多 ${limit} 只`)))
    } else assert.equal(ui.additionalAdoptOpen.value, false)
    ui.cleanup()
  })
}

test('饱腹 99.9 显示 99 且仍按精确阈值开饭，100 才显示 100', async () => {
  const ui = harness(); ui.seedMulti(multiSnapshot({ full: true }))
  ui.pets.value[0].hunger = 99.9; ui.selectCat(1)
  const hunger = nodes(ui.render()).find(n => n.props?.class?.includes?.('chip mini') && n.children?.some?.(child => child.children === '99'))
  assert.ok(hunger)
  await ui.feed(); assert.equal(ui.placingFeed.value, true)
  ui.cancelFeeding(); ui.pets.value[0].hunger = 100; ui.selectCat(1)
  assert.ok(nodes(ui.render()).some(n => n.type === 'small' && n.children === '100'))
  await ui.feed(); assert.equal(ui.placingFeed.value, false); assert.equal(feedRequests(ui).length, 0)
  ui.cleanup()
})

function assertCatsInsideViewport(ui) {
  const width = ui.roomEl.value.clientWidth
  for (const actor of ui.actors.values()) {
    assert.ok(actor.catPos.x - 55 + ui.cam.value.x >= 0, `猫 ${actor.id.value} 左侧不能裁切`)
    assert.ok(actor.catPos.x + 55 + ui.cam.value.x <= width, `猫 ${actor.id.value} 右侧不能裁切`)
    assert.equal(actor.treeCatSize.value, 110)
    assert.equal(actor.catRoomStyle.value.transform, undefined)
  }
  assert.equal(ui.WORLD_W, 1280); assert.equal(ui.WORLD_H, 720)
}

for (const width of [362, 292, 300]) for (const selected of ['1', '2']) {
  test(`手机视口 ${width}px 首屏及数据刷新：记忆选中 ${selected}，ResizeObserver 不覆盖双猫中点`, async () => {
    const ui = harness(); ui.saved.set('meow_selected_cat_7', selected); ui.seedMulti()
    ui.roomEl.value.clientWidth = width; ui.narrowMedia.matches = true; ui.isNarrow.value = true
    ui.initCat()
    ui.resizeObserved(); ui.resizeObserved(); await ui.flush()
    assert.equal(ui.cam.value.x, width / 2 - 779)
    assert.equal(ui.initialGroupFraming.value, true); assert.equal(ui.selectedPetID.value, selected)
    assertCatsInsideViewport(ui)
    ui.respond(() => response(multiSnapshot())); await ui.refreshPet()
    ui.resizeObserved(); await ui.flush()
    assert.equal(ui.cam.value.x, width / 2 - 779); assertCatsInsideViewport(ui)
    ui.cleanup()
  })
}

test('单猫首次载入、ResizeObserver 与刷新仍按原 selected follow，不改变尺寸', async () => {
  const ui = harness(); const snapshot = multiSnapshot(); snapshot.pets = [snapshot.pets[0]]
  ui.seedMulti(snapshot); ui.roomEl.value.clientWidth = 292
  ui.initCat(); ui.resizeObserved(); await ui.flush()
  assert.equal(ui.initialGroupFraming.value, false)
  assert.equal(ui.cam.value.x, 146 - 704)
  assertCatsInsideViewport(ui)
  ui.cleanup()
})

test('选猫退出首屏 group framing，后续 ResizeObserver 仍跟选中猫', async () => {
  const ui = harness(); ui.seedMulti(); ui.roomEl.value.clientWidth = 292
  ui.initCat(); ui.resizeObserved(); await ui.flush()
  ui.selectCat(2)
  assert.equal(ui.initialGroupFraming.value, false)
  assert.equal(ui.cam.value.x, 146 - 854)
  ui.resizeObserved(); await ui.flush()
  assert.equal(ui.cam.value.x, 146 - 854)
  ui.cleanup()
})

test('非选中猫自主活动不改相机，选中猫自主走动退出首屏 framing 并恢复 follow', async () => {
  const ui = harness(); ui.seedMulti(); ui.roomEl.value.clientWidth = 292; ui.initCat()
  const initialCamera = plain(ui.cam.value)
  await ui.actors.get('2').walkTo({ x: 900, y: 600 })
  assert.deepEqual(plain(ui.cam.value), initialCamera)
  ui.random(.5); ui.actors.get('1').behaviorTick(); await ui.flush()
  assert.equal(ui.initialGroupFraming.value, false)
  assert.equal(ui.cam.value.x, 146 - 620)
  ui.resizeObserved(); await ui.flush()
  assert.equal(ui.cam.value.x, 146 - ui.catPos.x)
  ui.cleanup()
})

for (const selected of ['1', '2']) {
  test(`开饭选中 ${selected}：餐位整体 framing 不被每猫 walkTo、选猫或 ResizeObserver 覆盖，结束恢复 selected`, async () => {
    const ui = harness(); ui.seedMulti(); ui.roomEl.value.clientWidth = 292; ui.selectCat(selected)
    ui.camMode.value = 'free'
    await ui.feed(); ui.respond(() => response(multiSnapshot({ fed: true }))); await ui.placeBowlAndGo(720, 600)
    const middle = (ui.mealSpots.value[0].x + ui.mealSpots.value[1].x) / 2
    assert.equal(ui.mealGroupFraming.value, true); assert.equal(ui.camMode.value, 'follow')
    assert.equal(ui.cam.value.x, 146 - middle)
    await ui.advance(1600)
    assert.equal(ui.cam.value.x, 146 - middle); assertCatsInsideViewport(ui)
    const nextSelected = selected === '1' ? '2' : '1'
    ui.selectCat(nextSelected); assert.equal(ui.cam.value.x, 146 - middle)
    ui.resetCam(); assert.equal(ui.cam.value.x, 146 - middle)
    ui.resizeObserved(); ui.onViewportResize(); await ui.flush()
    assert.equal(ui.cam.value.x, 146 - middle)
    await ui.advance(8400)
    assert.equal(ui.mealGroupFraming.value, false); assert.equal(ui.mealPhase.value, 'succeeded')
    assert.equal(ui.camMode.value, 'follow'); assert.equal(ui.selectedPetID.value, nextSelected)
    assert.equal(ui.cam.value.x, 146 - ui.catPos.x)
    assert.equal(feedRequests(ui).length, 1)
    ui.cleanup()
  })
}

function socialSnapshot(count = 2) {
  const snapshot = multiSnapshot()
  snapshot.pets = Array.from({ length: count }, (_, i) => ({ ...snapshot.pets[i % 2], id: i + 1, hunger: 70 }))
  snapshot.pet = snapshot.pets[0]; snapshot.pet_limit = count
  return snapshot
}
function socialRoom(count = 2) {
  const ui = harness(); ui.seedMulti(socialSnapshot(count))
  ui.sceneItems.value = [{ id: 10, item_key: 'yarn', x: 779, y: 590.4 }]
  return ui
}
for (const count of [3, 4]) {
  test(`${count}猫共享一个餐盘，分两批到位后各吃2s，候位不挤、只扣10一次`, async () => {
    const ui = harness(); ui.seedMulti(socialSnapshot(count))
    const fed = socialSnapshot(count); fed.account.balance = 60; fed.pets.forEach(p => { p.hunger = 100; p.balance = 60 }); fed.pet = fed.pets[0]; fed.fed = true
    ui.respond(() => response(fed))
    await ui.feed(); await ui.placeBowlAndGo(720, 600)
    const seen = new Set(), eatStarts = new Map()
    for (let i = 0; i < 400; i++) {
      await ui.advance(100)
      const positions = [...ui.actors.values()].map(a => a.catPos)
      for (let a = 0; a < positions.length; a++) for (let b = a + 1; b < positions.length; b++) {
        assert.ok(Math.hypot(positions[a].x - positions[b].x, positions[a].y - positions[b].y) >= 109.99, `阶段${ui.mealStage.value}不能重叠`)
      }
      const active = [...ui.actors.values()].filter(a => a.mealSeat.value !== null)
      assert.ok(active.length <= 2)
      if (ui.mealStage.value === 'eat') {
        if (!eatStarts.has(ui.mealBatch.value)) eatStarts.set(ui.mealBatch.value, i * 100)
        active.forEach(a => seen.add(a.id.value))
      }
      if (ui.mealBatch.value === 1) assert.ok(i * 100 - eatStarts.get(0) >= 2000)
      if (ui.showBowl.value) assert.equal(nodes(ui.render()).filter(n => n.props?.['data-meal-plate'] !== undefined).length, 1)
      if (ui.mealPhase.value === 'succeeded') break
    }
    assert.equal(seen.size, count); assert.equal(feedRequests(ui).length, 1); assert.equal(ui.pet.value.balance, 60)
    assert.equal(ui.showBowl.value, false); ui.cleanup()
  })
}
test('开饭running的移动、离席和候位猫翻转都立即生效，结束恢复常规transition', async () => {
  assert.match(source, /\.cat-inner \{ transition: transform \.3s;/)
  assert.match(source, /\.cat-inner\.meal-facing, \.cat-inner\.social-facing \{ transition: none; \}/)
  const ui = socialRoom(4); ui.sceneItems.value = []
  const fed = socialSnapshot(4); fed.account.balance = 60; fed.pets.forEach(p => { p.balance = 60 }); fed.pet = fed.pets[0]; fed.fed = true
  const inners = () => nodes(ui.render()).filter(node => String(node.props?.class || '').split(' ').includes('cat-inner'))
  assert.ok(inners().every(node => !node.props.class.includes('meal-facing') && !node.props.class.includes('social-facing')))
  ui.respond(() => response(fed)); await ui.feed(); await ui.placeBowlAndGo(720, 590)
  const stages = new Set()
  for (let i = 0; i < 350 && ui.mealAnimation.value === 'running'; i++) {
    const elements = inners(); assert.equal(elements.length, 4)
    assert.ok(elements.every(node => node.props.class.includes('meal-facing')), `阶段${ui.mealStage.value}所有参与猫禁用翻转插值`)
    stages.add(ui.mealStage.value)
    if (ui.mealBatch.value === 1 && ui.mealStage.value === 'eat') {
      const waiting = [...ui.actors.values()].filter(actor => actor.mealSeat.value === null)
      assert.equal(waiting.length, 2)
      for (const actor of waiting) actor.flipped.value = !actor.flipped.value
      assert.ok(inners().every(node => node.props.class.includes('meal-facing')))
    }
    await ui.advance(100)
  }
  assert.ok(stages.has('approach') && stages.has('eat') && stages.has('rotate'))
  assert.equal(ui.mealAnimation.value, 'completed')
  assert.ok(inners().every(node => !node.props.class.includes('meal-facing')))
  assert.equal(feedRequests(ui).length, 1); ui.cleanup()
})
test('社交yarn-wait候位同样禁用翻转transition，取消后恢复常规cat-inner', async () => {
  const ui = socialRoom(4); ui.interactWith(ui.sceneItems.value[0]); await ui.advance(600)
  const waiting = [...ui.actors.values()].filter(actor => actor.socialRole.value === 'yarn-wait')
  assert.equal(waiting.length, 2)
  waiting.forEach(actor => { actor.flipped.value = !actor.flipped.value })
  let inner = nodes(ui.render()).filter(node => String(node.props?.class || '').split(' ').includes('cat-inner'))
  assert.equal(inner.length, 4); assert.ok(inner.every(node => node.props.class.includes('social-facing')))
  ui.cancelSocial(false)
  inner = nodes(ui.render()).filter(node => String(node.props?.class || '').split(' ').includes('cat-inner'))
  assert.ok(inner.every(node => !node.props.class.includes('social-facing') && !node.props.class.includes('meal-facing')))
  ui.cleanup()
})
test('长路径餐盘不在未到位时提前取消，吃饭计时从最后一猫到位才开始', async () => {
  const ui = socialRoom(); Object.assign(ui.actors.get('1').catPos, { x: 70, y: 446.4 }); Object.assign(ui.actors.get('2').catPos, { x: 1190, y: 700 })
  ui.respond(() => response(multiSnapshot({ fed: true })))
  await ui.feed(); await ui.placeBowlAndGo(1190, 600); await ui.advance(5000)
  assert.equal(ui.showBowl.value, true); assert.notEqual(ui.mealStage.value, 'eat')
  for (let i = 0; i < 150 && ui.mealStage.value !== 'eat'; i++) await ui.advance(100)
  assert.equal(ui.mealStage.value, 'eat'); assert.equal(ui.showBowl.value, true)
  await ui.advance(1800); assert.equal(ui.showBowl.value, true)
  await ui.advance(300); assert.equal(ui.showBowl.value, false); assert.equal(feedRequests(ui).length, 1); ui.cleanup()
})
for (const targetX of [800, 500]) {
  test(`正常盆栽eat转开饭${targetX}先安全撤离并入席2s，不能100ms假结束`, async () => {
    const ui = socialRoom(), plant = { id: 44, item_key: 'plant', x: 500, y: 600 }
    ui.sceneItems.value = [plant]
    const a = ui.actors.get('1'), b = ui.actors.get('2')
    Object.assign(a.catPos, { x: 350, y: 600 }); Object.assign(b.catPos, { x: 900, y: 600 })
    a.interactWith(plant); await ui.advance(2100)
    assert.equal(a.catAnim.value, 'eat'); assert.deepEqual(plain(a.catPos), { x: 552, y: 600 }); assert.equal(ui.reservations.owner(44), '1')
    ui.respond(() => response(multiSnapshot({ fed: true })))
    await ui.feed(); await ui.placeBowlAndGo(targetX, 590); await ui.advance(100)
    assert.equal(ui.mealPhase.value, 'animating'); assert.equal(ui.mealAnimation.value, 'running'); assert.equal(ui.showBowl.value, true)
    assert.ok([...ui.actors.values()].every(actor => !actor.bubble.value.includes('真好吃')))
    if (targetX === 500) assert.notEqual(ui.mealPlate.value.center.x, 500)
    let ate = false
    for (let i = 0; i < 120 && ui.mealStage.value !== 'eat'; i++) {
      await ui.advance(50)
      assert.ok(a.catPos.x > 548, '撤离时不能穿回盆栽实体')
      assert.ok(Math.hypot(a.catPos.x - b.catPos.x, a.catPos.y - b.catPos.y) >= 110)
    }
    ate = ui.mealStage.value === 'eat'; assert.equal(ate, true)
    assert.equal(a.mealSeat.value, 0); assert.equal(b.mealSeat.value, 1)
    await ui.advance(1800); assert.equal(ui.showBowl.value, true)
    await ui.advance(300); assert.equal(ui.showBowl.value, false); assert.equal(ui.mealAnimation.value, 'completed')
    assert.equal(ui.mealPhase.value, 'succeeded'); assert.equal(feedRequests(ui).length, 1); assert.equal(ui.pet.value.balance, 60)
    assert.ok(a.bubble.value.includes('真好吃')); assert.equal(ui.reservations.owner(44), undefined); ui.cleanup()
  })
}
test('餐盘无安全路线时提示已结算但动画失败，不显示感谢、不退款不重发', async () => {
  const ui = socialRoom(); ui.sceneItems.value = [{ id: 44, item_key: 'nest', x: 704, y: 590.4 }]
  ui.respond(() => response(multiSnapshot({ fed: true })))
  await ui.feed(); await ui.placeBowlAndGo(800, 590); await ui.advance(100)
  assert.equal(ui.mealPhase.value, 'succeeded'); assert.equal(ui.mealClient().state.phase, 'succeeded')
  assert.equal(ui.mealAnimation.value, 'failed'); assert.equal(ui.showBowl.value, false)
  assert.ok(ui.toasts.some(text => text.includes('已结算') && text.includes('进食动画') && text.includes('没能完成')))
  assert.ok([...ui.actors.values()].every(actor => !actor.bubble.value.includes('真好吃') && !actor.showBubble.value))
  assert.equal(ui.pet.value.balance, 60); assert.equal(feedRequests(ui).length, 1); assert.equal(ui.frames.size, 0)
  assert.equal(nodes(ui.render()).find(n => n.props?.class === 'room').props['data-meal-animation'], 'failed')
  await ui.advance(1000); assert.equal(feedRequests(ui).length, 1); assert.equal(ui.pet.value.balance, 60); ui.cleanup()
})
test('首批已吃但轮换路线失败也不假感谢，结算状态与动画结果分开', async () => {
  const ui = socialRoom(3), fed = socialSnapshot(3); fed.account.balance = 60; fed.pets.forEach(p => { p.balance = 60 }); fed.pet = fed.pets[0]; fed.fed = true
  ui.sceneItems.value = []; ui.respond(() => response(fed))
  await ui.feed(); await ui.placeBowlAndGo(720, 590)
  for (let i = 0; i < 150 && ui.mealStage.value !== 'eat'; i++) await ui.advance(100)
  assert.equal(ui.mealStage.value, 'eat'); assert.equal(ui.mealBatch.value, 0)
  ui.sceneItems.value = [{ id: 44, item_key: 'plant', x: ui.actors.get('1').catPos.x, y: 590 }]
  await ui.advance(2100)
  assert.equal(ui.mealAnimation.value, 'failed'); assert.equal(ui.mealPhase.value, 'succeeded')
  assert.equal(feedRequests(ui).length, 1); assert.equal(ui.pet.value.balance, 60)
  assert.ok(ui.toasts.some(text => text.includes('已结算') && text.includes('没能完成')))
  assert.ok([...ui.actors.values()].every(actor => !actor.bubble.value.includes('真好吃'))); ui.cleanup()
})
test('反序餐猫不会CSS同时互穿，RAF逐段避让并按实际左右坐席吃饭', async () => {
  const ui = socialRoom(); ui.sceneItems.value = []
  Object.assign(ui.actors.get('1').catPos, { x: 900, y: 590 }); Object.assign(ui.actors.get('2').catPos, { x: 540, y: 590 })
  ui.respond(() => response(multiSnapshot({ fed: true })))
  await ui.feed(); await ui.placeBowlAndGo(720, 590)
  const seen = new Set()
  for (let i = 0; i < 160; i++) {
    await ui.advance(25)
    const a = ui.actors.get('1'), b = ui.actors.get('2')
    assert.ok(a.catPos.x - b.catPos.x >= 110)
    assert.equal(a.catEl.value.style.transition, 'none'); assert.equal(b.catEl.value.style.transition, 'none')
    if (ui.mealStage.value === 'eat') {
      assert.equal(a.mealSeat.value, 1); assert.equal(b.mealSeat.value, 0)
      seen.add('eat')
    }
  }
  assert.ok(seen.has('eat')); assert.equal(ui.showBowl.value, false); assert.equal(feedRequests(ui).length, 1); ui.cleanup()
})
for (const x of [70, 1190]) {
  test(`毛球${x}边界含退步余量，完整4轮且保存一次`, async () => {
    const ui = socialRoom(), ball = ui.sceneItems.value[0]; ball.x = x
    const base = x === 70 ? 70 : 1020
    Object.assign(ui.actors.get('1').catPos, { x: base, y: 590.4 }); Object.assign(ui.actors.get('2').catPos, { x: base + 170, y: 590.4 })
    ui.interactWith(ball); const session = ui.socialSession.value; assert.ok(session)
    const rounds = new Set()
    for (let i = 0; i < 240; i++) {
      await ui.advance(100)
      if (!ui.socialSession.value) break
      for (const actor of ui.actors.values()) assert.ok(actor.catPos.x >= 70 && actor.catPos.x <= 1190)
      if (session.stage === 'catch') rounds.add(session.round)
    }
    assert.equal(session.round, 3); assert.equal(session.saved, true); assert.equal(rounds.size, 4)
    assert.equal(ui.socialSession.value, null); assert.equal(ui.requests.filter(r => r.url.endsWith('/scene/place')).length, 1); ui.cleanup()
  })
}
test('同列追逐先让占位猫入座，真正追两段与交换后自然结束，不提前取消', async () => {
  const ui = socialRoom(); ui.sceneItems.value = []
  Object.assign(ui.actors.get('1').catPos, { x: 720, y: 480 }); Object.assign(ui.actors.get('2').catPos, { x: 720, y: 620 })
  assert.equal(ui.startChase(true), true); const session = ui.socialSession.value, stages = new Set(), roles = new Set()
  for (let i = 0; i < 360; i++) {
    await ui.advance(25)
    if (!ui.socialSession.value) break
    stages.add(session.stage); roles.add(session.roles.join(','))
    const [a, b] = session.activeIDs.map(id => ui.actors.get(id).catPos)
    assert.ok(Math.hypot(a.x - b.x, a.y - b.y) >= 110)
  }
  assert.ok(stages.has('chase') && stages.has('exchange') && stages.has('lookback'))
  assert.equal(roles.size, 2); assert.equal(session.round, 1); assert.equal(ui.socialSession.value, null)
  assert.equal(ui.requests.length, 0); assert.equal(ui.toasts.length, 0); ui.cleanup()
})
for (const change of ['drag', 'remove', 'move', 'refresh']) {
  test(`毛球roll时其他盆栽${change}取消整个会话，不留下RAF写球或保存`, async () => {
    const ui = socialRoom(), ball = ui.sceneItems.value[0], plant = { id: 44, item_key: 'plant', x: 1100, y: 650 }
    ui.sceneItems.value.push(plant); ui.interactWith(ball)
    for (let i = 0; i < 60 && ui.socialSession.value?.stage !== 'roll'; i++) await ui.advance(50)
    assert.equal(ui.socialSession.value?.stage, 'roll')
    const staleFrames = [...ui.frames.values()], position = { x: ball.x, y: ball.y }
    if (change === 'drag') {
      ui.onItemPointerDown(pointEvent(ui.trigger), plant, 44)
      ui.listeners.get('document:pointermove')({ pointerId: 1, clientX: 550, clientY: 600 }); ui.cancelDrag()
    } else if (change === 'remove') await ui.releaseFurniture(44)
    else if (change === 'move') {
      plant.x = 740
      for (const [source, callback] of ui.watchers) if (/sceneItems|floorFurnitureSnapshot/.test(String(source))) callback()
    } else {
      ui.respond(() => response({ items: [{ ...ball }, { ...plant, x: 740 }] })); await ui.refreshScene()
    }
    assert.equal(ui.socialSession.value, null); assert.equal(ui.reservations.owner(10), undefined)
    for (const frame of staleFrames) frame(10000)
    await ui.advance(1000); assert.deepEqual({ x: ball.x, y: ball.y }, position)
    assert.equal(ui.requests.filter(r => r.url.endsWith('/scene/place')).length, 0); ui.cleanup()
  })
}
test('毛球每帧移动不触发家具快照递归取消，移除其他盆栽快照立即取消', async () => {
  const ui = socialRoom(), plant = { id: 44, item_key: 'plant', x: 1100, y: 650 }
  ui.sceneItems.value.push(plant); ui.interactWith(ui.sceneItems.value[0])
  for (let i = 0; i < 60 && ui.socialSession.value?.stage !== 'roll'; i++) await ui.advance(50)
  const session = ui.socialSession.value; assert.equal(session.stage, 'roll')
  const ballX = session.ball.x; await ui.advance(150)
  for (const [source, callback] of ui.watchers) if (/sceneItems|floorFurnitureSnapshot/.test(String(source))) callback()
  assert.equal(ui.socialSession.value, session); assert.notEqual(session.ball.x, ballX)
  ui.sceneItems.value = ui.sceneItems.value.filter(it => it.id !== 44)
  for (const [source, callback] of ui.watchers) if (/sceneItems|floorFurnitureSnapshot/.test(String(source))) callback()
  assert.equal(ui.socialSession.value, null); ui.cleanup()
})
test('毛球只有session reservation和一个RAF，双击不重启，4轮交换自然结束只保存一次', async () => {
  const ui = socialRoom(); const ball = ui.sceneItems.value[0]
  ui.interactWith(ball); const session = ui.socialSession.value
  assert.equal(session.kind, 'yarn'); assert.equal(ui.reservations.owner(10), session.owner)
  ui.selectCat(2); ui.interactWith(ball); assert.equal(ui.socialSession.value, session)
  const stages = new Set(), roles = new Set()
  for (let i = 0; i < 240; i++) {
    await ui.advance(100)
    if (!ui.socialSession.value) break
    stages.add(session.stage); roles.add(session.roles.join(','))
    assert.ok(ui.frames.size <= 1)
    assert.equal([...ui.actors.values()].filter(a => a.socialRole.value).length, 2)
    const distance = Math.hypot(ui.actors.get('1').catPos.x - ui.actors.get('2').catPos.x, ui.actors.get('1').catPos.y - ui.actors.get('2').catPos.y)
    assert.ok(distance >= 110, `阶段${session.stage}两猫间距${distance}`)
    if (['tap', 'roll', 'catch'].includes(session.stage)) assert.ok(distance >= 169)
  }
  assert.equal(ui.socialSession.value, null)
  assert.ok(stages.has('tap') && stages.has('roll') && stages.has('pounce') && stages.has('catch'))
  assert.ok(roles.has('1,2') && roles.has('2,1'))
  assert.equal(ui.requests.filter(r => r.url.endsWith('/scene/place')).length, 1)
  assert.equal(session.saved, true); assert.equal(session.round, 3); assert.equal(ui.reservations.owner(10), undefined); ui.cleanup()
})
for (const count of [3, 4]) {
  test(`毛球${count}猫每轮只有2active，更多猫轮换可加入且相机只取当前双猫`, async () => {
    const ui = socialRoom(count); ui.roomEl.value.clientWidth = 292; ui.interactWith(ui.sceneItems.value[0])
    const seen = new Set(), history = []
    for (let i = 0; i < 350; i++) {
      await ui.advance(100)
      const s = ui.socialSession.value
      if (!s) break
      assert.equal(s.activeIDs.length, 2); s.activeIDs.forEach(id => seen.add(id))
      if (history.at(-1)?.stage !== s.stage) history.push({ stage: s.stage, round: s.round, ids: plain(s.activeIDs), positions: plain([...ui.actors.values()].map(a => a.catPos)) })
      if (['tap', 'roll', 'catch'].includes(s.stage)) {
        const active = s.activeIDs.map(id => ui.actors.get(id))
        assert.ok(active.every(a => a.catPos.x - 55 + ui.cam.value.x >= -.01 && a.catPos.x + 55 + ui.cam.value.x <= 292.01))
      }
    }
    assert.equal(seen.size, count, JSON.stringify(history)); assert.equal(ui.requests.filter(r => r.url.endsWith('/scene/place')).length, 1, JSON.stringify(history)); ui.cleanup()
  })
}
test('弱猫手动也不参与，睡眠和窝树占用猫不被毛球唤醒，缺伙伴保留单猫玩法', async () => {
  const ui = socialRoom(3); const one = ui.actors.get('1'), two = ui.actors.get('2'), three = ui.actors.get('3')
  ui.pets.value[0].hunger = 20; ui.interactWith(ui.sceneItems.value[0]); assert.equal(ui.socialSession.value, null); assert.equal(one.walking, false)
  two.catAnim.value = 'sleep'; three.socialRole.value = 'tree'
  ui.pets.value[0].hunger = 70; ui.interactWith(ui.sceneItems.value[0]); assert.equal(ui.socialSession.value, null)
  assert.equal(two.catAnim.value, 'sleep'); assert.equal(three.socialRole.value, 'tree'); assert.equal(ui.reservations.owner(10), '1')
  ui.cleanup()
})
for (const boundary of ['hidden', 'unmount', 'removed', 'drag', 'meal', 'touch', 'kid']) {
  test(`毛球合玩${boundary}整体cancel，旧timer/RAF不能写球、释放锁、无结束保存`, async () => {
    const ui = socialRoom(); const ball = ui.sceneItems.value[0]; ui.interactWith(ball); await ui.advance(1500)
    const staleTimers = [...ui.timers.values()], staleFrames = [...ui.frames.values()]
    if (boundary === 'hidden') { ui.document.hidden = true; ui.onDocumentVisibility() }
    else if (boundary === 'unmount') ui.cleanup()
    else if (boundary === 'removed') { ui.sceneItems.value = []; for (const [source, callback] of ui.watchers) if (/sceneItems|floorFurnitureSnapshot/.test(String(source))) callback() }
    else if (boundary === 'drag') {
      const event = pointEvent(ui.trigger); ui.onItemPointerDown(event, ball, 10)
      ui.listeners.get('document:pointermove')({ pointerId: 1, clientX: 550, clientY: 600 }); ui.cancelDrag()
    } else if (boundary === 'meal') await ui.feed()
    else if (boundary === 'touch') { ui.clickCat(Vue.reactive(ui.actors.get('1'))); await ui.flush() }
    else ui.applyPet(kidBSnapshot())
    const position = plain(ball)
    for (const timer of staleTimers) await timer.fn()
    for (const frame of staleFrames) frame(10000)
    await ui.advance(1000)
    assert.equal(ui.socialSession.value, null); assert.deepEqual(plain(ball), position); assert.equal(ui.reservations.owner(10), undefined)
    assert.equal(ui.requests.filter(r => r.url.endsWith('/scene/place')).length, 0)
    if (['hidden', 'unmount'].includes(boundary)) { assert.equal(ui.frames.size, 0); assert.equal(ui.timers.size, 0) }
    if (boundary !== 'unmount') ui.cleanup()
  })
}
for (const random of [.5, .7, .9]) {
  test(`四猫320验收布局在自主随机${random}时，候位计时不乱入且完成4round保存1`, async () => {
    const ui = socialRoom(4), positions = [{ x: 694, y: 590.4 }, { x: 864, y: 590.4 }, { x: 1150, y: 700 }, { x: 1000, y: 700 }]
    ui.roomEl.value.clientWidth = 292; ui.random(random)
    for (const [i, actor] of [...ui.actors.values()].entries()) {
      Object.assign(actor.catPos, positions[i]); ui.pets.value[i].hunger = 60 + i * 7; ui.pets.value[i].mood = 75
      actor.behaviorNext(1600)
    }
    ui.interactWith(ui.sceneItems.value[0]); const session = ui.socialSession.value
    assert.ok(session); assert.equal(session.members.size, 4)
    const rounds = new Set(), seen = new Set(), waits = new Map()
    for (let i = 0; i < 400; i++) {
      await ui.advance(100)
      if (!ui.socialSession.value) break
      assert.equal([...ui.actors.values()].filter(actor => actor.socialRole.value === 'yarn').length, 2)
      assert.equal([...ui.actors.values()].filter(actor => actor.socialRole.value === 'yarn-wait').length, 2)
      assert.equal(session.members.size, 4)
      session.activeIDs.forEach(id => seen.add(id))
      if (session.stage === 'catch') rounds.add(session.round)
      const positions = [...ui.actors.values()].map(actor => actor.catPos)
      for (let a = 0; a < positions.length; a++) for (let b = a + 1; b < positions.length; b++) {
        assert.ok(Math.abs(positions[a].x - positions[b].x) >= 109.99 || Math.abs(positions[a].y - positions[b].y) >= 82.99, '候位与轮换路线不能穿110×83猫实体')
      }
      for (const actor of ui.actors.values()) {
        assert.equal(actor.walking, false, '不能留下独立CSS自主走动')
        if (actor.socialRole.value === 'yarn-wait') {
          assert.equal(actor.catAnim.value, 'sit')
          const previous = waits.get(actor.id.value)
          if (previous) assert.deepEqual(plain(actor.catPos), previous, '候位猫不被behaviorTimer带走')
          waits.set(actor.id.value, plain(actor.catPos))
        } else waits.delete(actor.id.value)
      }
    }
    assert.equal(ui.socialSession.value, null); assert.equal(rounds.size, 4); assert.equal(seen.size, 4)
    assert.equal(session.saved, true); assert.equal(session.round, 3)
    assert.equal(ui.requests.filter(r => r.url.endsWith('/scene/place')).length, 1)
    assert.equal(ui.reservations.owner(10), undefined); assert.equal(ui.frames.size, 0)
    assert.ok([...ui.actors.values()].every(actor => actor.socialRole.value === ''))
    assert.ok(!ui.toasts.some(t => t.includes('合玩没能完成')))
    assert.equal(ui.timers.size, 4, '结束时每猫只恢复一个自主计时器')
    ui.cleanup(); assert.equal(ui.timers.size, 0)
  })
}
test('精确四猫320布局逐帧110×83矩形不相交，包含扑跃与候位且4轮save1', async () => {
  const ui = socialRoom(4), positions = [{ x: 694, y: 590.4 }, { x: 864, y: 590.4 }, { x: 1150, y: 700 }, { x: 1000, y: 700 }]
  ui.roomEl.value.clientWidth = 292
  for (const [i, actor] of [...ui.actors.values()].entries()) Object.assign(actor.catPos, positions[i])
  ui.interactWith(ui.sceneItems.value[0]); const session = ui.socialSession.value
  const stages = new Set(), rounds = new Set(), checked = new Set()
  let frameCount = 0
  for (let i = 0; i < 2750 && ui.socialSession.value; i++) {
    await ui.advance(16); if (!ui.socialSession.value) break
    frameCount++; stages.add(session.stage)
    if (session.stage === 'catch') rounds.add(session.round)
    const actors = [...ui.actors.values()]
    for (let a = 0; a < actors.length; a++) for (let b = a + 1; b < actors.length; b++) {
      const xOverlap = 110 - Math.abs(actors[a].catPos.x - actors[b].catPos.x)
      const yOverlap = 83 - Math.abs(actors[a].catPos.y - actors[b].catPos.y)
      checked.add([actors[a].socialRole.value, actors[b].socialRole.value].sort().join('+'))
      assert.ok(xOverlap <= .02 || yOverlap <= .02, `${session.stage}第${session.round}轮${actors[a].id.value}/${actors[b].id.value}框重叠${xOverlap}×${yOverlap}`)
    }
  }
  assert.ok(frameCount > 1000); assert.ok(stages.has('pounce') && stages.has('exchange'))
  assert.ok(checked.has('yarn+yarn') && checked.has('yarn+yarn-wait') && checked.has('yarn-wait+yarn-wait'))
  assert.equal(rounds.size, 4); assert.equal(session.saved, true)
  assert.equal(ui.requests.filter(r => r.url.endsWith('/scene/place')).length, 1); ui.cleanup()
})
test('等待猫仍属毛球会话，点击涉猫取消会恢复全部伙伴且旧timer不落盘', async () => {
  const ui = socialRoom(4); ui.interactWith(ui.sceneItems.value[0]); await ui.advance(600)
  const waiting = ui.actors.get('4'); assert.equal(waiting.socialRole.value, 'yarn-wait')
  const staleTimers = [...ui.timers.values()], staleFrames = [...ui.frames.values()]
  ui.clickCat(Vue.reactive(waiting)); await ui.flush()
  assert.equal(ui.socialSession.value, null); assert.equal(ui.reservations.owner(10), undefined)
  assert.ok([...ui.actors.values()].every(actor => actor.socialRole.value === ''))
  for (const timer of staleTimers) await timer.fn()
  for (const frame of staleFrames) frame(10000)
  assert.equal(ui.requests.filter(r => r.url.endsWith('/scene/place')).length, 0); ui.cleanup()
})
test('睡眠与弱猫不加入候位、不清掉它们自身计时，只有有力idle猫被会话持有', async () => {
  const ui = socialRoom(4), asleep = ui.actors.get('3'), weak = ui.actors.get('4')
  asleep.catAnim.value = 'sleep'; ui.pets.value[3].hunger = 20
  ui.interactWith(ui.sceneItems.value[0]); const session = ui.socialSession.value
  assert.deepEqual(plain([...session.members]), ['1', '2'])
  assert.equal(asleep.catAnim.value, 'sleep'); assert.equal(asleep.socialRole.value, ''); assert.equal(weak.socialRole.value, '')
  await ui.advance(5000); assert.equal(asleep.catAnim.value, 'sleep'); assert.ok(!session.members.has('3'))
  ui.cleanup()
})
test('合玩无法安全离场或入场时提示未完成，不能静默退出或保存球', async () => {
  const ui = socialRoom(3); ui.interactWith(ui.sceneItems.value[0]); const session = ui.socialSession.value
  await ui.advance(4300); assert.equal(session.stage, 'catch')
  Object.assign(ui.actors.get('3').catPos, ui.actors.get('1').catPos)
  await ui.advance(200)
  assert.equal(ui.socialSession.value, null); assert.equal(session.saved, false)
  assert.ok(ui.toasts.some(t => t.includes('合玩没能完成')))
  assert.equal(ui.requests.filter(r => r.url.endsWith('/scene/place')).length, 0)
  assert.equal(ui.reservations.owner(10), undefined); ui.cleanup()
})
test('合玩watchdog取消也提示未完成，无旧timer回写或多重提示', async () => {
  const ui = socialRoom(4); ui.interactWith(ui.sceneItems.value[0]); const session = ui.socialSession.value
  const watchdog = [...ui.timers.values()].find(t => t.at === 45000); assert.ok(watchdog)
  watchdog.fn(); await ui.flush()
  assert.equal(ui.socialSession.value, null); assert.equal(session.saved, false)
  assert.equal(ui.toasts.filter(t => t.includes('合玩没能完成')).length, 1)
  assert.equal(ui.requests.filter(r => r.url.endsWith('/scene/place')).length, 0); ui.cleanup()
})
test('毛球不唤醒睡眠猫，会话中能接纳刚空闲的新猫', async () => {
  const ui = socialRoom(3); const three = ui.actors.get('3'); three.catAnim.value = 'sleep'
  ui.interactWith(ui.sceneItems.value[0]); const session = ui.socialSession.value
  assert.deepEqual(plain(session.activeIDs), ['1', '2']); await ui.advance(3000)
  assert.equal(three.catAnim.value, 'sleep'); three.catAnim.value = 'sit'
  const seen = new Set(), rounds = new Set()
  for (let i = 0; i < 390 && ui.socialSession.value; i++) {
    await ui.advance(100); ui.socialSession.value?.activeIDs.forEach(id => seen.add(id))
    if (ui.socialSession.value?.stage === 'catch') rounds.add(ui.socialSession.value.round)
  }
  assert.ok(seen.has('3')); assert.equal(session.saved, true); assert.equal(rounds.size, 4)
  assert.equal(ui.requests.filter(r => r.url.endsWith('/scene/place')).length, 1); ui.cleanup()
})
test('毛球点击不打断真实爬架与猫窝占用，只有当前空闲强猫走单猫分支', async () => {
  const ui = socialRoom(4), one = ui.actors.get('1'), two = ui.actors.get('2'), three = ui.actors.get('3'), four = ui.actors.get('4')
  const treeItem = { id: 8, item_key: 'cat-tree', x: 500, y: 600 }, nest = { id: 13, item_key: 'nest-tent', x: 1004, y: 620 }
  ui.sceneItems.value.push(treeItem, nest)
  const climbing = one.playTree(treeItem, 'top'); three.interactWith(nest); four.catAnim.value = 'sleep'
  await ui.advance(4500); two.interactWith(ui.sceneItems.value[0])
  assert.equal(ui.socialSession.value, null); assert.equal(ui.reservations.owner(8), '1'); assert.equal(ui.reservations.owner(13), '3')
  assert.ok(one.treeSession.value); assert.ok(three.nestActive.value || three.nestEntry.value || three.nestWalking.value)
  assert.equal(four.catAnim.value, 'sleep'); assert.equal(ui.reservations.owner(10), '2')
  ui.cleanup(); await climbing
})
test('毛球结束保存请求在途换kid，迟到返回不能覆盖新kid家具和身份', async () => {
  const ui = socialRoom(); let resolve
  ui.respond(() => new Promise(r => { resolve = r }))
  ui.interactWith(ui.sceneItems.value[0]); await ui.advance(21000)
  assert.equal(ui.socialSession.value, null); assert.equal(typeof resolve, 'function')
  ui.applyPet(kidBSnapshot()); ui.sceneItems.value = [{ id: 42, item_key: 'plant', x: 600, y: 600 }]
  resolve(response({ items: [{ id: 10, item_key: 'yarn', x: 100, y: 600 }] })); await ui.flush()
  assert.equal(ui.sceneItems.value[0].id, 42); assert.equal(ui.kidID.value, '8')
  assert.equal(ui.requests.filter(r => r.url.endsWith('/scene/place')).length, 1); ui.cleanup()
})
test('4猫追逐只邀请2只，下一次轮到未参与猫，不占用球或产生结算', async () => {
  const ui = socialRoom(4); ui.sceneItems.value = []
  assert.equal(ui.startChase(true), true); const first = plain(ui.socialSession.value.activeIDs)
  ui.cancelSocial(false); for (const actor of ui.actors.values()) actor.cancel()
  assert.equal(ui.startChase(true), true); const next = plain(ui.socialSession.value.activeIDs)
  assert.equal(new Set([...first, ...next]).size, 4); assert.equal(next.length, 2)
  ui.cleanup(); assert.equal(ui.requests.length, 0)
})
test('只有成功启动追逐才收起陪伴/HUD/抽屉/游戏列表，结束默认保持收起', async () => {
  const ui = socialRoom(); ui.sceneItems.value = []; ui.isNarrow.value = true
  ui.toolsOpen.value = true; ui.hudOpen.value = true; ui.sheet.value = 'task'; ui.gamePicker.value = true
  assert.equal(ui.startGame('chase'), true)
  const session = ui.socialSession.value; assert.ok(session)
  assert.equal(ui.toolsOpen.value, false); assert.equal(ui.hudOpen.value, false); assert.equal(ui.sheet.value, ''); assert.equal(ui.gamePicker.value, false)
  for (let i = 0; i < 120 && ui.socialSession.value; i++) {
    await ui.advance(100)
    assert.equal(ui.toolsOpen.value, false); assert.equal(ui.hudOpen.value, false); assert.equal(ui.sheet.value, '')
  }
  assert.equal(ui.socialSession.value, null); assert.equal(session.round, 1)
  assert.equal(ui.toolsOpen.value, false); assert.equal(ui.gamePicker.value, false); ui.cleanup()
})
for (const reason of ['no-partner', 'no-path', 'busy']) {
  test(`追逐${reason}失败不隐藏原陪伴/HUD/抽屉/游戏列表`, () => {
    const ui = socialRoom(); ui.toolsOpen.value = true; ui.hudOpen.value = true; ui.sheet.value = 'task'; ui.gamePicker.value = true
    if (reason === 'no-partner') ui.actors.get('2').catAnim.value = 'sleep'
    else if (reason === 'no-path') ui.sceneItems.value = [{ id: 44, item_key: 'nest', x: 704, y: 590.4 }]
    else ui.playOpening.value = true
    assert.equal(ui.startGame('chase'), false); assert.equal(ui.socialSession.value, null)
    assert.equal(ui.toolsOpen.value, true); assert.equal(ui.hudOpen.value, true); assert.equal(ui.sheet.value, 'task'); assert.equal(ui.gamePicker.value, true)
    ui.cleanup()
  })
}
test('短追逐位于和猫玩列表，不增加手机导航；两段追后交换、停下回头并自然结束', async () => {
  const ui = socialRoom(); ui.isNarrow.value = true; ui.openGamePicker(ui.trigger); await ui.flush()
  assert.ok(nodes(ui.render()).some(n => n.props?.['data-room-action'] === 'chase'))
  assert.equal(nodes(ui.render()).find(n => n.props?.class === 'sheet-nav').children.length, 4)
  assert.equal(ui.startGame('chase'), true); assert.equal(ui.playOpen.value, false); assert.equal(ui.gamePicker.value, false)
  const session = ui.socialSession.value, stages = new Set(), roles = new Set()
  for (let i = 0; i < 150; i++) {
    await ui.advance(100)
    if (!ui.socialSession.value) break
    stages.add(session.stage); roles.add(session.roles.join(','))
    const [a, b] = session.activeIDs.map(id => ui.actors.get(id).catPos)
    assert.ok(Math.hypot(a.x - b.x, a.y - b.y) >= 110)
    assert.ok([a, b].every(p => p.x >= 70 && p.x <= 1190 && p.y >= 446.4 && p.y <= 700))
  }
  assert.equal(ui.socialSession.value, null); assert.equal(session.round, 1)
  assert.ok(stages.has('chase') && stages.has('lookback') && stages.has('exchange')); assert.equal(roles.size, 2)
  assert.equal(ui.requests.length, 0); assert.equal(ui.pet.value.balance, 70); ui.cleanup()
})
test('自动追逐有冷却且占用/睡眠/弱猫不能邀请；只切猫不会打断，点涉猫会恢复伙伴', async () => {
  const ui = socialRoom(3); const one = ui.actors.get('1'), two = ui.actors.get('2'), three = ui.actors.get('3')
  three.catAnim.value = 'sleep'; ui.random(.03); one.behaviorTick()
  const session = ui.socialSession.value; assert.equal(session.kind, 'chase'); assert.deepEqual(plain(session.activeIDs), ['1', '2'])
  ui.selectCat(2); assert.equal(ui.socialSession.value, session)
  ui.clickCat(Vue.reactive(two)); await ui.flush(); assert.equal(ui.socialSession.value, null); assert.equal(one.socialRole.value, ''); assert.equal(three.catAnim.value, 'sleep')
  ui.sceneItems.value = [] // 隔离追逐冷却，不把另一个有效的毛球邀请误当追逐重启。
  one.cancel(); two.cancel(); one.behaviorTick(); assert.equal(ui.socialSession.value, null)
  ui.pets.value[0].hunger = 20; two.catAnim.value = 'sleep'; assert.equal(ui.startChase(true), false); assert.equal(three.catAnim.value, 'sleep')
  ui.cleanup()
})
for (const boundary of ['hidden', 'unmount', 'meal', 'kid']) {
  test(`短追逐${boundary}取消全部epoch和计时，不恢复旧会话`, async () => {
    const ui = socialRoom(); ui.startChase(true); await ui.advance(1000)
    const stale = [...ui.frames.values()]
    if (boundary === 'hidden') { ui.document.hidden = true; ui.onDocumentVisibility() }
    else if (boundary === 'unmount') ui.cleanup()
    else if (boundary === 'meal') await ui.feed()
    else ui.applyPet(kidBSnapshot())
    for (const frame of stale) frame(10000)
    await ui.advance(1000); assert.equal(ui.socialSession.value, null)
    if (['hidden', 'unmount'].includes(boundary)) { assert.equal(ui.frames.size, 0); assert.equal(ui.timers.size, 0) }
    if (boundary !== 'unmount') ui.cleanup()
  })
}
test('取消开饭动画解除 meal framing，下一次 selected walkTo 不再被抑制', async () => {
  const ui = harness(); ui.seedMulti(); ui.roomEl.value.clientWidth = 292
  await ui.feed(); ui.respond(() => response(multiSnapshot({ fed: true }))); await ui.placeBowlAndGo(720, 600)
  await ui.advance(1600); ui.cancelFeeding()
  assert.equal(ui.mealGroupFraming.value, false)
  assert.equal(ui.cam.value.x, 146 - ui.catPos.x)
  await ui.actors.get('1').walkTo({ x: 700, y: 600 })
  assert.equal(ui.cam.value.x, 146 - 700)
  assert.equal(ui.pet.value.balance, 60)
  ui.cleanup()
})