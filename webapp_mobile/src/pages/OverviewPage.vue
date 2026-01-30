<template>
  <div class="mobile-page">
    <section class="mobile-panel has-dropdown">
      <div class="mobile-panel-header">
        <div>
          <div class="section-title">{{ t('app.menu.overview') }}</div>
          <div class="section-sub">{{ t('common.currentWebsite') }}</div>
        </div>
        <van-button size="small" type="primary" plain icon="replay" @click="refreshOverview">
          {{ t('common.refresh') }}
        </van-button>
      </div>
      <div class="filter-row">
        <button type="button" class="filter-trigger" @click="websiteSheetVisible = true">
          <span class="filter-value">{{ currentWebsiteLabel }}</span>
          <van-icon name="arrow-down" />
        </button>
        <button type="button" class="filter-trigger" @click="rangeSheetVisible = true">
          <span class="filter-value">{{ currentRangeLabel }}</span>
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
          <div class="hero-title">{{ t('overview.liveVisitors') }}</div>
          <div class="hero-value">{{ activeVisitorText }}</div>
          <div class="hero-meta">
            <van-tag type="primary" round>{{ t('overview.liveStatus') }}</van-tag>
            <span>{{ t('overview.metricsTitle') }}</span>
          </div>
        </div>

        <div class="metric-grid">
          <div v-for="item in metricCards" :key="item.key" class="metric-card">
            <div class="metric-card-header">
              <div class="metric-icon" :class="item.key">
                <svg v-if="item.key === 'pv'" viewBox="0 0 24 24" aria-hidden="true">
                  <path d="M4 15l4-4 4 3 6-7" />
                  <path d="M4 19h16" />
                </svg>
                <svg v-else-if="item.key === 'uv'" viewBox="0 0 24 24" aria-hidden="true">
                  <circle cx="8" cy="8" r="3" />
                  <circle cx="16" cy="10" r="3" />
                  <path d="M4 19c0-3 2.5-5 5-5" />
                  <path d="M12 19c0-2.5 2-4 4-4" />
                </svg>
                <svg v-else-if="item.key === 'session'" viewBox="0 0 24 24" aria-hidden="true">
                  <rect x="4" y="5" width="16" height="13" rx="3" />
                  <path d="M8 10h8M8 14h5" />
                </svg>
                <svg v-else viewBox="0 0 24 24" aria-hidden="true">
                  <path d="M4 18h16" />
                  <path d="M7 16V8" />
                  <path d="M12 16V6" />
                  <path d="M17 16v-5" />
                </svg>
              </div>
              <div class="metric-label">{{ item.label }}</div>
            </div>
            <div class="metric-value">{{ item.value }}</div>
          </div>
        </div>

        <section class="mobile-panel list-card">
          <div class="mobile-panel-header">
            <div class="mobile-panel-title">{{ t('overview.topPage') }}</div>
          </div>
          <van-cell-group inset>
            <van-cell
              v-for="item in topPages"
              :key="item.label"
              :value="item.value"
            >
              <template #title>
                <span class="list-title">
                  <span class="rank-badge">{{ item.rank }}</span>
                  <span>{{ item.label }}</span>
                </span>
              </template>
            </van-cell>
          </van-cell-group>
          <div v-if="topPages.length === 0" class="list-empty">{{ t('common.noData') }}</div>
        </section>

        <section class="mobile-panel list-card">
          <div class="mobile-panel-header">
            <div class="mobile-panel-title">{{ t('overview.referer') }}</div>
          </div>
          <van-cell-group inset>
            <van-cell
              v-for="item in topReferers"
              :key="item.label"
              :value="item.value"
            >
              <template #title>
                <span class="list-title">
                  <span class="rank-badge">{{ item.rank }}</span>
                  <span>{{ item.label }}</span>
                </span>
              </template>
            </van-cell>
          </van-cell-group>
          <div v-if="topReferers.length === 0" class="list-empty">{{ t('common.noData') }}</div>
        </section>
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
      v-model:show="rangeSheetVisible"
      :duration="ACTION_SHEET_DURATION"
      teleport="body"
      :actions="rangeActions"
      :cancel-text="t('common.cancel')"
      close-on-click-action
      @select="onSelectRange"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useI18n } from 'vue-i18n';
import { fetchOverallStats, fetchRefererStats, fetchUrlStats, fetchWebsites } from '@/api';
import type { SimpleSeriesStats, WebsiteInfo } from '@/api/types';
import { formatRefererLabel } from '@/i18n/mappings';
import { normalizeLocale } from '@/i18n';
import { formatTraffic, getUserPreference, saveUserPreference } from '@/utils';
import { ACTION_SHEET_DURATION } from '@mobile/constants/ui';

