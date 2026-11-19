import { afterEach, describe, expect, it, vi } from 'vitest';
import { bindBpsManualMenu, validBpsCallback, validBpsStart } from './bpsManualMenu';

const state = 'test_only_state_for_bps_manual_menu_01';
const redirect = 'https://bps.openai.com/basispoints/extension/360590d7-f8f9-4d88-bf75-0edfe0a4b9f3/auth/callback';
const start = new URL('https://auth.openai.com/api/accounts/authorize');
start.searchParams.set('state', state);
start.searchParams.set('redirect_uri', redirect);
const callback = `${redirect}?state=${state}&code=test-only`;

afterEach(() => vi.unstubAllGlobals());

describe('原生页手动授权', () => {
  it('只打开当前流程的授权地址，不接受其他来源、回调或重复参数', () => {
    expect(validBpsStart({ url: start.href, state })).toEqual({ state, url: start.href });
    for (const value of [
      start.href.replace('auth.openai.com', 'example.invalid'),
      start.href.replace('bps.openai.com', 'example.invalid'),
      `${start.href}&state=${state}`,
    ]) expect(validBpsStart({ url: value, state })).toBeNull();
    expect(validBpsCallback(callback, state)).toBe(callback);
    for (const value of [
      callback.replace(state, 'other'),
      callback.replace('bps.openai.com', 'example.invalid'),
      `${callback}&code=other`,
      `${callback}&error=denied`,
    ]) expect(validBpsCallback(value, state)).toBeNull();
  });

  it('跨源、文档跳转或不同标记均不接管表单', () => {
    const location = new URL('https://panel.invalid');
    const asset = new URL('/v0/resource/plugins/cpa-bps/index.html', location).href;
    vi.stubGlobal('window', { location });
    for (const docURL of [asset, asset + '?redirect', asset.replace('panel.invalid', 'other.invalid')]) {
      const doc = { URL: docURL, querySelectorAll: () => [], getElementById: () => null };
      const iframe = { inert: false, contentDocument: doc, getAttribute: () => 'allow-same-origin' };
      expect(bindBpsManualMenu(iframe as unknown as HTMLIFrameElement, asset, { apiBase: location.origin, managementKey: 'test-only' })).toBeNull();
      expect(iframe.inert).toBe(true);
    }
  });
});
