<template>
  <div class="app-shell" :class="{ 'setup-shell': setupRequired }">
    <SetupPage v-if="setupRequired" />
    <template v-else>
      <aside class="sidebar">
        <div class="brand">
          <div class="brand-mark" aria-hidden="true">
            <span class="brand-initials">NP</span>
            <svg class="brand-pulse" viewBox="0 0 32 16" role="presentation" aria-hidden="true">
              <path
                d="M1 8H7L10 3L14 13L18 8H31"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                stroke-linecap="round"
                stroke-linejoin="round"
              ></path>
            </svg>
          </div>
          <div class="brand-text">
            <div class="brand-title">NginxPulse</div>
            <div class="brand-sub">{{ t('app.brand.subtitle') }}</div>
          </div>
        </div>
      <nav class="menu">
        <RouterLink to="/" class="menu-item" :class="{ active: isActive('/') }">{{ t('app.menu.overview') }}</RouterLink>
        <RouterLink to="/daily" class="menu-item" :class="{ active: isActive('/daily') }">{{ t('app.menu.daily') }}</RouterLink>
        <RouterLink to="/realtime" class="menu-item" :class="{ active: isActive('/realtime') }">{{ t('app.menu.realtime') }}</RouterLink>
        <RouterLink to="/logs" class="menu-item" :class="{ active: isActive('/logs') }">{{ t('app.menu.logs') }}</RouterLink>
        <RouterLink to="/settings" class="menu-item" :class="{ active: isActive('/settings') }">{{ t('app.menu.setup') }}</RouterLink>
      </nav>
      <div class="sidebar-language-compact" role="group" :aria-label="t('app.sidebar.language')" :key="currentLocale">
        <button
          v-for="option in languageOptions"
          :key="option.value"
          class="sidebar-language-btn"
          :class="{ active: option.value === currentLocale }"
          type="button"
          :aria-pressed="option.value === currentLocale"
          :aria-label="option.label"
          @click="currentLocale = option.value"
        >
          <i :class="['language-icon', option.icon]" aria-hidden="true"></i>
          <span>{{ option.shortLabel }}</span>
        </button>
      </div>
      <div class="sidebar-footer">
        <template v-if="isActive('/')">
          <div class="sidebar-label">{{ t('app.sidebar.recentActive') }}</div>
          <div class="sidebar-metric">
            <div class="sidebar-metric-value">{{ liveVisitorText }}</div>
            <div class="sidebar-metric-label">{{ t('app.sidebar.recentActiveHint') }}</div>
          </div>
        </template>
        <template v-else>
          <div class="sidebar-label">{{ sidebarLabel }}</div>
          <div class="sidebar-hint">{{ sidebarHint }}</div>
        </template>
        <div class="sidebar-language-toggle">
          <div class="sidebar-language-label">{{ t('app.sidebar.language') }}</div>
          <div class="sidebar-language-group" role="group" :aria-label="t('app.sidebar.language')" :key="currentLocale">
            <button
              v-for="option in languageOptions"
              :key="option.value"
              class="sidebar-language-btn"
              :class="{ active: option.value === currentLocale }"
              type="button"
              :aria-pressed="option.value === currentLocale"
              :aria-label="option.label"
              @click="currentLocale = option.value"
            >
              <i :class="['language-icon', option.icon]" aria-hidden="true"></i>
              <span>{{ option.shortLabel }}</span>
            </button>
          </div>
        </div>
        <div v-if="versionText" class="app-version">
          <span class="app-version-dot" aria-hidden="true"></span>
          <span>{{ versionText }}</span>
        </div>
      </div>
    </aside>

      <main class="main-content" :class="[mainClass, { 'parsing-lock': parsingActive }]">
        <div v-if="demoMode" class="demo-mode-banner">
          <span class="demo-mode-badge">{{ t('demo.badge') }}</span>
          <span class="demo-mode-text">
            {{ t('demo.text') }}
            <a href="https://github.com/likaia/nginxpulse/" target="_blank" rel="noopener">https://github.com/likaia/nginxpulse/</a>
          </span>
        </div>
        <RouterView :key="`${route.fullPath}-${currentLocale}-${accessKeyReloadToken}`" />
      </main>

      <div v-if="accessKeyRequired" class="access-gate">
        <div class="access-card">
          <div class="access-title">{{ t('access.title') }}</div>
          <div class="access-sub">{{ t('access.subtitle') }}</div>
          <form class="access-form" @submit.prevent="submitAccessKey">
            <input
              v-model="accessKeyInput"
              class="access-input"
              type="password"
              autocomplete="current-password"
              :placeholder="t('access.placeholder')"
            />
            <button class="access-submit" type="submit" :disabled="accessKeySubmitting">
              {{ accessKeySubmitting ? t('access.submitting') : t('access.submit') }}
            </button>
          </form>
          <div v-if="accessKeyErrorMessage" class="access-error">{{ accessKeyErrorMessage }}</div>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, provide, ref, watch } from 'vue';
