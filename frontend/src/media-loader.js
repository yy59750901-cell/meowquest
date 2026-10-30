// 所有素材组件共享下载队列，展开多张图片也最多同时下载两张。
let active = 0
const queue = []
function drain() {
  while (active < 2 && queue.length) {
    const entry = queue.shift()
    if (entry.signal.aborted) { entry.reject(new DOMException('已取消', 'AbortError')); continue }
    active++
    entry.run().then(entry.resolve, entry.reject).finally(() => { active--; drain() })
  }
}
export function fetchMedia(url, signal, timeout = 45000) {
  return new Promise((resolve, reject) => {
    const entry = { signal, resolve, reject, run: async () => {
      const controller = new AbortController()
      const abort = () => controller.abort()
      signal.addEventListener('abort', abort, { once: true })
      const timer = setTimeout(abort, timeout)
      try {
        if (signal.aborted) throw new DOMException('已取消', 'AbortError')
        const response = await fetch(url, { signal: controller.signal })
        if (!response.ok) throw new Error(response.status === 415 ? '暂不支持此格式的缩略预览' : '图片加载失败，请重试')
        const blob = await response.blob()
        if (!blob.type.startsWith('image/')) throw new Error('不是可显示的图片')
        return blob
      } finally { clearTimeout(timer); signal.removeEventListener('abort', abort) }
    } }
    const cancelQueued = () => {
      const i = queue.indexOf(entry)
      if (i >= 0) { queue.splice(i, 1); reject(new DOMException('已取消', 'AbortError')) }
    }
    signal.addEventListener('abort', cancelQueued, { once: true })
    entry.resolve = value => { signal.removeEventListener('abort', cancelQueued); resolve(value) }
    entry.reject = error => { signal.removeEventListener('abort', cancelQueued); reject(error) }
    queue.push(entry); drain()
  })
}
export function thumbnailURL(path) { return '/api/media/thumbnail?path=' + encodeURIComponent(path) }
