import axios from 'axios';
import { i18n } from '@/i18n';
import { getWebBasePathWithSlash } from '@/utils';

const ACCESS_KEY_STORAGE = 'nginxpulse_access_key';
const ACCESS_KEY_HEADER = 'X-NginxPulse-Key';
const ACCESS_KEY_EVENT = 'nginxpulse:access-key-required';

const client = axios.create({
  baseURL: getWebBasePathWithSlash(),
  timeout: 15000,
  headers: {
    'X-Requested-With': 'XMLHttpRequest',
  },
});

client.interceptors.request.use((config) => {
  const accessKey = localStorage.getItem(ACCESS_KEY_STORAGE);
  if (accessKey) {
    config.headers[ACCESS_KEY_HEADER] = accessKey;
  }
  return config;
});

client.interceptors.response.use(
  (response) => response,
  (error) => {
    const status = error?.response?.status;
    const fallback = i18n.global.t('common.requestFailed');
    const message = error?.response?.data?.error || error?.message || fallback;
    if (status === 401) {
      window.dispatchEvent(
        new CustomEvent(ACCESS_KEY_EVENT, {
          detail: { message },
        })
      );
    }
    return Promise.reject(new Error(message));
  }
);

export default client;