import { RouterLink, RouterView, useRoute } from 'vue-router';
import { usePrimeVue } from 'primevue/config';
import { useI18n } from 'vue-i18n';
import { fetchAppStatus } from '@/api';
import { getLocaleFromQuery, getStoredLocale, normalizeLocale, setLocale } from '@/i18n';
import { primevueLocales } from '@/i18n/primevue';
import SetupPage from '@/pages/SetupPage.vue';

const route = useRoute();
const primevue = usePrimeVue();
const { t, n, locale } = useI18n({ useScope: 'global' });

const ACCESS_KEY_STORAGE = 'nginxpulse_access_key';
const ACCESS_KEY_EVENT = 'nginxpulse:access-key-required';

const sidebarLabel = computed(() => {
  const key = route.meta.sidebarLabelKey as string;
  return key ? t(key) : '';
});
const sidebarHint = computed(() => {
  const key = route.meta.sidebarHintKey as string;
  return key ? t(key) : '';
});
const mainClass = computed(() => (route.meta.mainClass as string) || '');

const isActive = (path: string) => route.path === path;

const isDark = ref(localStorage.getItem('darkMode') === 'true');
const parsingActive = ref(false);
const liveVisitorCount = ref<number | null>(null);
const demoMode = ref(false);
const migrationRequired = ref(false);
const setupRequired = ref(false);
const appVersion = ref('');
const accessKeyRequired = ref(false);
const accessKeySubmitting = ref(false);
const accessKeyInput = ref(localStorage.getItem(ACCESS_KEY_STORAGE) || '');
const accessKeyErrorKey = ref<string | null>(null);
const accessKeyErrorText = ref('');
const accessKeyReloadToken = ref(0);

const languageOptions = computed(() => {
  const _locale = locale.value;
  return [
    { value: 'zh-CN', label: t('language.zh'), shortLabel: t('language.zhShort'), icon: 'ri-translate-2' },
    { value: 'en-US', label: t('language.en'), shortLabel: t('language.enShort'), icon: 'ri-global-line' },
  ];
});

const currentLocale = computed({
  get: () => normalizeLocale(locale.value),
  set: (value: string) => setLocale(normalizeLocale(value)),
});

const applyTheme = (value: boolean) => {
  if (value) {
    document.body.classList.add('dark-mode');
    document.documentElement.classList.add('dark-mode');
    localStorage.setItem('darkMode', 'true');
  } else {
    document.body.classList.remove('dark-mode');
    document.documentElement.classList.remove('dark-mode');
    localStorage.setItem('darkMode', 'false');
  }
};

const toggleTheme = () => {
  isDark.value = !isDark.value;
};

onMounted(() => {
  applyTheme(isDark.value);
  refreshAppStatus();
  window.addEventListener(ACCESS_KEY_EVENT, handleAccessKeyEvent);
});

onBeforeUnmount(() => {
  window.removeEventListener(ACCESS_KEY_EVENT, handleAccessKeyEvent);
});

watch(isDark, (value) => {
  applyTheme(value);
});

watch(locale, (value) => {
  const normalized = normalizeLocale(value);
  primevue.config.locale = primevueLocales[normalized];
});

provide('theme', {
  isDark,
  toggle: toggleTheme,
});

provide('setParsingActive', (value: boolean) => {
  parsingActive.value = value;
});

provide('setLiveVisitorCount', (value: number | null) => {
  liveVisitorCount.value = value;
});

provide('demoMode', demoMode);
provide('migrationRequired', migrationRequired);

async function refreshAppStatus() {
  try {
    const status = await fetchAppStatus();
    demoMode.value = Boolean(status.demo_mode);
    migrationRequired.value = Boolean(status.migration_required);
    setupRequired.value = Boolean(status.setup_required);
    appVersion.value = status.version ?? '';
    accessKeyRequired.value = false;
    accessKeyErrorKey.value = null;
    accessKeyErrorText.value = '';
    const hasStoredLocale = getStoredLocale() !== null;
    const hasQueryLocale = getLocaleFromQuery() !== null;
    if (!hasStoredLocale && !hasQueryLocale && status.language) {
      setLocale(normalizeLocale(status.language), false);
    }
  } catch (error) {
    const message = error instanceof Error ? error.message : t('common.requestFailed');
    if (message.toLowerCase().includes('key') || message.includes('密钥')) {
      accessKeyRequired.value = true;
      setAccessKeyErrorMessage(message);
    } else {
      console.error('获取系统状态失败:', error);
    }
  }
}

