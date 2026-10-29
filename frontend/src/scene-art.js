const shanghaiClock = new Intl.DateTimeFormat('zh-CN', {
  timeZone: 'Asia/Shanghai',
  hour: '2-digit',
  minute: '2-digit',
  hourCycle: 'h23',
})

function smoothstep(start, end, value) {
  const t = Math.max(0, Math.min(1, (value - start) / (end - start)))
  return t * t * (3 - 2 * t)
}

export function getSceneLighting(date = new Date()) {
  const parts = shanghaiClock.formatToParts(date)
  const hour = Number(parts.find((part) => part.type === 'hour').value)
  const minute = Number(parts.find((part) => part.type === 'minute').value)
  const hours = hour + minute / 60
  const time = `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`

  let night = 0
  let dusk = 0
  let label = '日间'

  if (hours < 6 || hours >= 20) {
    night = 1
    label = '夜间'
  } else if (hours < 8) {
    night = 1 - smoothstep(6, 8, hours)
    label = '清晨'
  } else if (hours >= 17) {
    night = smoothstep(18.5, 20, hours)
    dusk = smoothstep(17, 18.5, hours) * (1 - night)
    label = '傍晚'
  }

  return { night, dusk, label, time }
}

// 注册新装饰只需追加原始 key，不改已有存储结构。
export const DECOR_ART = {
  nest: { name: '暖橘猫窝', size: 96, surface: 'floor' },
  yarn: { name: '紫色毛线球', size: 72, surface: 'floor' },
  plant: { name: '红陶盆栽', size: 96, surface: 'floor' },
  fish: { name: '小鱼挂画', size: 84, surface: 'wall' },
  'wp-pink': { name: '柔粉壁纸', size: 72, surface: 'wallpaper', wallColor: '#edccd0', pattern: '/art/wp-pink.png' },
  'wp-blue': { name: '雾蓝壁纸', size: 72, surface: 'wallpaper', wallColor: '#c6d8de', pattern: '/art/wp-blue.png' },
  'wp-mint': { name: '薄荷壁纸', size: 72, surface: 'wallpaper', wallColor: '#c5d7b5', pattern: '/art/wp-mint.png' },
}

// 未贴壁纸时的白墙：保留原 plaster 观感，纹理不需要明显。
const DEFAULT_WALL = { pattern: '/art/plaster.png', color: '#fffaf0', tile: 512 }
// 贴了壁纸后整层替换成壁纸自己的贴图，不再与白墙叠加（叠加会被米黄吃掉颜色）。
const WALLPAPER_TILE = 384

// 壁纸贴图出图时已把颜色乘进去（见 tools/gen_wallpaper.py），不做运行时混合：
// 依赖 background-blend-mode 时，一旦 blend 未生效，近白的图案会盖住颜色变成白墙。
// backgroundColor 只是图案没加载出来前的兜底色。
export function wallStyle(key, tile) {
  const art = decorArt(key)
  const isPaper = Boolean(art.pattern)
  return {
    backgroundImage: `url('${art.pattern || DEFAULT_WALL.pattern}')`,
    backgroundColor: art.wallColor || DEFAULT_WALL.color,
    backgroundSize: `${tile || (isPaper ? WALLPAPER_TILE : DEFAULT_WALL.tile)}px`,
  }
}

export function decorArt(key) {
  if (Object.prototype.hasOwnProperty.call(DECOR_ART, key)) return DECOR_ART[key]
  return { name: '像素装饰', size: 72, surface: 'floor' }
}

export function isWallpaper(key) {
  return (typeof key === 'string' && key.startsWith('wp-'))
    || decorArt(key).surface === 'wallpaper'
}
