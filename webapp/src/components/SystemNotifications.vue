<template>
  <div class="system-notice">
    <button class="system-notice-btn" type="button" :aria-label="t('notifications.title')" @click="toggleDialog">
      <i class="ri-notification-3-line" aria-hidden="true"></i>
      <span v-if="unreadCount > 0" class="system-notice-badge">{{ unreadLabel }}</span>
    </button>

    <Dialog
      v-model:visible="dialogVisible"
      :header="t('notifications.title')"
      modal
      :draggable="false"
      class="system-notice-dialog"
      @show="handleDialogOpen"
    >
      <div class="system-notice-body">
        <div v-if="loading" class="system-notice-loading">{{ t('common.loading') }}</div>
        <div v-else-if="notifications.length === 0" class="system-notice-empty">
          {{ t('notifications.empty') }}
        </div>
        <div v-else class="system-notice-list">
          <div
            v-for="notice in notifications"
            :key="notice.id"
            class="system-notice-item"
            :class="{ unread: !notice.read_at }"
          >
            <div class="system-notice-item-header">
              <span class="system-notice-title">{{ notice.title }}</span>
              <span class="system-notice-time">{{ formatNoticeTime(notice.last_occurred_at || notice.created_at) }}</span>
            </div>
            <div class="system-notice-message">{{ notice.message }}</div>
            <div class="system-notice-meta">
              <span v-if="(notice.occurrences || 0) > 1" class="system-notice-occurrence">
                {{ t('notifications.occurrence', { count: notice.occurrences }) }}
              </span>
              <button
                v-if="notice.category === 'ip_geo'"
                type="button"
                class="system-notice-link"
                @click="openFailureDialog"
              >
                {{ t('notifications.failureTitle') }}
              </button>
              <button
                v-if="!notice.read_at"
                type="button"
                class="system-notice-read"
                @click="markOneRead(notice)"
              >
                {{ t('notifications.markRead') }}
              </button>
            </div>
          </div>
        </div>
        <button
          v-if="hasMore"
          type="button"
          class="system-notice-load"
          :disabled="loadingMore"
          @click="loadMore"
        >
          {{ loadingMore ? t('common.loading') : t('notifications.loadMore') }}
        </button>
      </div>
      <div class="system-dialog-actions">
        <div class="system-dialog-actions-left">
          <Button
            outlined
            class="system-notice-action"
            :label="t('notifications.failureTitle')"
            @click="openFailureDialog"
          />
        </div>
        <div class="system-dialog-actions-right">
          <Button
            outlined
            severity="danger"
            class="system-notice-action"
            :label="t('notifications.clear')"
            :disabled="notificationsClearing || loading"
            @click="clearNotifications"
          />
          <Button
            outlined
            class="system-notice-action"
            :label="t('notifications.markAllRead')"
            :disabled="unreadCount === 0 || loading"
            @click="markAllRead"
          />
          <Button class="system-notice-action" :label="t('common.close')" @click="dialogVisible = false" />
        </div>
      </div>
    </Dialog>

    <Dialog
      v-model:visible="failureDialogVisible"
      :header="t('notifications.failureTitle')"
      modal
      :draggable="false"
      class="system-failure-dialog"
      @show="loadFailures(true)"
    >
      <div class="system-failure-body">
        <div class="system-failure-filters">
          <Dropdown
            v-model="failureWebsiteId"
            class="system-failure-select"
            :options="websiteOptions"
            optionLabel="label"
            optionValue="value"
            :placeholder="t('notifications.filterWebsite')"
          />
          <Dropdown
            v-model="failureReason"
            class="system-failure-select"
            :options="reasonOptions"
            optionLabel="label"
            optionValue="value"
            :placeholder="t('notifications.filterReason')"
          />
          <InputText
            v-model="failureKeyword"
            class="system-failure-input"
            :placeholder="t('notifications.filterKeyword')"
          />
          <Button class="system-failure-action" :label="t('common.search')" @click="loadFailures(true)" />
          <Button
            outlined
            class="system-failure-action"
            :label="t('common.reset')"
            @click="resetFailureFilters"
          />
        </div>
        <div v-if="failureLoading" class="system-notice-loading">{{ t('common.loading') }}</div>
        <div v-else-if="failures.length === 0" class="system-notice-empty">
          {{ t('notifications.failureEmpty') }}
        </div>
        <div v-else class="system-failure-list">
          <div class="system-failure-row system-failure-header">
            <span>{{ t('notifications.failureIP') }}</span>
            <span>{{ t('notifications.failureReason') }}</span>
            <span>{{ t('notifications.failureTime') }}</span>
          </div>
          <div v-for="item in failures" :key="item.id" class="system-failure-row">
            <span class="system-failure-ip">{{ item.ip }}</span>
            <span class="system-failure-reason">{{ item.reason }}</span>
            <span class="system-failure-time">{{ formatNoticeTime(item.created_at) }}</span>
          </div>
        </div>
        <button
          v-if="failureHasMore"
          type="button"
          class="system-notice-load"
          :disabled="failureLoadingMore"
          @click="loadMoreFailures"
        >
          {{ failureLoadingMore ? t('common.loading') : t('notifications.loadMore') }}
        </button>
      </div>
      <div class="system-dialog-actions">
        <div class="system-dialog-actions-left">
          <Button
            outlined
            class="system-notice-action"
            :label="t('notifications.export')"
            :disabled="failureExporting"
            @click="exportFailures"
          />
          <Button
            outlined
            severity="danger"
            class="system-notice-action"
            :label="t('notifications.clear')"
            :disabled="failureClearing || failureLoading"
            @click="clearFailures"
          />
        </div>
        <div class="system-dialog-actions-right">
          <Button class="system-notice-action" :label="t('common.close')" @click="failureDialogVisible = false" />
        </div>
      </div>
    </Dialog>

    <Dialog
      v-model:visible="confirmDialogVisible"
      :header="t('notifications.clearConfirmTitle')"
      modal
      :draggable="false"
      :closable="!confirmDialogLoading"
      class="system-confirm-dialog"
      @hide="resetConfirmDialogState"
    >
      <div class="system-confirm-content">
        <i class="ri-alert-line" aria-hidden="true"></i>
        <p>{{ confirmDialogMessage }}</p>
      </div>
      <div class="system-confirm-actions">
        <Button
          outlined
          :label="t('common.cancel')"
          :disabled="confirmDialogLoading"
          @click="closeConfirmDialog"
        />
        <Button
          severity="danger"
          :label="t('common.confirm')"
          :loading="confirmDialogLoading"
          :disabled="confirmDialogLoading"
          @click="runConfirmAction"
        />
      </div>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue';
