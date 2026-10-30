// 家长配置和孩子向导共用；旧媒体组合转换为逐项材料要求。
export const evidenceLabels = { photo: '拍下完成情况', audio: '说说完成情况', video: '录下完成情况' }
export function evidenceSteps(raw, legacy = '') {
  let rows = raw == null || raw === '' ? [] : typeof raw === 'string' ? JSON.parse(raw) : raw
  if (!Array.isArray(rows)) throw new Error('提交步骤不是有效数组')
  // 旧版本把无步骤存成 []，仍从媒体选择恢复；新版清空会同时保存 evidence_type=none。
  if (!rows.length) rows = legacy.split(',').map(kind => kind.trim()).filter(kind => evidenceLabels[kind]).map(kind => ({ kind, required: true }))
  return rows.map(st => {
    if (!st || !evidenceLabels[st.kind] || (st.required != null && typeof st.required !== 'boolean')) throw new Error('提交步骤配置无效')
    return { kind: st.kind, label: String(st.label || '').trim() || evidenceLabels[st.kind], required: st.required !== false }
  })
}
