import { planActorSeats } from './room-interaction.js'

// 共享的是家具锁和账户投影，猫本身的字段只按身份合并。
export function createReservations() {
  const owners = new Map()
  return {
    owner: id => owners.get(String(id)),
    acquire(id, catID) {
      const key = String(id), owner = owners.get(key)
      if (owner !== undefined && owner !== String(catID)) return false
      owners.set(key, String(catID))
      return true
    },
    release(id, catID) {
      if (owners.get(String(id)) === String(catID)) owners.delete(String(id))
    },
    releaseCat(catID) { for (const [id, owner] of owners) if (owner === String(catID)) owners.delete(id) },
    clear() { owners.clear() },
  }
}
export function mergeSnapshot(snapshot, previous = [], requestedID = '') {
  if (!snapshot?.pet) return null
  const account = snapshot.account || snapshot.pet
  const growth = Object.fromEntries(['balance', 'earned_total', 'level', 'streak'].filter(k => account[k] !== undefined).map(k => [k, account[k]]))
  const incoming = Array.isArray(snapshot.pets) ? snapshot.pets : [{ ...snapshot.pet, id: snapshot.pet.id || requestedID || previous[0]?.id || 'legacy' }]
  const pets = Array.isArray(snapshot.pets) ? incoming : previous.length ? previous.map(p => String(p.id) === String(incoming[0].id) ? { ...p, ...incoming[0] } : p) : incoming
  return { pets: pets.map(p => ({ ...previous.find(old => String(old.id) === String(p.id)), ...p, ...growth })), account: snapshot.account || growth }
}
// SVG preserveAspectRatio 使用 110/16 的等比像素，83 高留下上下各 .25px。
export const MEAL_HEAD_TRANSFORM = 'translate(1.1 3.4) rotate(-18 6 7)'
export function mealMouthLocal() {
  const angle = -18 * Math.PI / 180, dx = 2.01 - 6, dy = 5.01 - 7
  return { x: (6 + dx * Math.cos(angle) - dy * Math.sin(angle) + 1.1) * 6.875,
    y: .25 + (7 + dx * Math.sin(angle) + dy * Math.cos(angle) + 3.4) * 6.875 }
}
export function mealGeometry(x, y) {
  const mouth = mealMouthLocal(), offset = 55 - mouth.x, seatOffset = 28 + offset
  const center = { x: Math.max(70 + seatOffset, Math.min(1190 - seatOffset, x)), y: Math.max(556.4, Math.min(590, y)) }
  const foodY = center.y - 100 + mouth.y
  return { center, width: 104, height: 30, top: center.y - 47, floorY: center.y - 17,
    contacts: [-1, 1].map(side => ({ x: center.x + side * 28, y: foodY })),
    seats: [-1, 1].map(side => ({ x: center.x + side * seatOffset, y: center.y, flipped: side < 0 })),
    bboxWidth: 110 + seatOffset * 2 }
}
export function actorSeat(geometry, index) { return { ...geometry.seats[index % 2], seat: index % 2 } }
export function mealBatches(ids) {
  return Array.from({ length: Math.ceil(ids.length / 2) }, (_, i) => ids.slice(i * 2, i * 2 + 2))
}
export function mealPositions(x, y, count) {
  const geometry = mealGeometry(x, y)
  return Array.from({ length: count }, (_, index) => actorSeat(geometry, index))
}
export function mealWaitingPositions(geometry, count) {
  // 两列候位保持可见，不因猫数增加把餐盘或横向包围盒拉长。
  return Array.from({ length: count }, (_, i) => ({ x: geometry.seats[i % 2].x,
    y: i < 2 ? geometry.center.y + 110 : Math.max(446.4, geometry.center.y - 110) }))
}
export function planMealArrival(geometry, participants, obstacles = []) {
  const seats = geometry.seats.concat(mealWaitingPositions(geometry, Math.max(0, participants.length - 2)))
  const actors = participants.map((actor, i) => ({ ...actor,
    allowedSeats: i < 2 ? [0, 1].slice(0, Math.min(2, participants.length)) : seats.map((_, seat) => seat).slice(2) }))
  return planActorSeats(actors, seats.slice(0, participants.length), obstacles)
}
export function mealPlacement(x, y, participants, obstacles = []) {
  const initial = mealGeometry(x, y), margin = initial.bboxWidth / 2 + 1
  const candidates = [...new Set([initial.center.x, ...obstacles.flatMap(r => [r.left - margin, r.right + margin]), 70, 1190])]
    .sort((a, b) => Math.abs(a - initial.center.x) - Math.abs(b - initial.center.x))
  for (const centerX of candidates) {
    const geometry = mealGeometry(centerX, y)
    const plate = { left: geometry.center.x - 52, right: geometry.center.x + 52, top: geometry.top, bottom: geometry.top + geometry.height }
    if (obstacles.some(r => plate.left <= r.right && plate.right >= r.left && plate.top <= r.bottom && plate.bottom >= r.top)) continue
    const arrival = planMealArrival(geometry, participants, obstacles)
    if (arrival) return { geometry, arrival }
  }
  return null
}
function validMealSnapshot(data) {
  const metric = value => Number.isFinite(value) && value >= 0 && value <= 100
  if (!data?.pet || data.error || !metric(data.pet.hunger) || (data.pet.mood !== undefined && !metric(data.pet.mood))) return false
  const balance = data.account?.balance ?? data.pet.balance
  if (!Number.isFinite(balance) || balance < 0) return false
  if (data.fed !== undefined && (typeof data.fed !== 'boolean' || typeof data.all_full !== 'boolean' || data.fed === data.all_full)) return false
  if (data.all_full !== undefined && typeof data.all_full !== 'boolean') return false
  if (data.pets !== undefined) {
    if (!Array.isArray(data.pets) || !data.pets.length || !data.pets.every(p => p?.id && metric(p.hunger) && metric(p.mood))) return false
    if (!data.pets.some(p => String(p.id) === String(data.pet.id))) return false
    if (new Set(data.pets.map(p => String(p.id))).size !== data.pets.length) return false
  }
  return true
}
export function createMealTransaction({ post, storage, key, makeID }) {
  const storageKey = key()
  let pending = null, inFlight = null
  try { pending = JSON.parse(storage.getItem(storageKey) || 'null') } catch { /* 无旧事务 */ }
  const state = { phase: pending ? 'unknown' : 'idle', requestID: pending?.request_id || '' }
  const persist = () => pending ? storage.setItem(storageKey, JSON.stringify(pending)) : storage.removeItem(storageKey)
  return {
    state,
    run(petID) {
      if (inFlight) return inFlight
      pending ||= { request_id: makeID(), ...(petID && petID !== 'legacy' ? { pet_id: petID } : {}) }
      state.requestID = pending.request_id
      state.phase = 'sending'
      persist()
      inFlight = (async () => {
        try {
          const result = await post(pending)
          const success = result.ok && result.status >= 200 && result.status < 300 && validMealSnapshot(result.data)
          // 喂食合同的业务拒绝是中文 400；鉴权、限流或超时不能证明此前没有结算。
          const rejected = !result.ok && result.status === 400 && typeof result.data?.error === 'string' && result.data.error.trim()
          if (!success && !rejected) throw new Error('结算状态未知')
          state.phase = success ? 'succeeded' : 'rejected'
          pending = null
          persist()
          return result
        } catch (error) {
          state.phase = 'unknown'
          persist()
          throw error
        } finally { inFlight = null }
      })()
      return inFlight
    },
  }
}
