import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'
import * as Vue from 'vue'
import { parse, compileScript, compileTemplate } from '@vue/compiler-sfc'
import * as tree from './cat-tree-room.js'
import * as interaction from './room-interaction.js'
import * as catalog from './nest-catalog.js'
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
  const host = prefix => ({ addEventListener: (key, fn) => listeners.set(`${prefix}:${key}`, fn), removeEventListener: key => listeners.delete(`${prefix}:${key}`) })
  const document = { hidden: false, activeElement: null, ...host('document'), body: { appendChild() {} }, createElement: () => ({ style: {}, appendChild() {}, remove() {} }) }
  const focusable = name => Vue.markRaw({ name, isConnected: true, focus() { document.activeElement = this }, getBoundingClientRect: () => ({ left: 350, top: 260, width: 192, height: 192 }), querySelector: () => null })
  const trigger = focusable('爬架'), close = focusable('关闭'), action = focusable('动作')
  const context = {
    ...tree, ...interaction, ...catalog, decorArt, getSceneLighting, isWallpaper, getShopDisplay, ref: Vue.ref, computed: Vue.computed, reactive: Vue.reactive, nextTick: Vue.nextTick,
    onMounted: fn => { hooks.mount = fn }, onBeforeUnmount: fn => { hooks.cleanup = fn }, watch: (get, fn) => watchers.push([get, fn]),
    showToast: text => toasts.push(text), closeToast() {}, showDialog() {}, showConfirmDialog: async () => {}, reviewPending: () => false, reviewStages: {}, evidenceSteps: () => [],
    document, window: { innerWidth: 1280, innerHeight: 720, matchMedia: () => ({ matches: false }), ...host('window') },
    localStorage: { setItem: (...args) => storage.push(args), removeItem: (...args) => storage.push(args), getItem: () => null },
    performance: { now: () => now }, Blob, FormData, URL,
    Math: Object.assign(Object.create(Math), { random: () => .9 }),
    navigator: { mediaDevices: { getUserMedia: async () => { throw new Error('未授权') } } },
    MediaRecorder: class { static isTypeSupported() { return true } start() {} stop() {} },
    ResizeObserver: class { observe() {} disconnect() {} },
    getComputedStyle: el => el.world ? { transform: 'none' } : { marginLeft: ui.treeRendered.value ? '-32px' : '-55px', marginTop: ui.treeRendered.value ? '-48px' : '-100px' },
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
  ui.catEl.value = { style: {}, offsetWidth: 110, getBoundingClientRect: () => ({ left: ui.catPos.x - (ui.treeRendered.value ? 32 : 55), top: ui.catPos.y - (ui.treeRendered.value ? 48 : 100) }), querySelector: () => null }
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
  assert.match(source, /@keydown\.enter\.prevent\.stop="it\.item_key === 'cat-tree' && playTree\(it, 'top'\)"/)
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

test('放碗走路 100ms 后隐藏再回来 10 秒，不发喂食请求、不留碗或 busy', async () => {
  const ui = harness()
  await ui.feed()
  ui.placeBowlAndGo(500, 600)
  await ui.advance(100)
  assert.equal(ui.showBowl.value, true)
  assert.equal(ui.gameBusy(), true)
  ui.document.hidden = true
  ui.onDocumentVisibility()
  assert.equal(ui.showBowl.value, false)
  assert.equal(ui.placingFeed.value, false)
  assert.equal(ui.gameBusy(), false)
  assert.equal(ui.timers.size, 0)
  assert.equal(ui.frames.size, 0)
  assert.match(ui.toasts.at(-1), /取消.*没有扣星星/)
  ui.document.hidden = false
  ui.onDocumentVisibility()
  await ui.advance(10000)
  assert.equal(ui.requests.filter(r => r.url === '/api/pet/feed').length, 0)
  assert.equal(ui.showBowl.value, false)
  assert.equal(ui.gameBusy(), false)
  assert.equal(ui.pet.value.balance, 70)
  await ui.feed()
  assert.equal(ui.placingFeed.value, true)
  ui.cleanup()
})

for (const boundary of ['hidden', 'unmount']) {
  test(`进食 2300ms timer 在 ${boundary} 时取消，旧 timer 不 POST、不创建 say timer`, async () => {
    const ui = harness()
    await ui.feed()
    ui.placeBowlAndGo(308, 600)
    await ui.advance(300)
    assert.equal(ui.catAnim.value, 'eat')
    assert.equal(ui.gameBusy(), true)
    const staleTimers = [...ui.timers.values()]
    if (boundary === 'hidden') { ui.document.hidden = true; ui.onDocumentVisibility() }
    else ui.cleanup()
    assert.equal(ui.showBowl.value, false)
    assert.equal(ui.gameBusy(), false)
    assert.equal(ui.timers.size, 0)
    // 主动执行已失效的队列回调，覆盖清计时外的 epoch/disposed 防线。
    for (const timer of staleTimers) await timer.fn()
    await ui.advance(10000)
    assert.equal(ui.requests.filter(r => r.url === '/api/pet/feed').length, 0)
    assert.equal(ui.timers.size, 0)
    if (boundary === 'hidden') ui.cleanup()
  })
}

test('取消后的旧喂食 timer 不抢新会话；新会话仍等待原 2300ms 再结算一次', async () => {
  const ui = harness()
  await ui.feed()
  ui.placeBowlAndGo(308, 600)
  await ui.advance(300)
  const oldTimers = [...ui.timers.values()]
  ui.document.hidden = true
  ui.onDocumentVisibility()
  ui.document.hidden = false
  ui.onDocumentVisibility()
  await ui.flush()
  await ui.feed()
  ui.placeBowlAndGo(308, 600)
  await ui.advance(300)
  for (const timer of oldTimers) await timer.fn()
  assert.equal(ui.showBowl.value, true)
  assert.equal(ui.gameBusy(), true)
  assert.equal(ui.requests.filter(r => r.url === '/api/pet/feed').length, 0)
  await ui.advance(2200)
  assert.equal(ui.requests.filter(r => r.url === '/api/pet/feed').length, 0)
  ui.respond(() => ({ ok: true, status: 200, json: async () => ({ pet: { balance: 60, hunger: 100 } }) }))
  await ui.advance(100)
  assert.equal(ui.requests.filter(r => r.url === '/api/pet/feed').length, 1)
  assert.equal(ui.pet.value.balance, 60)
  assert.equal(ui.showBowl.value, false)
  assert.equal(ui.gameBusy(), false)
  assert.equal(ui.catAnim.value, 'sit')
  assert.equal(ui.bubble.value, '真好吃！谢谢你！')
  ui.cleanup()
})

test('喂食 POST 抛异常也释放会话，可重新喂且不自动重复 POST', async () => {
  const ui = harness()
  ui.respond(() => { throw new Error('网络中断') })
  await ui.feed()
  ui.placeBowlAndGo(308, 600)
  await ui.advance(2700)
  assert.equal(ui.requests.filter(r => r.url === '/api/pet/feed').length, 1)
  assert.equal(ui.showBowl.value, false)
  assert.equal(ui.gameBusy(), false)
  assert.equal(ui.catAnim.value, 'sit')
  assert.match(ui.toasts.at(-1), /无法确认.*小猫状态/)
  assert.equal(ui.pet.value.balance, 70)
  await ui.feed()
  assert.equal(ui.placingFeed.value, true)
  ui.cleanup()
})

for (const outcome of ['success', 'failure']) {
  test(`POST 在途隐藏且回来时仍互斥，迟到 ${outcome} 不回滚、不重复结算`, async () => {
    const ui = harness()
    let resolvePost, rejectPost
    ui.respond(() => new Promise((resolve, reject) => { resolvePost = resolve; rejectPost = reject }))
    await ui.feed()
    ui.placeBowlAndGo(308, 600)
    await ui.advance(2700)
    assert.equal(ui.requests.filter(r => r.url === '/api/pet/feed').length, 1)
    ui.document.hidden = true
    ui.onDocumentVisibility()
    assert.equal(ui.showBowl.value, false)
    assert.equal(ui.gameBusy(), true)
    assert.match(ui.toasts.at(-1), /请求已发送/)
    await ui.feed()
    ui.placeBowlAndGo(400, 600)
    ui.document.hidden = false
    ui.onDocumentVisibility()
    await ui.advance(10000)
    await ui.feed()
    ui.placeBowlAndGo(400, 600)
    assert.equal(ui.requests.filter(r => r.url === '/api/pet/feed').length, 1)
    assert.equal(ui.placingFeed.value, false)
    if (outcome === 'success') resolvePost({ ok: true, status: 200, json: async () => ({ pet: { balance: 60, hunger: 100 } }) })
    else rejectPost(new Error('迟到断网'))
    await ui.flush()
    assert.equal(ui.gameBusy(), false)
    assert.equal(ui.showBowl.value, false)
    assert.equal(ui.pet.value.balance, 70)
    assert.equal(ui.showBubble.value, false)
    assert.equal(ui.timers.size, 0)
    assert.equal(ui.requests.filter(r => r.url === '/api/pet/feed').length, 1)
    // 结算以服务端为准，回前台的常规同步读到已扣余额，而非本地退款或补发。
    ui.respond(() => ({ ok: true, status: 200, json: async () => ({ pet: { balance: 60, hunger: 100 } }) }))
    await ui.refreshPet()
    assert.equal(ui.pet.value.balance, 60)
    await ui.feed()
    assert.equal(ui.placingFeed.value, true)
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
      for (const [, callback] of ui.watchers) callback(false)
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
