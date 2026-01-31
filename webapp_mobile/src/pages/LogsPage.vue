<template>
  <div class="mobile-page">
    <section class="mobile-panel has-dropdown">
      <div class="mobile-panel-header">
        <div>
          <div class="section-title">{{ t('app.menu.logs') }}</div>
          <div class="section-sub">{{ t('logs.subtitle') }}</div>
        </div>
        <van-button size="small" type="primary" plain icon="replay" @click="resetAndLoad">
          {{ t('common.refresh') }}
        </van-button>
      </div>
      <div class="filter-row">
        <button type="button" class="filter-trigger" @click="websiteSheetVisible = true">
          <span class="filter-value">{{ currentWebsiteLabel }}</span>
          <van-icon name="arrow-down" />
        </button>
        <button type="button" class="filter-trigger" @click="sortSheetVisible = true">
          <span class="filter-value">{{ currentSortLabel }}</span>
          <van-icon name="arrow-down" />
        </button>
      </div>
    </section>

    <van-empty v-if="!currentWebsiteId && !websitesLoading" :description="t('common.emptyWebsite')" />

    <div v-else class="mobile-page">
      <section class="mobile-panel mobile-filter-card">
        <van-search
          v-model="searchFilter"
          :placeholder="t('logs.searchPlaceholder')"
          shape="round"
          class="mobile-search"
          @search="resetAndLoad"
          @clear="resetAndLoad"
        />
        <van-cell-group class="mobile-filter-group">
          <van-cell :title="t('common.pageview')">
            <template #value>
              <van-switch v-model="pageviewOnly" size="20" />
            </template>
          </van-cell>
          <van-cell
            :title="t('logs.advancedFilters')"
            is-link
            class="filter-advanced-entry"
            @click="advancedVisible = true"
          >
            <template #value>
              <van-tag v-if="activeFilterCount" round plain type="primary" class="filter-count-tag">
                {{ activeFilterCount }}
              </van-tag>
            </template>
          </van-cell>
        </van-cell-group>
        <div v-if="activeFilterTags.length" class="filter-tag-row">
          <div class="inline-tags">
            <van-tag
              v-for="tag in activeFilterTags"
              :key="tag.key"
              round
              plain
              closeable
              type="primary"
              @close="removeFilter(tag.key)"
            >
              {{ tag.label }}
            </van-tag>
          </div>
        </div>
      </section>

      <section class="mobile-panel list-card mobile-log-list">
        <van-list
          v-model:loading="loading"
          :finished="finished"
          :finished-text="t('common.noMore')"
          @load="loadMore"
        >
          <van-cell-group inset>
            <van-cell
              v-for="item in logs"
              :key="item.key"
              :class="['mobile-log-cell', item.statusType]"
              clickable
              @click="openLogDetail(item)"
            >
              <template #title>
                <div class="mobile-log-item">
                  <div class="mobile-log-header">
                    <span class="method-pill">{{ item.method }}</span>
                    <van-text-ellipsis class="log-path" :content="item.path" :rows="2" />
                  </div>
                  <div class="mobile-log-meta">
                    <span class="meta-item">{{ item.time }}</span>
                    <span class="meta-dot">·</span>
                    <span class="meta-item">{{ item.ip }}</span>
                  </div>
                  <div class="mobile-log-location">{{ item.location }}</div>
                </div>
              </template>
              <template #value>
                <div class="mobile-tag-group">
                  <van-tag :type="item.statusType" round>{{ item.statusCode }}</van-tag>
                  <van-tag v-if="item.pageview" plain type="primary" round>PV</van-tag>
                </div>
              </template>
            </van-cell>
          </van-cell-group>
        </van-list>
      </section>
    </div>

    <van-popup
      v-model:show="detailVisible"
      position="bottom"
      round
      teleport="body"
      class="log-detail-popup"
    >
      <div v-if="detailItem" class="log-detail-sheet">
        <div class="log-detail-header">
          <div class="log-detail-title">{{ t('app.menu.logs') }}</div>
          <van-icon name="cross" class="log-detail-close" @click="detailVisible = false" />
        </div>
        <div class="log-detail-path">{{ detailItem.path }}</div>
        <div class="log-detail-tags">
          <van-tag :type="detailItem.statusType" round>{{ detailItem.statusCode }}</van-tag>
          <van-tag v-if="detailItem.pageview" plain type="primary" round>PV</van-tag>
        </div>
        <van-cell-group inset class="log-detail-meta">
          <van-cell :title="t('common.time')" :value="detailItem.time" />
          <van-cell :title="t('common.ip')" :value="detailItem.ip" />
          <van-cell :title="t('common.location')" :value="detailItem.location" />
          <van-cell :title="t('common.method')" :value="detailItem.method" />
        </van-cell-group>
      </div>
    </van-popup>

    <van-popup
      v-model:show="advancedVisible"
      position="bottom"
      round
      teleport="body"
      class="log-filter-popup"
    >
      <div class="log-filter-sheet">
        <div class="log-filter-header">
          <div class="log-filter-title">{{ t('logs.advancedFilters') }}</div>
          <van-icon name="cross" class="log-filter-close" @click="advancedVisible = false" />
        </div>
        <div class="log-filter-section">
          <div class="log-filter-label">{{ t('logs.statusCode') }}</div>
          <div class="status-chip-row">
            <button
              v-for="option in statusClassOptions"
              :key="option.value"
              type="button"
              class="status-chip"
              :class="{ 'is-active': statusClass === option.value }"
              :aria-pressed="statusClass === option.value"
              @click="toggleStatusClass(option.value)"
            >
              {{ option.label }}
            </button>
          </div>
          <van-field
            v-model="statusCodeInput"
            type="digit"
            :placeholder="t('logs.statusCodePlaceholder')"
            clearable
            maxlength="3"
            class="mobile-filter-field"
          />
        </div>
        <van-cell-group class="mobile-filter-group">
          <van-cell :title="t('logs.excludeInternal')">
            <template #value>
              <van-switch v-model="excludeInternal" size="20" />
            </template>
          </van-cell>
          <van-cell :title="t('logs.excludeSpider')">
            <template #value>
              <van-switch v-model="excludeSpider" size="20" />
            </template>
          </van-cell>
          <van-cell :title="t('logs.excludeForeign')">
            <template #value>
              <van-switch v-model="excludeForeign" size="20" />
            </template>
          </van-cell>
        </van-cell-group>
      </div>
    </van-popup>

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
      v-model:show="sortSheetVisible"
      :duration="ACTION_SHEET_DURATION"
      teleport="body"
      :actions="sortActions"
      :cancel-text="t('common.cancel')"
      close-on-click-action
      @select="onSelectSortOrder"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useI18n } from 'vue-i18n';
