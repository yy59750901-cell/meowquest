import { decorArt } from './scene-art.js'

const CATEGORY_LABELS = { furniture: '家具', toy: '玩具', decor: '装饰' }

const SHOP_DISPLAY = {
  'nest-apple': { category: 'furniture', icon: '/art/nest-apple-thumb.png', description: '按原照片重画的切面苹果窝，小猫会钻进去安稳趴睡。' },
  nest: { category: 'furniture', description: '暖色猫窝，为房间布置一个舒适的小角落。' },
  'nest-lace-square': { category: 'furniture', icon: '/art/nests/lace-square-thumb.png', description: '小猫会小跳一下越过前沿，落进软垫安稳趴睡。' },
  'nest-tent': { category: 'furniture', icon: '/art/nests/tent-thumb.png', description: '小猫会低跃钻进洞口，被窝沿挡住身体，只露出小脑袋趴睡。' },
  'nest-petal-cylinder': { category: 'furniture', icon: '/art/nests/petal-cylinder-thumb.png', description: '小猫会低跃钻进洞口，被窝沿挡住身体，只露出小脑袋趴睡。' },
  'nest-stripe-handle': { category: 'furniture', icon: '/art/nests/stripe-handle-thumb.png', description: '小猫会小跳一下越过前沿，落进软垫安稳趴睡。' },
  'nest-knit-drop': { category: 'furniture', icon: '/art/nests/knit-drop-thumb.png', description: '小猫会低跃钻进洞口，被窝沿挡住身体，只露出小脑袋趴睡。' },
  'nest-dot-u': { category: 'furniture', icon: '/art/nests/dot-U-thumb.png', description: '小猫会小跳一下越过前沿，落进软垫安稳趴睡。' },
  'nest-quilt-u': { category: 'furniture', icon: '/art/nests/quilt-U-thumb.png', description: '小猫会小跳一下越过前沿，落进软垫安稳趴睡。' },
  'nest-greyblue-round': { category: 'furniture', icon: '/art/nests/greyblue-round-thumb.png', description: '小猫会小跳一下越过前沿，落进软垫安稳趴睡。' },
  'nest-leaf-apple': { category: 'furniture', icon: '/art/nests/leaf-apple-thumb.png', description: '小猫会低跃钻进洞口，被窝沿挡住身体，只露出小脑袋趴睡。' },
  'nest-banana': { category: 'furniture', icon: '/art/nests/banana-thumb.png', description: '小猫会小跳一下越过前沿，落进软垫安稳趴睡。' },
  'nest-catear': { category: 'furniture', icon: '/art/nests/catear-thumb.png', description: '小猫会低跃钻进洞口，被窝沿挡住身体，只露出小脑袋趴睡。' },
  'nest-house': { category: 'furniture', icon: '/art/nests/house-thumb.png', description: '小猫会跨进半开放软垫，安稳趴睡。' },
  'nest-blue-yellow': { category: 'furniture', icon: '/art/nests/blue-yellow-thumb.png', description: '小猫会跨进半开放软垫，安稳趴睡。' },
  'nest-red-dots': { category: 'furniture', icon: '/art/nests/red-dots-thumb.png', description: '小猫会低跃钻进洞口，被窝沿挡住身体，只露出小脑袋趴睡。' },
  'nest-green-monster': { category: 'furniture', icon: '/art/nests/green-monster-thumb.png', description: '小猫会低跃钻进洞口，被窝沿挡住身体，只露出小脑袋趴睡。' },
  yarn: { category: 'toy', description: '柔软的紫色毛线球，给小猫的房间添一件玩具。' },
  plant: { category: 'decor', description: '红陶盆与绿叶，让房间多一点自然的颜色。' },
  fish: { category: 'decor', description: '挂在墙上的小鱼画，为房间添一处像素装饰。' },
  'cat-tree': {
    name: '猫爬架',
    category: 'furniture',
    description: '小猫会自主爬架、抓柱、钻窝和登高。',
    unlockBadge: '购买后解锁家具互动和无限爬高游戏',
    ownedHint: '无限爬高已加入「和猫玩」；摆进房间后，点不同位置还有不同反应。',
  },
  'wall-default': { name: '原装白墙', category: 'decor', description: '房间自带的温暖白墙，可随时换回，不耗星星。' },
  'wp-pink': { category: 'decor', description: '柔粉像素壁纸，替换房间整面墙的颜色与纹理。' },
  'wp-blue': { category: 'decor', description: '雾蓝像素壁纸，替换房间整面墙的颜色与纹理。' },
  'wp-mint': { category: 'decor', description: '薄荷像素壁纸，替换房间整面墙的颜色与纹理。' },
}

export function getShopDisplay(itemOrKey) {
  const item = typeof itemOrKey === 'string' ? { key: itemOrKey } : (itemOrKey || {})
  const key = String(item.key ?? '')
  const art = decorArt(key)
  const config = Object.hasOwn(SHOP_DISPLAY, key) ? SHOP_DISPLAY[key] : {}
  const metadata = item.metadata || {}
  const wallpaper = Boolean(item.builtin) || key === 'wall-default' || key.startsWith('wp-')
    || item.type === 'wallpaper' || metadata.surface === 'wallpaper' || art.surface === 'wallpaper'
  const fallbackCategory = wallpaper || item.type === 'decor' || item.type === 'wall'
    ? 'decor' : item.type === 'toy' ? 'toy' : 'furniture'
  const category = config.category || (Object.hasOwn(CATEGORY_LABELS, metadata.category) ? metadata.category : fallbackCategory)

  return {
    ...config,
    ...metadata,
    name: item.name || metadata.name || config.name || art.name,
    icon: item.icon || metadata.icon || config.icon || key,
    description: item.description || metadata.description || config.description
      || (wallpaper ? '替换房间墙面的装饰，可在商店中选择使用。' : '解锁后可用来布置房间。'),
    category,
    categoryLabel: CATEGORY_LABELS[category],
    surface: wallpaper ? 'wallpaper' : (metadata.surface || art.surface),
  }
}

export function filterShopItems(items, { query = '', category = 'all', ownedOnly = false } = {}) {
  const search = String(query ?? '').trim().toLowerCase()
  return items.filter((item) => {
    const display = getShopDisplay(item)
    return (category === 'all' || display.category === category)
      && (!ownedOnly || Boolean(item.owned) || Boolean(item.builtin))
      && (!search || String(display.name).toLowerCase().includes(search))
  })
}
