<script setup>
import { computed, ref } from 'vue'
import PixelItem from './PixelItem.vue'
import { wallStyle } from '../scene-art.js'
import { filterShopItems, getShopDisplay } from '../shop-display.js'

const props = defineProps({
  items: { type: Array, default: () => [] },
  balance: { type: Number, default: 0 },
  busy: { type: Boolean, default: false },
})
const emit = defineEmits(['action'])
const query = ref('')
const category = ref('all')
const ownedOnly = ref(false)
const categories = [
  { key: 'all', label: '全部' },
  { key: 'furniture', label: '家具' },
  { key: 'toy', label: '玩具' },
  { key: 'decor', label: '装饰' },
]
const products = computed(() => filterShopItems(props.items, {
  query: query.value,
  category: category.value,
  ownedOnly: ownedOnly.value,
}).map((item) => ({ item, display: getShopDisplay(item) })))
const emptyMessage = computed(() => {
  if (query.value.trim()) return '没有找到这个名称的商品'
  if (ownedOnly.value) return category.value === 'all' ? '暂无已购商品' : '这个类别暂无已购商品'
  return category.value === 'all' ? '商店暂无商品' : '这个类别暂无商品'
})
</script>

<template>
  <section class="pixel-shop" aria-label="像素商店">
    <header class="shop-toolbar">
      <div class="shop-heading">
        <h2 class="shop-title">像素商店</h2>
        <p class="shop-balance" role="status" aria-live="polite" aria-atomic="true">
          <span class="balance-label">余额</span>
          <strong>{{ props.balance }}</strong><span>星星</span>
        </p>
        <button
          type="button"
          class="shop-owned-filter"
          :aria-pressed="ownedOnly"
          aria-label="只看已购商品"
          @click="ownedOnly = !ownedOnly"
        >
          <span class="filter-check" aria-hidden="true"></span>已购
        </button>
      </div>
      <input
        v-model="query"
        class="shop-search"
        type="search"
        aria-label="搜索商品名称"
        placeholder="搜索商品名称"
        autocomplete="off"
      >
      <div class="shop-categories" role="group" aria-label="商品分类">
        <button
          v-for="tab in categories"
          :key="tab.key"
          type="button"
          class="shop-category"
          :aria-pressed="category === tab.key"
          @click="category = tab.key"
        >{{ tab.label }}</button>
      </div>
    </header>

    <div class="shop-products" :aria-busy="props.busy">
      <div class="shop-results" role="status" aria-live="polite" aria-atomic="true">
        {{ products.length }} 件商品<span v-if="ownedOnly"> · 只看已购</span>
      </div>
      <div v-if="!products.length" class="shop-empty">
        <p>{{ emptyMessage }}</p>
        <span v-if="query.trim()">试试其他名称，或清空搜索。</span>
        <span v-else-if="ownedOnly">取消「已购」筛选，看看可以解锁的商品。</span>
        <span v-else-if="category !== 'all'">切换到「全部」看看其他商品。</span>
      </div>
      <ul v-else class="shop-product-list">
        <li v-for="{ item, display } in products" :key="item.key" class="shop-card">
          <div class="shop-card-heading">
            <span class="shop-item-art" aria-hidden="true">
              <span
                v-if="display.surface === 'wallpaper'"
                class="shop-wallpaper"
                :style="wallStyle(item.builtin || item.key === 'wall-default' ? '' : item.key, 64)"
              ></span>
              <img v-else-if="String(display.icon).startsWith('/')" class="shop-item-image" :src="display.icon" alt="">
              <PixelItem v-else :item-key="display.icon" :size="64" />
            </span>
            <div class="shop-card-title">
              <span class="shop-item-category">{{ display.categoryLabel }}</span>
              <h3 class="shop-item-name">{{ display.name }}</h3>
            </div>
          </div>
          <p class="shop-description">{{ display.description }}</p>
          <p v-if="item.builtin" class="shop-badge shop-badge-owned">自带 · 免费使用</p>
          <p v-else-if="item.owned" class="shop-badge shop-badge-owned">已解锁 · 反复玩不耗星星</p>
          <p v-else-if="display.unlockBadge" class="shop-badge">{{ display.unlockBadge }}</p>
          <p v-if="item.owned && display.ownedHint" class="shop-owned-hint">{{ display.ownedHint }}</p>
          <div class="shop-card-state">
            <span class="shop-price">{{ item.builtin ? '免费' : `${item.price} 星星` }}</span>
            <span class="shop-state">
              {{ item.using ? '使用中' : item.placed ? '已摆放在房间' : item.builtin ? '随时可用' : item.owned ? '已解锁' : '尚未解锁' }}
            </span>
          </div>
          <button
            type="button"
            class="shop-action"
            :class="{ 'shop-action-owned': item.owned || item.builtin }"
            :disabled="props.busy || item.using"
            :aria-label="`${item.actionLabel}：${display.name}`"
            @click="emit('action', item)"
          >{{ item.actionLabel }}</button>
        </li>
      </ul>
    </div>
  </section>
</template>

<style scoped>
.pixel-shop {
  display: flex;
  flex-direction: column;
  width: 100%;
  height: 100%;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
  color: #654738;
  background: #fff9ed;
  font-family: inherit;
  font-size: 13px;
  line-height: 1.5;
}

.pixel-shop *,
.pixel-shop *::before,
.pixel-shop *::after {
  box-sizing: border-box;
}

.shop-toolbar {
  display: flex;
  flex: 0 0 auto;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
  padding: 8px;
  border-bottom: 2px solid #dfc6a4;
  background: #f8eddb;
}