import { fetchLogs, fetchWebsites } from '@/api';
import type { WebsiteInfo } from '@/api/types';
import { formatLocationLabel } from '@/i18n/mappings';
import { normalizeLocale } from '@/i18n';
import { getUserPreference, saveUserPreference } from '@/utils';
import { ACTION_SHEET_DURATION } from '@mobile/constants/ui';

const { t, locale } = useI18n({ useScope: 'global' });

const websites = ref<WebsiteInfo[]>([]);
const websitesLoading = ref(false);
const websiteSheetVisible = ref(false);
const sortSheetVisible = ref(false);
const currentWebsiteId = ref('');
const sortField = ref(getUserPreference('logsSortField', 'timestamp'));
const sortOrder = ref(getUserPreference('logsSortOrder', 'desc'));
const searchFilter = ref('');
const pageviewOnly = ref(false);
const advancedVisible = ref(false);
const statusClass = ref(getUserPreference('logsStatusClass', ''));
const statusCodeInput = ref(getUserPreference('logsStatusCode', ''));
const excludeInternal = ref(getUserPreference('logsExcludeInternal', '') === '1');
const excludeSpider = ref(getUserPreference('logsExcludeSpider', '') === '1');
const excludeForeign = ref(getUserPreference('logsExcludeForeign', '') === '1');

const loading = ref(false);
const finished = ref(false);
const page = ref(1);
const pageSize = 20;
const totalPages = ref(0);
const logs = ref<Array<Record<string, any>>>([]);
const detailVisible = ref(false);
const detailItem = ref<Record<string, any> | null>(null);

const currentLocale = computed(() => normalizeLocale(locale.value));

const statusClassOptions = [
  { label: '2xx', value: '2xx' },
  { label: '3xx', value: '3xx' },
  { label: '4xx', value: '4xx' },
  { label: '5xx', value: '5xx' },
];

const websiteOptions = computed(() =>
  websites.value.map((site) => ({ text: site.name, value: site.id }))
);

const websiteActions = computed(() =>
  websites.value.map((site) => ({ name: site.name, value: site.id }))
);

const sortOrderOptions = computed(() => [
  { text: t('logs.sortDesc'), value: 'desc' },
  { text: t('logs.sortAsc'), value: 'asc' },
]);

const sortActions = computed(() =>
  sortOrderOptions.value.map((option) => ({ name: option.text, value: option.value }))
);

const currentWebsiteLabel = computed(() => {
  if (!currentWebsiteId.value) {
    return t('common.selectWebsite');
  }
  return websites.value.find((site) => site.id === currentWebsiteId.value)?.name || t('common.selectWebsite');
});

const currentSortLabel = computed(() => {
  const option = sortOrderOptions.value.find((item) => item.value === sortOrder.value);
  return option?.text || t('common.select');
});

const statusCodeParam = computed(() => resolveStatusCodeParam(statusCodeInput.value));
const statusClassParam = computed(() => (statusCodeParam.value ? '' : statusClass.value));

const activeFilterTags = computed(() => {
  const tags: Array<{ key: string; label: string }> = [];
  if (pageviewOnly.value) {
    tags.push({ key: 'pageview', label: t('logs.excludeNoPv') });
  }
  if (statusCodeParam.value) {
    tags.push({ key: 'statusCode', label: `${t('logs.statusCode')} ${statusCodeParam.value}` });
  } else if (statusClassParam.value) {
    tags.push({ key: 'statusClass', label: `${t('logs.statusCode')} ${statusClassParam.value}` });
  }
  if (excludeInternal.value) {
    tags.push({ key: 'excludeInternal', label: t('logs.excludeInternal') });
  }
  if (excludeSpider.value) {
    tags.push({ key: 'excludeSpider', label: t('logs.excludeSpider') });
  }
  if (excludeForeign.value) {
    tags.push({ key: 'excludeForeign', label: t('logs.excludeForeign') });
  }
  return tags;
});

