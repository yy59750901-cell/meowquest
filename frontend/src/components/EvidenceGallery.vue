<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { fetchMedia, thumbnailURL } from '../media-loader.js'
const props = defineProps({ evidence: { type: [String, Array], default: '[]' } })
const files = computed(() => {
  try { const rows = typeof props.evidence === 'string' ? JSON.parse(props.evidence) : props.evidence; return Array.isArray(rows) ? rows.filter(x => x?.path) : [] } catch { return [] }
})
const photos = computed(() => files.value.filter(f => f.type === 'photo'))
const otherMedia = computed(() => files.value.filter(f => f.type !== 'photo'))
const root = ref(null)
const previews = reactive({})
let observer = null
const controllers = new Set()
async function loadPreview(path) {
  if (previews[path]?.state === 'loading' || previews[path]?.state === 'ready') return
  const controller = new AbortController(); controllers.add(controller)
  previews[path] = { state: 'loading', src: '', error: '' }
  try {
    const blob = await fetchMedia(thumbnailURL(path), controller.signal)
    if (controller.signal.aborted) return
    previews[path] = { state: 'ready', src: URL.createObjectURL(blob), error: '' }
  } catch (error) {
    if (!controller.signal.aborted) previews[path] = { state: 'error', src: '', error: error.name === 'AbortError' ? '加载超时，请重试' : error.message }
  } finally { controllers.delete(controller) }
}
function previewError(path) {
  if (previews[path]?.src) URL.revokeObjectURL(previews[path].src)
  previews[path] = { state: 'error', src: '', error: '图片解码失败，可重试或查看原图' }
}
async function observePhotos() {
  observer?.disconnect()
  await nextTick()
  if (!root.value) return
  observer = new IntersectionObserver(entries => {
    for (const entry of entries) if (entry.isIntersecting) { observer.unobserve(entry.target); loadPreview(entry.target.dataset.path) }
  }, { rootMargin: '120px' })
  root.value.querySelectorAll('[data-path]').forEach(el => observer.observe(el))
}
const selected = ref(-1)
const original = reactive({ src: '', state: '', error: '' })
const zoomed = ref(false)
let originalController = null
let originalSequence = 0
const current = computed(() => photos.value[selected.value])
async function loadOriginal() {
  const sequence = ++originalSequence
  originalController?.abort()
  if (original.src) URL.revokeObjectURL(original.src)
  original.src = ''; original.state = 'loading'; original.error = ''; zoomed.value = false
  if (!current.value) return
  const controller = new AbortController(); originalController = controller
  try {
    const blob = await fetchMedia(current.value.path, controller.signal, 90000)
    if (controller.signal.aborted || sequence !== originalSequence) return
    original.src = URL.createObjectURL(blob); original.state = 'ready'
  } catch (error) {
    if (!controller.signal.aborted && sequence === originalSequence) { original.state = 'error'; original.error = error.name === 'AbortError' ? '原图加载超时' : error.message }
  }
}
function originalError() {
  if (original.src) URL.revokeObjectURL(original.src)
  original.src = ''; original.state = 'error'; original.error = '原图解码失败'
}
function openPhoto(i) { selected.value = i; loadOriginal() }
function closePhoto() {
  selected.value = -1; originalSequence++; originalController?.abort()
  if (original.src) URL.revokeObjectURL(original.src)
  original.src = ''
}
function onKey(e) { if (selected.value >= 0 && e.key === 'Escape') closePhoto() }
watch(() => props.evidence, () => { closePhoto(); observePhotos() })
onMounted(() => { observePhotos(); window.addEventListener('keydown', onKey) })
onBeforeUnmount(() => {
  observer?.disconnect(); controllers.forEach(c => c.abort()); closePhoto(); window.removeEventListener('keydown', onKey)
  Object.values(previews).forEach(p => { if (p.src) URL.revokeObjectURL(p.src) })
})
</script>

