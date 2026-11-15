/**
 * 配置文件相关 API（/config.yaml）
 */

import { apiClient, createScopedApiRequestConfig, type ApiClientRequestScope } from './client';

export type ConfigFileRequest = {
  scope?: ApiClientRequestScope;
  signal?: AbortSignal;
  etag?: string;
  connectionRef?: string;
};

async function fetchSnapshot(options: ConfigFileRequest = {}) {
  const request = options.scope ? createScopedApiRequestConfig(options.scope) : {};
  const response = await apiClient.getRaw('/config.yaml', {
    ...request,
    signal: options.signal,
    responseType: 'text',
    headers: {
      ...request.headers,
      Accept: 'application/yaml, text/yaml, text/plain',
      'X-CPA-Connection-Ref': options.connectionRef || 'required',
    },
  });
  if (typeof response.data !== 'string') throw new Error('配置响应格式错误');
  return {
    yaml: response.data,
    etag: String(response.headers?.etag ?? ''),
    connectionRef: String(response.headers?.['x-cpa-connection-ref'] ?? ''),
  };
}

export const configFileApi = {
  fetchSnapshot,
  async fetchConfigYaml(): Promise<string> {
    const response = await apiClient.getRaw('/config.yaml', {
      responseType: 'text',
      headers: { Accept: 'application/yaml, text/yaml, text/plain' },
    });
    const data: unknown = response.data;
    if (typeof data === 'string') return data;
    if (data === undefined || data === null) return '';
    return String(data);
  },

  async saveConfigYaml(content: string, options: ConfigFileRequest = {}): Promise<void> {
    const request = options.scope ? createScopedApiRequestConfig(options.scope) : {};
    await apiClient.put('/config.yaml', content, {
      ...request,
      signal: options.signal,
      headers: {
        ...request.headers,
        ...(options.etag ? { 'If-Match': options.etag } : {}),
        ...(options.connectionRef ? { 'X-CPA-Connection-Ref': options.connectionRef } : {}),
        'Content-Type': 'application/yaml',
        Accept: 'application/json, text/plain, */*',
      },
    });
  },
};