const activeFilterCount = computed(() => activeFilterTags.value.length);

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

function mapLogItem(log: Record<string, any>, index: number) {
  const time = log.time || t('common.none');
  const ip = log.ip || t('common.none');
  const locationRaw = log.domestic_location || log.global_location || '';
  const location = formatLocationLabel(locationRaw, currentLocale.value, t) || t('common.none');
  const method = log.method || '';
  const url = log.url || '';
  const request = `${method} ${url}`.trim() || t('common.none');
  const path = url || request;
  const statusCode = Number(log.status_code || 0);
  const statusType =
    statusCode >= 500 ? 'danger' : statusCode >= 400 ? 'warning' : statusCode >= 300 ? 'primary' : 'success';
  return {
    key: `${time}-${ip}-${index}`,
    time,
    ip,
    location,
    request,
    method: method || 'GET',
    path,
    statusCode: statusCode || '--',
    statusType,
    pageview: Boolean(log.pageview_flag),
  };
}

function onSelectWebsite(action: { value?: string }) {
  if (action?.value) {
    currentWebsiteId.value = action.value;
  }
}

function onSelectSortOrder(action: { value?: string }) {
  if (action?.value) {
    sortOrder.value = action.value;
  }
}

function openLogDetail(item: Record<string, any>) {
  detailItem.value = item;
  detailVisible.value = true;
}

function resolveStatusCodeParam(value: string) {
  if (!value) {
    return '';
  }
  const normalized = Math.trunc(Number(value));
  if (!Number.isFinite(normalized) || normalized < 100 || normalized > 599) {
    return '';
  }
  return String(normalized);
}

function toggleStatusClass(value: string) {
  statusClass.value = statusClass.value === value ? '' : value;
  statusCodeInput.value = '';
}

function removeFilter(key: string) {
  switch (key) {
    case 'pageview':
      pageviewOnly.value = false;
      break;
    case 'statusCode':
      statusCodeInput.value = '';
      break;
    case 'statusClass':
      statusClass.value = '';
      break;
    case 'excludeInternal':
      excludeInternal.value = false;
      break;
    case 'excludeSpider':
      excludeSpider.value = false;
      break;
    case 'excludeForeign':
      excludeForeign.value = false;
      break;
    default:
      break;
  }
}

async function loadMore() {
  loading.value = true;
  if (!currentWebsiteId.value) {
    finished.value = true;
    loading.value = false;
    return;
  }
  try {
    const result = await fetchLogs(
      currentWebsiteId.value,
      page.value,
      pageSize,
      sortField.value,
      sortOrder.value,
      searchFilter.value,
      undefined,
      statusClassParam.value || undefined,
      statusCodeParam.value || undefined,
      excludeInternal.value,
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
      pageviewOnly.value,
      undefined,
      undefined,
      excludeSpider.value,
      excludeForeign.value
    );
    const rawLogs = result.logs || [];
    const mapped = rawLogs.map((log: Record<string, any>, index: number) => mapLogItem(log, index));
    logs.value = logs.value.concat(mapped);
    totalPages.value = result.pagination?.pages || 0;
    if (page.value >= totalPages.value || rawLogs.length === 0) {
      finished.value = true;
    } else {
      page.value += 1;
    }
  } catch (error) {
    console.error('加载日志失败:', error);
    finished.value = true;
  } finally {
    loading.value = false;
  }
}

function resetAndLoad() {
  logs.value = [];
  page.value = 1;
  finished.value = false;
  loading.value = false;
  loadMore();
}

let statusCodeTimer: ReturnType<typeof setTimeout> | null = null;

watch(currentWebsiteId, (value) => {
  if (value) {
    saveUserPreference('selectedWebsite', value);
  }
  resetAndLoad();
});

watch([sortOrder, pageviewOnly, excludeInternal, excludeSpider, excludeForeign], () => {
  saveUserPreference('logsSortOrder', sortOrder.value);
  saveUserPreference('logsExcludeInternal', excludeInternal.value ? '1' : '');
  saveUserPreference('logsExcludeSpider', excludeSpider.value ? '1' : '');
  saveUserPreference('logsExcludeForeign', excludeForeign.value ? '1' : '');
  resetAndLoad();
});

watch(statusClass, (value) => {
  saveUserPreference('logsStatusClass', value);
  resetAndLoad();
});

watch(statusCodeInput, (value) => {
  saveUserPreference('logsStatusCode', value);
  if (value) {
    statusClass.value = '';
  }
  if (statusCodeTimer) {
    clearTimeout(statusCodeTimer);
  }
  statusCodeTimer = setTimeout(() => {
    resetAndLoad();
  }, 400);
});

onMounted(() => {
  loadWebsites();
});
</script>
