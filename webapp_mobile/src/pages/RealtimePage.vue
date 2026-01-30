<template>
  <div class="mobile-page">
    <section class="mobile-panel has-dropdown">
      <div class="mobile-panel-header">
        <div>
          <div class="section-title">{{ t('realtime.title') }}</div>
          <div class="section-sub">{{ t('realtime.subtitle') }}</div>
        </div>
        <van-button size="small" type="primary" plain icon="replay" @click="refreshRealtime">
          {{ t('common.refresh') }}
        </van-button>
      </div>
      <div class="filter-row">
        <button type="button" class="filter-trigger" @click="websiteSheetVisible = true">
          <span class="filter-value">{{ currentWebsiteLabel }}</span>
          <van-icon name="arrow-down" />
        </button>
        <button type="button" class="filter-trigger" @click="windowSheetVisible = true">
          <span class="filter-value">{{ currentWindowLabel }}</span>
          <van-icon name="arrow-down" />
        </button>
      </div>
    </section>

    <van-empty v-if="!currentWebsiteId && !websitesLoading" :description="t('common.emptyWebsite')" />

    <div v-else class="mobile-page">
      <div v-if="loading" class="mobile-panel">
        <van-loading size="24" />
      </div>

      <div v-else class="mobile-stack">
        <div class="hero-card">
          <div class="hero-title">{{ t('realtime.activeTitle', { value: windowMinutes }) }}</div>
          <div class="hero-value">{{ formatCount(realtimeData?.activeCount) }}</div>
          <div class="hero-meta">
            <van-tag type="danger" round>{{ t('realtime.minutes', { value: windowMinutes }) }}</van-tag>
            <span>{{ t('realtime.deviceSubtitle', { value: windowMinutes }) }}</span>
          </div>
        </div>

        <section class="mobile-panel">
          <div class="mobile-panel-header">
            <div class="mobile-panel-title">{{ t('realtime.device') }}</div>
          </div>
          <van-grid :column-num="3" :border="false">
            <van-grid-item
              v-for="item in deviceRows"
              :key="item.label"
            >
              <div class="device-icon" :class="item.icon">
                <svg v-if="item.icon === 'desktop'" viewBox="0 0 24 24" aria-hidden="true">
                  <rect x="3" y="5" width="18" height="11" rx="2" />
                  <path d="M8 19h8" />
                  <path d="M12 16v3" />
                </svg>
                <svg v-else-if="item.icon === 'mobile'" viewBox="0 0 24 24" aria-hidden="true">
                  <rect x="7" y="3" width="10" height="18" rx="2" />
                  <circle cx="12" cy="17" r="1" />
                </svg>
                <svg v-else-if="item.icon === 'tablet'" viewBox="0 0 24 24" aria-hidden="true">
                  <rect x="5" y="4" width="14" height="16" rx="2" />
                  <circle cx="12" cy="17" r="1" />
                </svg>
                <svg v-else viewBox="0 0 24 24" aria-hidden="true">
                  <circle cx="12" cy="12" r="8" />
                  <path d="M4 12h16" />
                  <path d="M12 4v16" />
                </svg>
              </div>
              <div class="metric-value">{{ item.value }}</div>
              <div class="metric-label">{{ item.label }} · {{ item.percent }}</div>
            </van-grid-item>
          </van-grid>
          <div v-if="deviceRows.length === 0" class="list-empty">{{ t('realtime.noData') }}</div>
        </section>

        <van-tabs v-model:active="activeTab" animated class="mobile-tabs">
          <van-tab v-for="section in listSections" :key="section.key" :title="section.title">
            <section class="mobile-panel list-card">
              <van-cell-group inset>
                <van-cell
                  v-for="(item, index) in section.items"
                  :key="item.name"
                >
                  <template #title>
                    <span class="list-title">
                      <span class="rank-badge">{{ index + 1 }}</span>
                      <van-text-ellipsis class="list-label" :content="item.name" />
                    </span>
                  </template>
                  <template #value>
                    <div class="inline-tags">
                      <van-tag type="primary">{{ formatCount(item.count) }}</van-tag>
                      <van-tag plain type="success">{{ formatPercent(item.percent) }}</van-tag>
                    </div>
                  </template>
                </van-cell>
              </van-cell-group>
              <div v-if="section.items.length === 0" class="list-empty">{{ t('realtime.noData') }}</div>
            </section>
          </van-tab>
        </van-tabs>
      </div>
    </div>

    <van-action-sheet
      v-model:show="websiteSheetVisible"
      :duration="ACTION_SHEET_DURATION"
      teleport="body"
      :actions="websiteActions"
      :cancel-text="t('common.cancel')"
      close-on-click-action
      @select="onSelectWebsite"
    />
    <van-action-sheet
      v-model:show="windowSheetVisible"
      :duration="ACTION_SHEET_DURATION"
      teleport="body"
      :actions="windowActions"
      :cancel-text="t('common.cancel')"
      close-on-click-action
      @select="onSelectWindow"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useI18n } from 'vue-i18n';
