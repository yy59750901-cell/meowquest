export const reviewStages = { queued: '等待领取', preparing: '准备材料', transcribing: '录音转写', reviewing: '图文分析', analyzing: '图文分析', finished: '分析完成', canceled: '已停止', legacy: '旧版本分析' }
export function reviewResult(item) {
  if (item.ai_review?.result) return item.ai_review.result
  try { return typeof item.review_result === 'string' ? JSON.parse(item.review_result || 'null') : item.review_result || null } catch { return null }
}
export function reviewLabel(review, isAI = false) {
  if (!review) return isAI ? 'AI 暂无结果，可重新分析' : '无需 AI 分析'
  return { queued: 'AI 排队中', running: 'AI 分析中', succeeded: review.result?.pass ? 'AI 初审通过' : 'AI 初审未通过', failed: 'AI 执行失败', canceled: 'AI 已停止' }[review.status] || 'AI 暂无结果'
}
export function reviewPending(review) { return ['queued', 'running'].includes(review?.status) }
export function reviewElapsed(review, now = Date.now()) {
  if (!review?.started_at) return ''
  const end = review.finished_at ? new Date(review.finished_at).getTime() : now
  const sec = Math.max(0, Math.floor((end - new Date(review.started_at).getTime()) / 1000))
  return Number.isFinite(sec) ? `${Math.floor(sec / 60)}分${sec % 60}秒` : ''
}