function handleAccessKeyEvent(event: Event) {
  const detail = (event as CustomEvent<{ message?: string }>).detail;
  accessKeyRequired.value = true;
  setAccessKeyErrorMessage(detail?.message || '');
}

async function submitAccessKey() {
  const value = accessKeyInput.value.trim();
  if (!value) {
    accessKeyErrorKey.value = 'access.required';
    accessKeyErrorText.value = '';
    return;
  }
  accessKeySubmitting.value = true;
  localStorage.setItem(ACCESS_KEY_STORAGE, value);
  try {
    await refreshAppStatus();
    if (!accessKeyRequired.value) {
      accessKeyReloadToken.value += 1;
    }
  } finally {
    accessKeySubmitting.value = false;
  }
}

function setAccessKeyErrorMessage(message: string) {
  const normalized = message.trim().toLowerCase();
  if (!message || normalized.includes('需要访问密钥') || normalized.includes('access key required')) {
    accessKeyErrorKey.value = 'access.title';
    accessKeyErrorText.value = '';
    return;
  }
  if (normalized.includes('访问密钥无效') || normalized.includes('invalid')) {
    accessKeyErrorKey.value = 'access.invalid';
    accessKeyErrorText.value = '';
    return;
  }
  accessKeyErrorKey.value = null;
  accessKeyErrorText.value = message;
}

const liveVisitorText = computed(() =>
  Number.isFinite(liveVisitorCount.value ?? NaN)
    ? n(liveVisitorCount.value as number)
    : '--'
);

const versionText = computed(() => appVersion.value || '');
const accessKeyErrorMessage = computed(() => {
  if (accessKeyErrorKey.value) {
    return t(accessKeyErrorKey.value);
  }
  return accessKeyErrorText.value;
});
</script>

<style lang="scss" scoped>
.demo-mode-banner {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
  margin-bottom: 16px;
  border-radius: 14px;
  border: 1px solid rgba(239, 68, 68, 0.2);
  background: rgba(239, 68, 68, 0.08);
  color: #991b1b;
  font-size: 13px;
  font-weight: 500;
  box-shadow: var(--shadow-soft);
}

.demo-mode-badge {
  display: inline-flex;
  align-items: center;
  padding: 4px 10px;
  border-radius: 999px;
  background: rgba(239, 68, 68, 0.14);
  color: #b91c1c;
  font-weight: 700;
  font-size: 12px;
  letter-spacing: 0.4px;
}

.demo-mode-text {
  color: inherit;
  line-height: 1.5;
}

.demo-mode-text a {
  color: inherit;
  text-decoration: underline;
  text-underline-offset: 3px;
}

.access-gate {
  position: fixed;
  inset: 0;
  display: grid;
  place-items: center;
  padding: 24px;
  background: rgba(15, 23, 42, 0.35);
  backdrop-filter: blur(10px);
  z-index: 50;
}

.access-card {
  width: min(420px, 100%);
  background: var(--panel);
  border-radius: 22px;
  border: 1px solid var(--border);
  box-shadow: var(--shadow);
  padding: 28px;
  text-align: center;
}

.access-title {
  font-size: 20px;
  font-weight: 700;
  margin-bottom: 6px;
}

.access-sub {
  font-size: 13px;
  color: var(--muted);
  margin-bottom: 18px;
}

.access-form {
  display: grid;
  gap: 12px;
}

.access-input {
  width: 100%;
  padding: 12px 14px;
  border-radius: 14px;
  border: 1px solid var(--border);
  background: var(--input-bg);
  color: var(--text);
  font-size: 14px;
  outline: none;
}

.access-input:focus {
  border-color: rgba(var(--primary-color-rgb), 0.6);
  box-shadow: 0 0 0 3px rgba(var(--primary-color-rgb), 0.15);
}

.access-submit {
  border: none;
  border-radius: 14px;
  padding: 12px 14px;
  font-size: 14px;
  font-weight: 600;
  color: #fff;
  background: linear-gradient(135deg, var(--primary) 0%, var(--primary-strong) 100%);
  cursor: pointer;
  transition: transform 0.2s ease, box-shadow 0.2s ease;
  box-shadow: var(--shadow-soft);
}

.access-submit:hover {
  transform: translateY(-1px);
}

.access-submit:disabled {
  cursor: default;
  opacity: 0.75;
  transform: none;
}

.access-error {
  margin-top: 12px;
  font-size: 12px;
  color: var(--error-color);
}

.app-version {
  margin-top: 14px;
  font-size: 11px;
  color: var(--muted);
  letter-spacing: 0.02em;
  display: flex;
  gap: 6px;
  align-items: center;
}

.app-version-dot {
  width: 6px;
  height: 6px;
  border-radius: 999px;
  background: var(--primary);
  box-shadow: 0 0 0 3px var(--primary-soft);
}
</style>
