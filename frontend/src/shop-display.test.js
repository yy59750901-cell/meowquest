import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'
import { parse, compileScript, compileTemplate, compileStyle } from '@vue/compiler-sfc'
import * as Vue from 'vue'
import { wallStyle } from './scene-art.js'
import { getShopDisplay, filterShopItems } from './shop-display.js'

const items = [
  { key: 'nest', name: '暖橘猫窝', price: 30, type: 'furniture', owned: true },
  { key: 'yarn', name: '紫色毛线球', price: 15, type: 'furniture', owned: false },
  { key: 'cat-tree', name: '猫爬架', price: 50, type: 'furniture', owned: false },
  { key: 'plant', name: '红陶盆栽', price: 20, type: 'furniture', owned: true },
  { key: 'fish', name: '小鱼挂画', price: 12, type: 'furniture', owned: false },
  { key: 'wall-default', name: '原装白墙', price: 0, builtin: true, owned: true },
  { key: 'wp-blue', name: '雾蓝壁纸', price: 25, type: 'wallpaper', owned: false },
]
const keys = (rows) => rows.map((item) => item.key)

test('字符串与对象统一展示，既有通用 furniture 类型按商品细分', () => {
  for (const [key, category] of Object.entries({
    nest: 'furniture', yarn: 'toy', 'cat-tree': 'furniture', plant: 'decor', fish: 'decor',
    'wall-default': 'decor', 'wp-pink': 'decor', 'wp-blue': 'decor', 'wp-mint': 'decor',
  })) {
    assert.equal(getShopDisplay(key).category, category)
    assert.equal(getShopDisplay({ key, type: 'furniture' }).category, category)
    assert.ok(getShopDisplay(key).description)
  }
  assert.equal(getShopDisplay('yarn').icon, 'yarn')
  assert.equal(getShopDisplay('wall-default').name, '原装白墙')
})

test('猫爬架售前功能明确，售后入口只指向房间', () => {
  const display = getShopDisplay('cat-tree')
  for (const word of ['自主爬架', '抓柱', '钻窝', '登高']) assert.ok(display.description.includes(word))
  assert.equal(display.unlockBadge, '购买后解锁家具互动和无限爬高游戏')
  assert.equal(display.ownedHint, '无限爬高已加入「和猫玩」；摆进房间后，点不同位置还有不同反应。')
  assert.equal(Object.hasOwn(display, 'price'), false)
})

test('未知 key、未知类型和未来壁纸保留在全部及正确的兜底分类中', () => {
  const future = [
    { key: 'future-paper', type: 'wallpaper' },
    { key: 'wp-future', type: 'new-type' },
    { key: 'future-wall', type: 'wall' },
    { key: 'future-decor', type: 'decor' },
    { key: 'future-toy', type: 'toy' },
    { key: 'future-chair', type: 'unregistered' },
    { key: 'future-plain' },
  ]
  assert.deepEqual(filterShopItems(future), future)
  assert.deepEqual(keys(filterShopItems(future, { category: 'decor' })), ['future-paper', 'wp-future', 'future-wall', 'future-decor'])
  assert.deepEqual(keys(filterShopItems(future, { category: 'toy' })), ['future-toy'])
  assert.deepEqual(keys(filterShopItems(future, { category: 'furniture' })), ['future-chair', 'future-plain'])
  assert.equal(getShopDisplay(future[0]).surface, 'wallpaper')
  assert.equal(getShopDisplay('toString').category, 'furniture')
  assert.equal(getShopDisplay('__proto__').category, 'furniture')
})

test('未来 metadata 与 icon 不丢失，名称与描述可直接传入', () => {
  const metadata = Object.freeze({ category: 'toy', description: '新玩具的实际说明', icon: 'yarn', future: { mode: 'custom' } })
  const item = { key: 'future-item', name: '新玩具', metadata }
  const display = getShopDisplay(item)
  assert.equal(display.name, '新玩具')
  assert.equal(display.category, 'toy')
  assert.equal(display.description, metadata.description)
  assert.equal(display.icon, 'yarn')
  assert.equal(display.future, metadata.future)
  assert.equal(getShopDisplay({ ...item, description: '主调用方说明', icon: 'nest' }).description, '主调用方说明')
  assert.equal(getShopDisplay({ ...item, icon: 'nest' }).icon, 'nest')
  assert.equal(getShopDisplay({ key: 'future', metadata: { category: 'future-category' } }).category, 'furniture')
})

test('中文名称搜索、空白搜索与英文大小写，不把描述当名称命中', () => {
  assert.deepEqual(keys(filterShopItems(items, { query: '  猫  ' })), ['nest', 'cat-tree'])
  assert.deepEqual(keys(filterShopItems(items, { query: '毛线' })), ['yarn'])
  assert.deepEqual(filterShopItems(items, { query: '   ' }), items)
  assert.deepEqual(filterShopItems(items, { query: null }), items)
  const english = [{ key: 'new-toy', name: 'Cat TOY', metadata: { description: '独特描述词' } }]
  assert.deepEqual(filterShopItems(english, { query: ' cAt toy ' }), english)
  assert.deepEqual(filterShopItems(english, { query: '独特描述词' }), [])
  assert.deepEqual(filterShopItems(items, { query: '不存在的名称' }), [])
})