import { useI18n } from 'vue-i18n';
import Dialog from 'primevue/dialog';
import Dropdown from 'primevue/dropdown';
import InputText from 'primevue/inputtext';
import {
  clearIPGeoFailures,
  clearSystemNotifications,
  exportIPGeoFailures,
  fetchIPGeoFailures,
  fetchSystemNotifications,
  fetchWebsites,
  markSystemNotificationsRead,
} from '@/api';
import type { IPGeoAPIFailure, SystemNotification, WebsiteInfo } from '@/api/types';

const { t } = useI18n({ useScope: 'global' });

const dialogVisible = ref(false);
const loading = ref(false);
const loadingMore = ref(false);
const notifications = ref<SystemNotification[]>([]);
const unreadCount = ref(0);
const page = ref(1);
const pageSize = 20;
const hasMore = ref(false);
let pollTimer: ReturnType<typeof setInterval> | null = null;
const failureDialogVisible = ref(false);
const failureLoading = ref(false);
const failureLoadingMore = ref(false);
const failures = ref<IPGeoAPIFailure[]>([]);
const failurePage = ref(1);
const failureHasMore = ref(false);
const failurePageSize = 50;
const failureWebsiteId = ref('');
const failureReason = ref('');
const failureKeyword = ref('');
const failureExporting = ref(false);
const failureClearing = ref(false);
const notificationsClearing = ref(false);
const websites = ref<WebsiteInfo[]>([]);
const confirmDialogVisible = ref(false);
const confirmDialogMessage = ref('');
const confirmDialogLoading = ref(false);
let confirmAction: (() => Promise<void>) | null = null;

const unreadLabel = computed(() => (unreadCount.value > 99 ? '99+' : `${unreadCount.value}`));
const websiteOptions = computed(() => [
  { label: t('common.all'), value: '' },
  ...websites.value.map((site) => ({ label: site.name, value: site.id })),
]);
const reasonOptions = computed(() => [
  { label: t('common.all'), value: '' },
  { label: t('notifications.reasonRequest'), value: 'request_error' },
  { label: t('notifications.reasonHttp'), value: 'http_status' },
  { label: t('notifications.reasonDecode'), value: 'decode_error' },
  { label: t('notifications.reasonApi'), value: 'api_fail' },
  { label: t('notifications.reasonUnknown'), value: 'unknown' },
]);

const toggleDialog = () => {
  dialogVisible.value = !dialogVisible.value;
};

const handleDialogOpen = () => {
  loadNotifications(true);
};

const formatNoticeTime = (value?: string) => {
  if (!value) {
    return '-';
  }
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return value;
  }
  return parsed.toLocaleString();
};

const refreshUnreadCount = async () => {
  try {
    const response = await fetchSystemNotifications(1, 1, true);
    unreadCount.value = response.unread_count ?? 0;
  } catch (error) {
    // 忽略未读数刷新失败
  }
};

