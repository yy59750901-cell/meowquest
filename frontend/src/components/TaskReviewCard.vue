<script setup>
import { computed } from 'vue'
import EvidenceGallery from './EvidenceGallery.vue'
import { reviewResult, reviewLabel, reviewPending, reviewElapsed, reviewStages } from '../ai-review.js'
const props = defineProps({ item: { type: Object, required: true }, expanded: Boolean, history: Boolean, busy: Boolean, now: Number })
const emit = defineEmits(['toggle', 'approve', 'reject', 'retry'])
const snap = computed(() => { try { return typeof props.item.snapshot === 'string' ? JSON.parse(props.item.snapshot) : props.item.snapshot || {} } catch { return {} } })
const result = computed(() => reviewResult(props.item))
const isAI = computed(() => (snap.value.confirm_mode || '').includes('b'))
const status = computed(() => props.item.ai_review)
const canRetry = computed(() => !props.history && isAI.value && (!status.value && !result.value || status.value?.status === 'failed'))
const count = computed(() => { try { const v = typeof props.item.evidence === 'string' ? JSON.parse(props.item.evidence) : props.item.evidence; return Array.isArray(v) ? v.length : 0 } catch { return 0 } })
const confirmLabel = computed(() => ({ approved: '家长已通过', rejected: props.item.reject_reason?.startsWith('AI：') ? 'AI 退回补交' : '家长已驳回', auto: '自动通过' }[props.item.confirm_status] || '待家长确认'))
</script>

<template>
  <article class="review-card">
    <button class="review-summary" :aria-expanded="expanded" @click="emit('toggle')">
      <span class="review-heading"><b>{{ item.kid_name }} · {{ snap.name }}</b><span class="expand-mark">{{ expanded ? '收起' : '展开' }}</span></span>
      <span class="review-meta">{{ Math.round((item.duration_sec || 0) / 60) }}分钟 · {{ count }}个材料<span v-if="history"> · {{ confirmLabel }}</span></span>
      <span class="review-date">提交于 {{ new Date(item.submitted_at).toLocaleString('zh-CN') }}</span>
      <span class="review-state" :class="{ failed: status?.status === 'failed', running: reviewPending(status) }">{{ reviewLabel(status, isAI) }}</span>
    </button>
    <div v-if="expanded" class="review-detail">
      <div class="review-report" :class="{ failed: status?.status === 'failed' || result && !result.pass }">
        <b>{{ result ? result.pass ? 'AI 初审通过' : 'AI 初审未通过' : reviewLabel(status, isAI) }}<span v-if="result?.score" class="score"> · {{ result.score }} / 5 星</span></b>
        <p v-if="reviewPending(status)">{{ reviewStages[status.stage] || '正在处理' }}<span v-if="reviewElapsed(status, now)"> · 已用 {{ reviewElapsed(status, now) }}</span>。可离开页面，后台继续处理全部材料。</p>
        <p v-if="status?.status === 'failed'">失败阶段：{{ reviewStages[status.stage] || status.stage }}<br />{{ status.error || '本次分析未完成，请重试或由家长审核。' }}</p>
        <p v-if="status?.status === 'canceled'">本次 AI 未形成评价，任务已由家长处理或提交已更新。</p>
        <p v-if="result">{{ result.reason }}</p>
        <p v-if="result?.kid_feedback" class="kid-feedback">给孩子的反馈：{{ result.kid_feedback }}</p>
        <small v-if="status?.attempt">第 {{ status.attempt }} 次执行<span v-if="status.finished_at"> · {{ new Date(status.finished_at).toLocaleString('zh-CN') }}</span></small>
        <button v-if="canRetry" :disabled="busy" class="retry-ai" @click="emit('retry')">{{ busy ? '提交中…' : '重试 AI 分析' }}</button>
      </div>
      <p v-if="!history && reviewPending(status)" class="early-warning">AI 尚未完成。你可以提前审核，但确认后将停止本次 AI 分析，不会再用其结果修改你的决定。</p>
      <p v-if="history && item.reject_reason" class="history-reason">退回原因：{{ item.reject_reason }}</p>
      <p v-if="history && item.confirmed_at" class="history-reason">处理于 {{ new Date(item.confirmed_at).toLocaleString('zh-CN') }}</p>
      <div v-if="!history" class="review-actions">
        <button class="approve" :disabled="busy" @click="emit('approve')">通过并结算</button><button class="reject" :disabled="busy" @click="emit('reject')">驳回补交</button>
      </div>
      <EvidenceGallery :evidence="item.evidence" />
      <p v-if="!count" class="history-reason">没有上传材料</p>
    </div>
  </article>
</template>

<style scoped>
.review-card { margin: 10px; padding: 14px; background: #fff; border: 1px solid #E5E2D9; border-radius: 12px; color: #444441; }
.review-summary { display: block; width: 100%; padding: 0; text-align: left; color: inherit; background: transparent; border: 0; cursor: pointer; font: inherit; }
.review-heading { display: flex; align-items: baseline; gap: 8px; }
.review-heading b { min-width: 0; font-size: 16px; overflow-wrap: anywhere; }
.expand-mark { margin-left: auto; font-size: 12px; color: #888780; flex-shrink: 0; }
.review-meta, .review-date, .review-state { display: block; margin-top: 6px; font-size: 12px; line-height: 1.5; }
.review-meta { color: #534AB7; }
.review-date { color: #6F6E68; }
.review-state { color: #0F6E56; }
.review-state.failed { color: #A32D2D; }
.review-state.running { color: #854F0B; }
.review-detail { padding-top: 8px; }
.review-report { padding: 10px 12px; background: #E1F5EE; border: 1px solid #9FE1CB; border-radius: 8px; font-size: 13px; overflow-wrap: anywhere; }
.review-report.failed { background: #FAEEDA; border-color: #FAC775; }
.review-report b { font-size: 14px; }
.review-report p { margin: 6px 0; font-size: 13px; line-height: 1.6; }
.review-report small { display: block; color: #6F6E68; font-size: 11px; margin-top: 5px; }
.score, .kid-feedback { color: #534AB7; }
.early-warning { color: #854F0B; background: #FAEEDA; padding: 10px; font-size: 12px; line-height: 1.6; border-radius: 8px; }
.history-reason { font-size: 12px; line-height: 1.5; color: #6F6E68; overflow-wrap: anywhere; }
.review-actions { display: flex; gap: 8px; margin: 12px 0; }
.review-actions button, .retry-ai { font: inherit; padding: 9px 12px; font-size: 13px; border: 1px solid #D3D1C7; border-radius: 8px; cursor: pointer; }
.review-actions button { flex: 1; }
.approve { color: #0F6E56; background: #E1F5EE; }
.reject { color: #A32D2D; background: #FCEBEB; }
.retry-ai { margin-top: 8px; color: #534AB7; background: #fff; }
button:disabled { opacity: .5; cursor: default; }
</style>