test('分类、名称及已购过滤求交集，自带白墙按已拥有处理', () => {
  assert.deepEqual(keys(filterShopItems(items, { ownedOnly: true })), ['nest', 'plant', 'wall-default'])
  assert.deepEqual(keys(filterShopItems(items, { category: 'decor', ownedOnly: true })), ['plant', 'wall-default'])
  assert.deepEqual(keys(filterShopItems(items, { query: '猫', category: 'furniture', ownedOnly: true })), ['nest'])
  assert.deepEqual(filterShopItems(items, { category: 'toy', ownedOnly: true }), [])
  const builtin = { key: 'wall-default', builtin: true, owned: false, price: 0 }
  assert.deepEqual(filterShopItems([builtin], { category: 'decor', ownedOnly: true }), [builtin])
  assert.equal(getShopDisplay(builtin).surface, 'wallpaper')
  assert.deepEqual(filterShopItems([], { query: '猫' }), [])
})

test('过滤不排序、不修改输入，不丢失未知字段、原对象及后端价格', () => {
  const source = Object.freeze(items.map((item) => Object.freeze({ ...item, metadata: Object.freeze({ future: '保留' }), extra: 7 })))
  const before = JSON.stringify(source)
  const result = filterShopItems(source, { category: 'decor' })
  assert.notEqual(filterShopItems(source), source)
  for (const item of result) {
    assert.equal(item, source.find((candidate) => candidate.key === item.key))
    assert.equal(item.extra, 7)
    assert.equal(getShopDisplay(item).future, '保留')
  }
  assert.equal(JSON.stringify(source), before)
  for (const price of [0, 1, 73]) {
    const priced = { key: 'cat-tree', name: '猫爬架', price }
    assert.equal(filterShopItems([priced])[0].price, price)
    assert.equal(Object.hasOwn(getShopDisplay(priced), 'price'), false)
  }
})

const filename = new URL('./components/PixelShop.vue', import.meta.url)
const source = readFileSync(filename, 'utf8')
const parsed = parse(source, { filename: filename.pathname })
const script = compileScript(parsed.descriptor, { id: 'pixel-shop-test' })
const template = compileTemplate({
  source: parsed.descriptor.template.content,
  filename: filename.pathname,
  id: 'pixel-shop-test',
  compilerOptions: { bindingMetadata: script.bindings },
})

// 执行真实 setup 与模板；不挂载 DOM，v-model 只验证生成的更新回调。
function shopHost(initialItems = items) {
  const props = Vue.shallowReactive({ items: initialItems, balance: 81, busy: false })
  const emitted = []
  const context = { ...Vue, Vue: { ...Vue, withDirectives: (node) => node }, PixelItem: {}, wallStyle, getShopDisplay, filterShopItems }
  runInNewContext(script.content.replace(/^import[^\n]*\n/gm, '').replace('export default', 'globalThis.component ='), context)
  runInNewContext(template.code
    .replace(/^import \{ (.*) \} from "vue"\n/gm, (_, bindings) => `const { ${bindings.replace(/ as /g, ': ')} } = Vue\n`)
    .replace('export function render', 'globalThis.render = function render'), context)
  const api = context.component.setup(props, { expose() {}, emit: (...args) => emitted.push(args) })
  const setup = Vue.proxyRefs(api)
  return { props, api, emitted, render: () => context.render({}, [], props, setup, {}, {}) }
}

function nodes(node, predicate) {
  if (Array.isArray(node)) return node.flatMap((child) => nodes(child, predicate))
  if (!node || typeof node !== 'object') return []
  return [...(predicate(node) ? [node] : []), ...nodes(node.children, predicate)]
}
const byClass = (node, name) => nodes(node, (child) => typeof child.props?.class === 'string' && child.props.class.split(' ').includes(name))
function text(node) {
  if (typeof node === 'string') return node
  if (Array.isArray(node)) return node.map(text).join('')
  return node && typeof node === 'object' ? text(node.children) : ''
}