const loadNotifications = async (reset = false) => {
  if (loading.value) {
    return;
  }
  loading.value = true;
  try {
    if (reset) {
      page.value = 1;
      notifications.value = [];
    }
    const response = await fetchSystemNotifications(page.value, pageSize, false);
    if (reset) {
      notifications.value = response.notifications || [];
    } else {
      notifications.value = notifications.value.concat(response.notifications || []);
    }
    hasMore.value = Boolean(response.has_more);
    unreadCount.value = response.unread_count ?? unreadCount.value;
  } finally {
    loading.value = false;
  }
};

const loadMore = async () => {
  if (loadingMore.value || !hasMore.value) {
    return;
  }
  loadingMore.value = true;
  try {
    page.value += 1;
    const response = await fetchSystemNotifications(page.value, pageSize, false);
    notifications.value = notifications.value.concat(response.notifications || []);
    hasMore.value = Boolean(response.has_more);
    unreadCount.value = response.unread_count ?? unreadCount.value;
  } finally {
    loadingMore.value = false;
  }
};

const openFailureDialog = () => {
  failureDialogVisible.value = true;
};

const loadFailures = async (reset = false) => {
  if (failureLoading.value) {
    return;
  }
  failureLoading.value = true;
  try {
    if (reset) {
      failurePage.value = 1;
      failures.value = [];
    }
    const response = await fetchIPGeoFailures(failurePage.value, failurePageSize, {
      websiteId: failureWebsiteId.value,
      reason: failureReason.value,
      keyword: failureKeyword.value,
    });
    if (reset) {
      failures.value = response.failures || [];
    } else {
      failures.value = failures.value.concat(response.failures || []);
    }
    failureHasMore.value = Boolean(response.has_more);
  } finally {
    failureLoading.value = false;
  }
};

const loadMoreFailures = async () => {
  if (failureLoadingMore.value || !failureHasMore.value) {
    return;
  }
  failureLoadingMore.value = true;
  try {
    failurePage.value += 1;
    const response = await fetchIPGeoFailures(failurePage.value, failurePageSize, {
      websiteId: failureWebsiteId.value,
      reason: failureReason.value,
      keyword: failureKeyword.value,
    });
    failures.value = failures.value.concat(response.failures || []);
    failureHasMore.value = Boolean(response.has_more);
  } finally {
    failureLoadingMore.value = false;
  }
};

const resetFailureFilters = () => {
  failureWebsiteId.value = '';
  failureReason.value = '';
  failureKeyword.value = '';
  loadFailures(true);
};

const exportFailures = async () => {
  if (failureExporting.value) {
    return;
  }
  failureExporting.value = true;
  try {
    const response = await exportIPGeoFailures({
      websiteId: failureWebsiteId.value,
      reason: failureReason.value,
      keyword: failureKeyword.value,
    });
    const blob = new Blob([response.data], { type: 'text/csv;charset=utf-8' });
    const url = window.URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `ip_geo_failures_${Date.now()}.csv`;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    window.URL.revokeObjectURL(url);
  } finally {
    failureExporting.value = false;
  }
};

const openConfirmDialog = (message: string, action: () => Promise<void>) => {
  confirmDialogMessage.value = message;
  confirmDialogVisible.value = true;
  confirmDialogLoading.value = false;
  confirmAction = action;
};

const closeConfirmDialog = () => {
  if (confirmDialogLoading.value) {
    return;
  }
  confirmDialogVisible.value = false;
};

const resetConfirmDialogState = () => {
  confirmDialogMessage.value = '';
  confirmDialogLoading.value = false;
  confirmAction = null;
};

const runConfirmAction = async () => {
  if (!confirmAction || confirmDialogLoading.value) {
    return;
  }
  confirmDialogLoading.value = true;
  try {
    await confirmAction();
    confirmDialogVisible.value = false;
  } finally {
    confirmDialogLoading.value = false;
    confirmAction = null;
  }
};

const clearFailures = async () => {
  if (failureClearing.value) {
    return;
  }
  openConfirmDialog(t('notifications.clearFailureConfirm'), async () => {
    failureClearing.value = true;
    try {
      await clearIPGeoFailures({});
      await loadFailures(true);
    } finally {
      failureClearing.value = false;
    }
  });
};

const markOneRead = async (notice: SystemNotification) => {
  if (!notice || notice.read_at) {
    return;
  }
  await markSystemNotificationsRead({ ids: [notice.id] });
  notice.read_at = new Date().toISOString();
  if (unreadCount.value > 0) {
    unreadCount.value -= 1;
  }
};

const markAllRead = async () => {
  if (unreadCount.value === 0) {
    return;
  }
  await markSystemNotificationsRead({ all: true });
  notifications.value = notifications.value.map((notice) => ({
    ...notice,
    read_at: notice.read_at || new Date().toISOString(),
  }));
  unreadCount.value = 0;
};

