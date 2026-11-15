import { useEffect, useMemo, useState, type FormEvent } from 'react';
import { useAuthStore } from '@/stores';
import { BPSModelsPanel } from './BPSModelsPanel';
import { CpaBpsModelsPanel } from './CpaBpsModelsPanel';
import { loginTasksApi, type BPSLoginMode } from '@/services/api/loginTasks';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import {
  bpsSettingsApi,
  type BPSSettings,
  type BPSSettingsInput,
} from '@/services/api/bpsSettings';
import type { ApiClientRequestScope } from '@/services/api/client';

export function BPSSettingsPanel({ scope }: { scope: ApiClientRequestScope }) {
  const [settings, setSettings] = useState<BPSSettings>();
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  useEffect(() => {
    let disposed = false;
    bpsSettingsApi
      .get(scope)
      .then((next) => {
        if (!disposed) setSettings(next);
      })
      .catch((error: unknown) => {
        if (!disposed) setMessage(error instanceof Error ? error.message : '读取BPS设置失败');
      });
    return () => {
      disposed = true;
    };
  }, [scope]);
  const save = async (event: FormEvent) => {
    event.preventDefault();
    if (!settings) return;
    if (settings.image_transport === 'relay') {
      try {
        const url = new URL(settings.image_relay_base_url);
        if (
          url.protocol !== 'https:' ||
          url.username ||
          url.password ||
          (url.pathname !== '/' && url.pathname !== '') ||
          url.search ||
          url.hash
        )
          throw new Error();
      } catch {
        setMessage('中转模式需要有效的HTTPS基址，不含路径、凭据或查询');
        return;
      }
    }
    setBusy(true);
    setMessage('');
    const input: BPSSettingsInput = {
      enabled: settings.enabled,
      images_enabled: settings.images_enabled,
      image_transport: settings.image_transport,
      image_relay_base_url: settings.image_relay_base_url,
      connection_ref: settings.connection_ref,
    };
    try {
      await bpsSettingsApi.save(input, scope);
      setSettings(await bpsSettingsApi.get(scope));
      setMessage('设置已保存，对新请求即时生效');
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '保存回执未确认，请刷新核对');
    } finally {
      setBusy(false);
    }
  };
  return (
    <Card title="旧版 BPS 协议与图片输入">
      <p>
        仅作用于旧版 bps 通道，不控制 cpa-bps
        插件。保存影响后续新请求，正在生成的请求继续使用原设置。
      </p>
      {settings && (
        <form onSubmit={save} style={{ display: 'grid', gap: 16 }}>
          <ToggleSwitch
            label="启用 BPS 协议"
            checked={settings.enabled}
            onChange={(enabled) => setSettings({ ...settings, enabled })}
            disabled={busy}
          />
          <ToggleSwitch
            label="启用图片输入"
            checked={settings.images_enabled}
            onChange={(images_enabled) => setSettings({ ...settings, images_enabled })}
            disabled={busy}
          />
          <p>关闭图片后，含对话、工具截图或历史图片的新请求会明确报错。</p>
          <label>
            图片传输方式{' '}
            <select
              aria-label="图片传输方式"
              value={settings.image_transport}
              disabled={busy}
              onChange={(event) =>
                setSettings({
                  ...settings,
                  image_transport: event.target.value as 'native' | 'relay',
                })
              }
            >
              <option value="native">BPS 原生附件（默认）</option>
              <option value="relay">临时 HTTPS 中转</option>
            </select>
          </label>
          <Input
            label="图片中转 HTTPS 基址"
            value={settings.image_relay_base_url}
            placeholder="https://images.example.com"
            disabled={busy}
            onChange={(event) =>
              setSettings({ ...settings, image_relay_base_url: event.target.value })
            }
            hint="仅中转模式需要；中转图片由当前 CPA 临时保存和读取"
          />
          <p>图片有效期30分钟；每图20MiB、64MP；每请求20张、合计32MiB；临时存储1GiB、512项。</p>
          <Button type="submit" disabled={busy}>
            {busy ? '正在保存' : '保存 BPS 设置'}
          </Button>
        </form>
      )}
      {message && <p role="status">{message}</p>}
    </Card>
  );
}

export function BPSSettingsCard() {
  const apiBase = useAuthStore((state) => state.apiBase);
  const managementKey = useAuthStore((state) => state.managementKey);
  const scope = useMemo(() => ({ apiBase, managementKey }), [apiBase, managementKey]);
  return <BPSSettingsContent key={JSON.stringify([apiBase, managementKey])} scope={scope} />;
}

function BPSSettingsContent({ scope }: { scope: ApiClientRequestScope }) {
  const [mode, setMode] = useState<BPSLoginMode | 'loading'>('loading');
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    let disposed = false;
    loginTasksApi
      .capabilities(scope)
      .then((result) => {
        if (!disposed)
          setMode(
            result.bps_login_mode === 'plugin' || result.bps_login_mode === 'builtin'
              ? result.bps_login_mode
              : 'unavailable'
          );
      })
      .catch(() => {
        if (!disposed) setMode('unavailable');
      });
    return () => {
      disposed = true;
    };
  }, [scope, revision]);
  if (mode === 'plugin') return <CpaBpsModelsPanel scope={scope} />;
  if (mode !== 'builtin')
    return (
      <Card title="BPS 模型设置">
        <p role="status">
          {mode === 'loading'
            ? '正在确认当前 BPS 通道'
            : 'BPS 通道未确认，请重试；旧版设置未启用。'}
        </p>
        <Button
          disabled={mode === 'loading'}
          onClick={() => {
            setMode('loading');
            setRevision((value) => value + 1);
          }}
        >
          重新读取通道
        </Button>
      </Card>
    );
  return (
    <div style={{ display: 'grid', gap: 20 }}>
      <BPSSettingsPanel scope={scope} />
      <BPSModelsPanel scope={scope} />
    </div>
  );
}