test('真实模板展示 1 星星后端价与原按钮文案，action 原样传商品', () => {
  const item = Object.freeze({ key: 'cat-tree', name: '名字很长也应完整显示的猫爬架', price: 1, owned: false, actionLabel: '购买', future: '保留' })
  const host = shopHost([item])
  const tree = host.render()
  assert.equal(text(byClass(tree, 'shop-price')[0]), '1 星星')
  assert.equal(text(byClass(tree, 'shop-item-name')[0]), item.name)
  assert.ok(text(tree).includes('购买后解锁家具互动和无限爬高游戏'))
  const button = byClass(tree, 'shop-action')[0]
  assert.equal(text(button), '购买')
  button.props.onClick()
  assert.equal(host.emitted.length, 1)
  assert.equal(host.emitted[0][0], 'action')
  assert.equal(host.emitted[0][1], item)
  host.props.items = [{ ...item, price: 73, owned: true, placed: true, actionLabel: '收回' }]
  const ownedTree = host.render()
  assert.equal(text(byClass(ownedTree, 'shop-price')[0]), '73 星星')
  assert.equal(text(byClass(ownedTree, 'shop-action')[0]), '收回')
  assert.ok(text(ownedTree).includes('已解锁 · 反复玩不耗星星'))
  assert.ok(text(ownedTree).includes('无限爬高已加入'))
  assert.ok(text(ownedTree).includes('已摆放在房间'))
  assert.equal(text(ownedTree).includes('购买后解锁家具互动'), false)
})

test('余额、busy 与 using 都来自实时 props，自带白墙显示免费', () => {
  const host = shopHost([{ key: 'wall-default', name: '原装白墙', price: 0, builtin: true, owned: true, using: false, actionLabel: '使用' }])
  assert.ok(text(byClass(host.render(), 'shop-balance')[0]).includes('81'))
  assert.equal(text(byClass(host.render(), 'shop-price')[0]), '免费')
  assert.equal(byClass(host.render(), 'shop-action')[0].props.disabled, false)
  host.props.balance = 1
  host.props.busy = true
  assert.ok(text(byClass(host.render(), 'shop-balance')[0]).includes('1'))
  assert.equal(byClass(host.render(), 'shop-products')[0].props['aria-busy'], true)
  assert.equal(byClass(host.render(), 'shop-action')[0].props.disabled, true)
  host.props.busy = false
  host.props.items = [{ ...host.props.items[0], using: true, actionLabel: '使用中' }]
  assert.equal(byClass(host.render(), 'shop-action')[0].props.disabled, true)
  assert.equal(text(byClass(host.render(), 'shop-state')[0]).trim(), '使用中')
})

test('真实筛选控件可组合，名称、类别、已购与商店空状态明确', () => {
  const host = shopHost()
  const tree = host.render()
  const search = byClass(tree, 'shop-search')[0]
  assert.equal(search.props.type, 'search')
  assert.equal(search.props['aria-label'], '搜索商品名称')
  search.props['onUpdate:modelValue']('毛线')
  assert.equal(host.api.query.value, '毛线')
  assert.deepEqual(keys(host.api.products.value.map(({ item }) => item)), ['yarn'])
  search.props['onUpdate:modelValue']('')
  const tabs = byClass(tree, 'shop-category')
  assert.deepEqual(tabs.map(text), ['全部', '家具', '玩具', '装饰'])
  tabs[2].props.onClick()
  assert.equal(host.api.category.value, 'toy')
  assert.deepEqual(keys(host.api.products.value.map(({ item }) => item)), ['yarn'])
  byClass(host.render(), 'shop-owned-filter')[0].props.onClick()
  assert.equal(byClass(host.render(), 'shop-owned-filter')[0].props['aria-pressed'], true)
  assert.ok(text(host.render()).includes('这个类别暂无已购商品'))
  host.api.ownedOnly.value = false
  host.props.items = []
  assert.ok(text(host.render()).includes('这个类别暂无商品'))
  host.api.query.value = '不存在'
  assert.ok(text(host.render()).includes('没有找到这个名称的商品'))
  host.api.query.value = ''
  host.api.category.value = 'all'
  assert.ok(text(host.render()).includes('商店暂无商品'))
})

test('Vue 脚本模板样式编译通过，只保留商品区滚动与完整名称', () => {
  assert.deepEqual(parsed.errors, [])
  assert.deepEqual(template.errors, [])
  const style = parsed.descriptor.styles[0].content
  assert.deepEqual(compileStyle({ source: style, filename: filename.pathname, id: 'pixel-shop-test', scoped: true }).errors, [])
  assert.equal((style.match(/overflow-y:\s*auto/g) || []).length, 1)
  assert.match(style, /\.pixel-shop\s*\{[^}]*flex-direction: column;[^}]*height: 100%;[^}]*min-height: 0;/s)
  assert.match(style, /\.shop-toolbar\s*\{[^}]*flex: 0 0 auto;/s)
  assert.match(style, /\.shop-products\s*\{[^}]*flex: 1 1 auto;[^}]*min-height: 0;[^}]*overflow-y: auto;/s)
  assert.match(style, /\.shop-item-name\s*\{[^}]*overflow-wrap: anywhere;/s)
  assert.match(style, /\.shop-action\s*\{[^}]*min-height: 44px;/s)
  assert.equal(/text-overflow|line-clamp|\b(?:dvh|vh)\b/.test(style), false)
  assert.equal(/<slot|localStorage|sessionStorage|axios|fetch\(|router|50\s*星星/.test(source), false)
})
