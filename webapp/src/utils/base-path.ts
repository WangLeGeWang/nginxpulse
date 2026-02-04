const BASE_PATH_KEY = '__NGINXPULSE_BASE_PATH__';

const normalizeBasePath = (value?: string): string => {
  if (!value) {
    return '';
  }
  let normalized = value.trim();
  if (!normalized) {
    return '';
  }
  if (normalized.startsWith('/')) {
    normalized = normalized.slice(1);
  }
  if (normalized.endsWith('/')) {
    normalized = normalized.slice(0, -1);
  }
  return normalized ? `/${normalized}` : '';
};

export const getWebBasePath = (): string => {
  if (typeof window === 'undefined') {
    return '';
  }
  const raw = (window as unknown as Record<string, string>)[BASE_PATH_KEY];
  return normalizeBasePath(raw);
};

export const getWebBasePathWithSlash = (): string => {
  const base = getWebBasePath();
  return base ? `${base}/` : '/';
};

export const getMobileBasePathWithSlash = (): string => {
  const base = getWebBasePath();
  return base ? `${base}/m/` : '/m/';
};