const clearNotifications = async () => {
  if (notificationsClearing.value) {
    return;
  }
  openConfirmDialog(t('notifications.clearNotificationsConfirm'), async () => {
    notificationsClearing.value = true;
    try {
      await clearSystemNotifications({ all: true });
      page.value = 1;
      notifications.value = [];
      hasMore.value = false;
      unreadCount.value = 0;
      await loadNotifications(true);
    } finally {
      notificationsClearing.value = false;
    }
  });
};

onMounted(() => {
  fetchWebsites().then((data) => {
    websites.value = data || [];
  });
  refreshUnreadCount();
  pollTimer = setInterval(refreshUnreadCount, 30000);
});

onUnmounted(() => {
  if (pollTimer) {
    clearInterval(pollTimer);
    pollTimer = null;
  }
});
</script>

<style scoped>
.system-notice {
  position: relative;
}

.system-notice-btn {
  position: relative;
  width: 40px;
  height: 40px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border);
  background: var(--panel);
  color: var(--text);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: border-color 0.2s ease, background 0.2s ease, color 0.2s ease;
}

.system-notice-btn:hover {
  border-color: rgba(var(--primary-color-rgb), 0.5);
  color: var(--accent-color);
}

.system-notice-badge {
  position: absolute;
  top: -6px;
  right: -6px;
  min-width: 18px;
  height: 18px;
  padding: 0 6px;
  border-radius: var(--radius-pill);
  background: var(--error-color);
  color: #fff;
  font-size: 11px;
  font-weight: 700;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  box-shadow: 0 4px 10px rgba(0, 0, 0, 0.15);
}

.system-notice-body {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.system-notice-loading,
.system-notice-empty {
  padding: 12px 4px;
  color: var(--muted);
  text-align: center;
}

.system-notice-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
  max-height: 360px;
  overflow: auto;
  padding-right: 4px;
}

.system-notice-item {
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 10px 12px;
  background: var(--panel);
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.system-notice-item.unread {
  border-color: rgba(var(--primary-color-rgb), 0.5);
  background: rgba(var(--primary-color-rgb), 0.08);
}

.system-notice-item-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.system-notice-title {
  font-weight: 600;
  font-size: 13px;
}

.system-notice-time {
  font-size: 12px;
  color: var(--muted);
}

.system-notice-message {
  font-size: 13px;
  color: var(--text);
  line-height: 1.5;
}

.system-notice-meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  font-size: 12px;
  color: var(--muted);
}

.system-notice-link {
  border: none;
  background: transparent;
  color: var(--accent-color);
  font-weight: 600;
  cursor: pointer;
}

.system-notice-link:hover {
  text-decoration: underline;
}

.system-notice-read {
  border: none;
  background: transparent;
  color: var(--accent-color);
  font-weight: 600;
  cursor: pointer;
}

.system-notice-read:hover {
  text-decoration: underline;
}

.system-notice-load {
  border: 1px dashed rgba(var(--primary-color-rgb), 0.4);
  border-radius: var(--radius-xs);
  padding: 8px 12px;
  background: rgba(var(--primary-color-rgb), 0.06);
  color: var(--accent-color);
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
}

.system-notice-load:disabled {
  opacity: 0.6;
  cursor: default;
}

.system-dialog-actions {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  margin-top: 12px;
}

.system-dialog-actions-left,
.system-dialog-actions-right {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.system-notice-action {
  min-width: 96px;
}

:global(.system-notice-dialog) {
  width: 720px;
  max-width: 94vw;
}

:global(.system-failure-dialog) {
  width: 920px;
  max-width: 96vw;
}

.system-failure-body {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.system-failure-filters {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}

.system-failure-select {
  min-width: 140px;
}

.system-failure-input {
  min-width: 180px;
}

.system-failure-action {
  min-width: 90px;
}

.system-failure-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  max-height: 360px;
  overflow: auto;
}

.system-failure-row {
  display: grid;
  grid-template-columns: minmax(140px, 1.2fr) minmax(100px, 1fr) minmax(140px, 1fr);
  gap: 12px;
  font-size: 12px;
  padding: 8px 10px;
  border-radius: var(--radius-xs);
  border: 1px solid var(--border);
  background: var(--panel);
}

.system-failure-header {
  font-weight: 600;
  background: var(--panel-muted);
}

.system-failure-ip {
  font-weight: 600;
}

.system-failure-time {
  color: var(--muted);
}

:global(.system-confirm-dialog) {
  width: 420px;
  max-width: 92vw;
}

.system-confirm-content {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  color: var(--text);
  line-height: 1.55;
}

.system-confirm-content i {
  color: var(--warning-color);
  font-size: 18px;
  margin-top: 1px;
}

.system-confirm-content p {
  margin: 0;
}

.system-confirm-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 14px;
}
</style>