const { t, n, locale } = useI18n({ useScope: 'global' });

const websites = ref<WebsiteInfo[]>([]);
const websitesLoading = ref(false);
const websiteSheetVisible = ref(false);
const rangeSheetVisible = ref(false);
const currentWebsiteId = ref('');
const dateRange = ref('today');
const loading = ref(false);
const overall = ref<Record<string, any> | null>(null);
const urlStats = ref<SimpleSeriesStats | null>(null);
const refererStats = ref<SimpleSeriesStats | null>(null);

const websiteOptions = computed(() =>
  websites.value.map((site) => ({ text: site.name, value: site.id }))
);

const websiteActions = computed(() =>
  websites.value.map((site) => ({ name: site.name, value: site.id }))
);

const dateRangeOptions = computed(() => [
  { text: t('common.today'), value: 'today' },
  { text: t('common.yesterday'), value: 'yesterday' },
  { text: t('common.last7Days'), value: 'last7days' },
  { text: t('common.last30Days'), value: 'last30days' },
]);

const rangeActions = computed(() =>
  dateRangeOptions.value.map((option) => ({ name: option.text, value: option.value }))
);

const currentWebsiteLabel = computed(() => {
  if (!currentWebsiteId.value) {
    return t('common.selectWebsite');
  }
  return websites.value.find((site) => site.id === currentWebsiteId.value)?.name || t('common.selectWebsite');
});

const currentRangeLabel = computed(() => {
  const option = dateRangeOptions.value.find((item) => item.value === dateRange.value);
  return option?.text || t('common.select');
});

const metricCards = computed(() => {
  const data = overall.value || {};
  return [
    {
      key: 'pv',
      label: t('common.pageview'),
      value: formatCount(data.pv),
    },
    {
      key: 'uv',
      label: t('common.visitors'),
      value: formatCount(data.uv),
    },
    {
      key: 'session',
      label: t('daily.session'),
      value: formatCount(data.sessionCount),
    },
    {
      key: 'traffic',
      label: t('common.traffic'),
      value: formatTraffic(Number(data.traffic || 0)),
    },
  ];
});

const activeVisitorText = computed(() => formatCount(overall.value?.activeVisitorCount));

const currentLocale = computed(() => normalizeLocale(locale.value));
const topPages = computed(() => buildSeriesRows(urlStats.value));
const topReferers = computed(() =>
  buildSeriesRows(refererStats.value, (label) => formatRefererLabel(label, currentLocale.value, t))
);

function formatCount(value: number | string | undefined | null) {
  const num = Number(value || 0);
  if (!Number.isFinite(num)) {
    return t('common.none');
  }
  return n(num);
}

function onSelectWebsite(action: { value?: string }) {
  if (action?.value) {
    currentWebsiteId.value = action.value;
  }
}

function onSelectRange(action: { value?: string }) {
  if (action?.value) {
    dateRange.value = action.value;
  }
}

function buildSeriesRows(stats: SimpleSeriesStats | null, formatLabel?: (value: string) => string) {
  if (!stats || !stats.key) {
    return [] as Array<{ label: string; value: string; rank: number }>;
  }
  return stats.key.map((label, index) => {
    const value = stats.pv?.[index] ?? stats.uv?.[index] ?? 0;
    const normalizedLabel = formatLabel ? formatLabel(label) : label;
    return { label: normalizedLabel || t('common.none'), value: formatCount(value), rank: index + 1 };
  });
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

async function refreshOverview() {
  if (!currentWebsiteId.value) {
    return;
  }
  loading.value = true;
  try {
    const range = dateRange.value;
    const [overallData, urlData, refererData] = await Promise.all([
      fetchOverallStats(currentWebsiteId.value, range),
      fetchUrlStats(currentWebsiteId.value, range, 10),
      fetchRefererStats(currentWebsiteId.value, range, 10),
    ]);
    overall.value = overallData;
    urlStats.value = urlData;
    refererStats.value = refererData;
  } catch (error) {
    console.error('加载概况数据失败:', error);
  } finally {
    loading.value = false;
  }
}

watch(currentWebsiteId, (value) => {
  if (value) {
    saveUserPreference('selectedWebsite', value);
  }
  refreshOverview();
});

watch(dateRange, () => {
  refreshOverview();
});

onMounted(() => {
  loadWebsites();
});
</script>