import { fetchRealtimeStats, fetchWebsites } from '@/api';
import type { RealtimeStats, WebsiteInfo } from '@/api/types';
import { getUserPreference, saveUserPreference } from '@/utils';
import { ACTION_SHEET_DURATION } from '@mobile/constants/ui';

const { t, n } = useI18n({ useScope: 'global' });

const websites = ref<WebsiteInfo[]>([]);
const websitesLoading = ref(false);
const websiteSheetVisible = ref(false);
const windowSheetVisible = ref(false);
const currentWebsiteId = ref('');
const windowMinutes = ref(5);
const loading = ref(false);
const realtimeData = ref<RealtimeStats | null>(null);
const activeTab = ref(0);

const websiteOptions = computed(() =>
  websites.value.map((site) => ({ text: site.name, value: site.id }))
);

const websiteActions = computed(() =>
  websites.value.map((site) => ({ name: site.name, value: site.id }))
);

const windowOptions = computed(() => [
  { text: t('realtime.minutes', { value: 5 }), value: 5 },
  { text: t('realtime.minutes', { value: 15 }), value: 15 },
  { text: t('realtime.minutes', { value: 30 }), value: 30 },
]);

const windowActions = computed(() =>
  windowOptions.value.map((option) => ({ name: option.text, value: option.value }))
);

const currentWebsiteLabel = computed(() => {
  if (!currentWebsiteId.value) {
    return t('common.selectWebsite');
  }
  return websites.value.find((site) => site.id === currentWebsiteId.value)?.name || t('common.selectWebsite');
});

const currentWindowLabel = computed(() => {
  const option = windowOptions.value.find((item) => item.value === windowMinutes.value);
  return option?.text || t('common.select');
});

const deviceRows = computed(() => {
  const items = realtimeData.value?.deviceBreakdown || [];
  return items.map((item) => ({
    label: item.name,
    value: formatCount(item.count),
    percent: formatPercent(item.percent),
    icon: resolveDeviceIcon(item.name),
  }));
});

const listSections = computed(() => [
  { key: 'pages', title: t('realtime.pages'), items: realtimeData.value?.pages || [] },
  { key: 'referer', title: t('realtime.referer'), items: realtimeData.value?.referers || [] },
  { key: 'entry', title: t('realtime.entryPages'), items: realtimeData.value?.entryPages || [] },
  { key: 'browser', title: t('realtime.browser'), items: realtimeData.value?.browsers || [] },
  { key: 'location', title: t('realtime.location'), items: realtimeData.value?.locations || [] },
]);

function formatCount(value: number | string | undefined | null) {
  const num = Number(value || 0);
  if (!Number.isFinite(num)) {
    return t('common.none');
  }
  return n(num);
}

function formatPercent(value: number | string | undefined | null) {
  const num = Number(value || 0);
  if (!Number.isFinite(num)) {
    return t('common.none');
  }
  return `${num.toFixed(1)}%`;
}

function resolveDeviceIcon(label: string) {
  const lower = String(label || '').toLowerCase();
  if (lower.includes('pc') || lower.includes('desktop') || lower.includes('电脑')) {
    return 'desktop';
  }
  if (lower.includes('mobile') || lower.includes('手机') || lower.includes('android') || lower.includes('ios')) {
    return 'mobile';
  }
  if (lower.includes('tablet') || lower.includes('pad') || lower.includes('平板')) {
    return 'tablet';
  }
  return 'other';
}

function onSelectWebsite(action: { value?: string }) {
  if (action?.value) {
    currentWebsiteId.value = action.value;
  }
}

function onSelectWindow(action: { value?: number }) {
  if (typeof action?.value === 'number') {
    windowMinutes.value = action.value;
  }
}

async function loadWebsites() {
  websitesLoading.value = true;
  try {
    const data = await fetchWebsites();
    websites.value = data || [];
    const saved = getUserPreference('selectedWebsite', '');
    if (saved && websites.value.find((site) => site.id === saved)) {
      currentWebsiteId.value = saved;
    } else if (websites.value.length > 0) {
      currentWebsiteId.value = websites.value[0].id;
    } else {
      currentWebsiteId.value = '';
    }
  } catch (error) {
    console.error('初始化网站失败:', error);
    websites.value = [];
    currentWebsiteId.value = '';
  } finally {
    websitesLoading.value = false;
  }
}

async function refreshRealtime() {
  if (!currentWebsiteId.value) {
    return;
  }
  loading.value = true;
  try {
    const data = await fetchRealtimeStats(currentWebsiteId.value, windowMinutes.value);
    realtimeData.value = data;
  } catch (error) {
    console.error('加载实时数据失败:', error);
  } finally {
    loading.value = false;
  }
}

watch(currentWebsiteId, (value) => {
  if (value) {
    saveUserPreference('selectedWebsite', value);
  }
  refreshRealtime();
});

watch(windowMinutes, () => {
  refreshRealtime();
});

onMounted(() => {
  loadWebsites();
});
</script>