.shop-heading {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto auto;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.shop-title {
  margin: 0;
  font-size: 14px;
  line-height: 1.3;
}

.shop-balance {
  display: flex;
  align-items: baseline;
  gap: 3px;
  margin: 0;
  font-size: 12px;
}

.balance-label {
  color: #846449;
}

.shop-balance strong {
  color: #88532b;
  font-size: 15px;
  font-variant-numeric: tabular-nums;
}

.shop-owned-filter,
.shop-category,
.shop-action {
  min-width: 0;
  min-height: 40px;
  border: 2px solid #b9946a;
  border-radius: 3px;
  color: #654738;
  background: #fffbf2;
  font: inherit;
  font-weight: 700;
  cursor: pointer;
}

.shop-owned-filter {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 4px 6px;
  font-size: 12px;
}

.filter-check {
  width: 11px;
  height: 11px;
  border: 2px solid #b9946a;
  background: #fffbf2;
}

.shop-owned-filter[aria-pressed='true'] .filter-check {
  border-color: #765532;
  background: #765532;
  box-shadow: inset 0 0 0 2px #f4dfb3;
}

.shop-owned-filter[aria-pressed='true'],
.shop-category[aria-pressed='true'] {
  border-color: #886440;
  color: #563921;
  background: #efd7ae;
}

.shop-search {
  display: block;
  width: 100%;
  min-width: 0;
  min-height: 36px;
  padding: 6px 9px;
  border: 2px solid #c8ac87;
  border-radius: 3px;
  color: #654738;
  background: #fffdf7;
  font: inherit;
}

.shop-search::placeholder {
  color: #886d56;
  opacity: 1;
}

.shop-categories {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 5px;
  min-width: 0;
}

.shop-category {
  padding: 4px 2px;
}

.shop-search:focus-visible {
  outline: 3px solid #426d78;
  outline-offset: 1px;
}

.shop-owned-filter:focus-visible,
.shop-category:focus-visible,
.shop-action:focus-visible {
  outline: 3px solid #946237;
  outline-offset: 2px;
}

.shop-products {
  flex: 1 1 auto;
  min-width: 0;
  min-height: 0;
  overflow-y: auto;
  overflow-x: hidden;
  overscroll-behavior-y: contain;
  padding: 8px 10px 12px;
}

.shop-results {
  margin-bottom: 8px;
  color: #846449;
  font-size: 12px;
}

.shop-product-list {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 12px;
  margin: 0;
  padding: 0;
  list-style: none;
}

.shop-card {
  min-width: 0;
  padding: 12px;
  border: 2px solid #d7bb95;
  border-radius: 4px;
  background: #fffdf6;
  box-shadow: 2px 2px 0 #eddcc3;
}

.shop-card-heading {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.shop-item-art {
  display: flex;
  flex: 0 0 64px;
  align-items: center;
  justify-content: center;
  width: 64px;
  min-height: 64px;
}

.shop-item-image {
  display: block;
  width: 64px;
  height: 64px;
  object-fit: contain;
  image-rendering: pixelated;
}

.shop-wallpaper {
  display: block;
  width: 64px;
  height: 64px;
  border: 3px solid #b9946a;
  box-shadow: inset 2px 2px 0 #ffffff;
  image-rendering: pixelated;
}

.shop-card-title {
  flex: 1;
  min-width: 0;
}

.shop-item-category {
  color: #846449;
  font-size: 12px;
}

.shop-item-name {
  margin: 2px 0 0;
  font-size: 16px;
  line-height: 1.4;
  overflow-wrap: anywhere;
}

.shop-description {
  margin: 10px 0 0;
  line-height: 1.6;
  overflow-wrap: anywhere;
}

.shop-badge {
  margin: 8px 0 0;
  padding: 5px 7px;
  border-left: 3px solid #bb8950;
  color: #805429;
  background: #fff0cf;
  font-size: 12px;
  overflow-wrap: anywhere;
}

.shop-badge-owned {
  border-left-color: #879663;
  color: #506039;
  background: #eff2df;
}

.shop-owned-hint {
  margin: 6px 0 0;
  color: #6d7350;
  font-size: 12px;
  overflow-wrap: anywhere;
}

.shop-card-state {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  justify-content: space-between;
  gap: 4px 8px;
  margin: 10px 0;
}

.shop-price {
  color: #88532b;
  font-size: 15px;
  font-weight: 700;
}

.shop-state {
  color: #846449;
  font-size: 12px;
}

.shop-action {
  display: block;
  width: 100%;
  min-height: 44px;
  padding: 8px;
  border-color: #a47345;
  background: #f1d394;
  box-shadow: 0 2px 0 #b9946a;
  overflow-wrap: anywhere;
}

.shop-action-owned {
  border-color: #9da574;
  color: #506039;
  background: #e9edcf;
  box-shadow: 0 2px 0 #a7ad83;
}

.shop-action:disabled {
  color: #776d5d;
  border-color: #c0b39d;
  background: #ece7db;
  box-shadow: none;
  cursor: default;
}

@media (hover: hover) {
  .shop-category:hover,
  .shop-owned-filter:hover,
  .shop-action:not(:disabled):hover {
    border-color: #805833;
  }
}

.shop-empty {
  padding: 24px 6px;
  text-align: center;
  overflow-wrap: anywhere;
}

.shop-empty p {
  margin: 0 0 8px;
  font-weight: 700;
}

.shop-empty span {
  color: #846449;
  font-size: 12px;
}

@media (min-width: 600px) and (max-width: 900px) and (max-height: 500px) {
  .shop-toolbar { display: grid; grid-template-columns: 1fr 1fr; align-items: center; }
  .shop-categories { grid-column: 1 / -1; }
  .shop-product-list { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
</style>
