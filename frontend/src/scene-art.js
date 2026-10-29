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
  'wp-pink': { name: '柔粉壁纸', size: 72, surface: 'wallpaper', wallColor: '#edccd0' },
  'wp-blue': { name: '雾蓝壁纸', size: 72, surface: 'wallpaper', wallColor: '#c6d8de' },
  'wp-mint': { name: '薄荷壁纸', size: 72, surface: 'wallpaper', wallColor: '#c5d7b5' },
}

export function decorArt(key) {
  if (Object.prototype.hasOwnProperty.call(DECOR_ART, key)) return DECOR_ART[key]
  return { name: '像素装饰', size: 72, surface: 'floor' }
}

export function isWallpaper(key) {
  return (typeof key === 'string' && key.startsWith('wp-'))
    || decorArt(key).surface === 'wallpaper'
}
