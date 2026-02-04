export const formatDate = (date: Date): string => {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, '0');
  const day = String(date.getDate()).padStart(2, '0');
  return `${year}-${month}-${day}`;
};

export const formatTraffic = (traffic: number): string => {
  if (traffic < 1024) {
    return `${traffic.toFixed(2)} B`;
  }
  if (traffic < 1024 * 1024) {
    return `${(traffic / 1024).toFixed(2)} KB`;
  }
  if (traffic < 1024 * 1024 * 1024) {
    return `${(traffic / (1024 * 1024)).toFixed(2)} MB`;
  }
  if (traffic < 1024 * 1024 * 1024 * 1024) {
    return `${(traffic / (1024 * 1024 * 1024)).toFixed(2)} GB`;
  }
  return `${(traffic / (1024 * 1024 * 1024 * 1024)).toFixed(2)} TB`;
};

export const saveUserPreference = (key: string, value: string): void => {
  localStorage.setItem(key, value);
};

export const getUserPreference = <T extends string>(key: string, defaultValue: T): T => {
  const saved = localStorage.getItem(key);
  return (saved || defaultValue) as T;
};

export { getMobileBasePathWithSlash, getWebBasePath, getWebBasePathWithSlash } from './base-path';