<template>
  <div ref="root" class="evidence-gallery" @click.stop>
    <div v-for="(file, i) in otherMedia" :key="file.path + i" class="media-line">
      <span>{{ file.type === 'audio' ? '录音' : '录像' }} {{ i + 1 }}</span>
      <audio v-if="file.type === 'audio'" :src="file.path" controls preload="none" />
      <video v-else-if="file.type === 'video'" :src="file.path" controls preload="none" playsinline />
    </div>
    <p v-if="photos.length" class="gallery-hint">{{ photos.length }} 张图片，点击查看大图；列表仅加载缩略图，AI 分析使用全部大图。</p>
    <div class="photo-grid">
      <div v-for="(file, i) in photos" :key="file.path + i" :data-path="file.path" class="photo-tile">
        <button v-if="previews[file.path]?.state === 'ready'" class="photo-open" @click="openPhoto(i)">
          <img :src="previews[file.path].src" :alt="'材料图片 ' + (i + 1)" decoding="async" @error="previewError(file.path)" />
          <span class="page-number">{{ i + 1 }}</span>
        </button>
        <div v-else-if="previews[file.path]?.state === 'error'" class="photo-placeholder failed">
          <span>图片 {{ i + 1 }}</span><small>{{ previews[file.path].error }}</small>
          <button @click="loadPreview(file.path)">重新加载</button><button @click="openPhoto(i)">查看原图</button>
        </div>
        <div v-else class="photo-placeholder"><span>图片 {{ i + 1 }}</span><small>{{ previews[file.path]?.state === 'loading' ? '加载预览中…' : '滚动到此处加载' }}</small></div>
      </div>
    </div>
    <Teleport to="body">
      <div v-if="selected >= 0" class="media-preview-mask" role="dialog" aria-modal="true" aria-label="材料大图预览" @click.stop>
        <div class="preview-toolbar"><b>图片 {{ selected + 1 }} / {{ photos.length }}</b><button @click="closePhoto">关闭</button></div>
        <div class="original-area" :class="{ zoomed }">
          <img v-if="original.src" :src="original.src" alt="材料原图" @click="zoomed = !zoomed" @error="originalError" />
          <div v-else class="original-status"><p>{{ original.state === 'error' ? original.error : '正在加载这张原图…' }}</p><button v-if="original.state === 'error'" @click="loadOriginal">重新加载</button></div>
        </div>
        <div class="preview-footer">
          <button :disabled="selected <= 0" @click="openPhoto(selected - 1)">上一张</button>
          <button :disabled="!original.src" @click="zoomed = !zoomed">{{ zoomed ? '适应屏幕' : '放大看细节' }}</button>
          <button :disabled="selected >= photos.length - 1" @click="openPhoto(selected + 1)">下一张</button>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.evidence-gallery { margin-top: 10px; cursor: default; }
.media-line { margin-bottom: 12px; font-size: 12px; color: #5F5E5A; }
.media-line audio, .media-line video { display: block; width: 100%; max-width: 100%; margin-top: 6px; }
.gallery-hint { font-size: 12px; color: #6F6E68; line-height: 1.5; margin: 8px 0; }
.photo-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
.photo-tile { min-width: 0; aspect-ratio: 4 / 3; background: #F1EFE8; border: 1px solid #D3D1C7; border-radius: 8px; overflow: hidden; }
.photo-open { position: relative; display: block; width: 100%; height: 100%; padding: 0; border: none; background: transparent; cursor: zoom-in; }
.photo-open img { width: 100%; height: 100%; object-fit: contain; display: block; }
.page-number { position: absolute; left: 5px; bottom: 5px; background: #fff; color: #444441; padding: 2px 6px; border-radius: 4px; font-size: 11px; }
.photo-placeholder { height: 100%; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 5px; padding: 8px; box-sizing: border-box; color: #6F6E68; font-size: 12px; text-align: center; }
.photo-placeholder small { font-size: 11px; line-height: 1.4; }
.photo-placeholder button { font: inherit; font-size: 11px; padding: 3px 8px; border: 1px solid #D3D1C7; border-radius: 5px; color: #534AB7; background: #fff; cursor: pointer; }
.failed { color: #A32D2D; }
.media-preview-mask { position: fixed; inset: 0; z-index: 2000; background: #FFFCF6; color: #444441; display: flex; flex-direction: column; padding: env(safe-area-inset-top, 0px) 0 env(safe-area-inset-bottom, 0px); box-sizing: border-box; }
.preview-toolbar, .preview-footer { display: flex; align-items: center; justify-content: space-between; gap: 8px; padding: 12px; flex-shrink: 0; border-bottom: 1px solid #D3D1C7; font-size: 14px; }
.preview-footer { border-bottom: 0; border-top: 1px solid #D3D1C7; }
.preview-toolbar button, .preview-footer button, .original-status button { font: inherit; font-size: 12px; padding: 8px 10px; background: #fff; color: #534AB7; border: 1px solid #D3D1C7; border-radius: 8px; cursor: pointer; }
button:disabled { opacity: .4; cursor: default; }
.original-area { min-height: 0; flex: 1; overflow: auto; display: flex; align-items: center; justify-content: center; touch-action: pan-x pan-y; }
.original-area img { max-width: 100%; max-height: 100%; object-fit: contain; cursor: zoom-in; }
.original-area.zoomed { display: block; }
.original-area.zoomed img { width: 1600px; max-width: none; max-height: none; cursor: zoom-out; }
.original-status { text-align: center; font-size: 13px; }
@media (max-width: 360px) { .photo-placeholder { gap: 3px; padding: 4px; } .photo-placeholder small { font-size: 10px; } }
</style>
