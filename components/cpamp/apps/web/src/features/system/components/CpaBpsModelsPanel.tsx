import { useEffect, useState } from 'react';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { ModelDiscoveryNotice } from '@/features/authFiles/components/ModelDiscoveryNotice';
import { authFilesApi, type ModelDiscoveryState } from '@/services/api/authFiles';
import { pluginsApi } from '@/services/api/plugins';
import type { ApiClientRequestScope } from '@/services/api/client';
import type { AuthFileItem, PluginConfigObject } from '@/types';

const credentialKey = (file: AuthFileItem) =>
  JSON.stringify([file.name, file.id ?? '', file.authIndex ?? file.auth_index ?? '']);

export function CpaBpsModelsPanel({ scope }: { scope: ApiClientRequestScope }) {
  return (
    <PluginModelsContent key={JSON.stringify([scope.apiBase, scope.managementKey])} scope={scope} />
  );
}

function PluginModelsContent({ scope }: { scope: ApiClientRequestScope }) {
  const [files, setFiles] = useState<AuthFileItem[]>([]);
  const [config, setConfig] = useState<PluginConfigObject>();
  const [selected, setSelected] = useState('');
  const [models, setModels] = useState<string[]>();
  const [discovery, setDiscovery] = useState<ModelDiscoveryState>();
  const [loading, setLoading] = useState(true);
  const [modelLoading, setModelLoading] = useState(true);
  const [error, setError] = useState('');
  const [modelError, setModelError] = useState('');
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    let disposed = false;
    Promise.all([authFilesApi.list(scope), pluginsApi.getConfig('cpa-bps', scope)])
      .then(([inventory, settings]) => {
        if (disposed) return;
        const next = inventory.files.filter(
          (file) =>
            String(file.provider || file.type || '')
              .trim()
              .toLowerCase() === 'cpa-bps'
        );
        setFiles(next);
        setConfig(settings);
        setSelected((previous) =>
          next.some((file) => credentialKey(file) === previous)
            ? previous
            : next[0]
              ? credentialKey(next[0])
              : ''
        );
      })
      .catch(() => {
        if (!disposed) setError('读取插件凭证或配置失败，请刷新；当前状态未确认。');
      })
      .finally(() => {
        if (!disposed) setLoading(false);
      });
    return () => {
      disposed = true;
    };
  }, [scope, revision]);
  const file = files.find((item) => credentialKey(item) === selected);
  useEffect(() => {
    let disposed = false;
    if (!file) return;
    authFilesApi
      .getModelsWithDiscovery(String(file.id || file.name), scope)
      .then((result) => {
        if (!disposed) {
          setModels([...new Set(result.models.map((item) => item.id))]);
          setDiscovery(result.discovery);
        }
      })
      .catch(() => {
        if (!disposed) setModelError('读取本站注册目录失败；模型状态未知，请刷新。');
      })
      .finally(() => {
        if (!disposed) setModelLoading(false);
      });
    return () => {
      disposed = true;
    };
  }, [file, scope]);
  const manualModels = Array.isArray(config?.models)
    ? config.models.filter((id): id is string => typeof id === 'string' && Boolean(id.trim()))
    : [];
  const mode = !config
    ? '未确认'
    : manualModels.length > 0
      ? `手动列表（${manualModels.length} 个）`
      : config.auto_discover === true
        ? '自动发现并发布'
        : config.auto_discover === false
          ? '自动发现已关闭，手动列表为空'
          : '自动发现配置未确认';
  const refresh = () => {
    setLoading(true);
    setFiles([]);
    setConfig(undefined);
    setModels(undefined);
    setDiscovery(undefined);
    setModelError('');
    setError('');
    setModelLoading(true);
    setRevision((value) => value + 1);
  };
  return (
    <Card title="BPS 模型设置（cpa-bps 插件）">
      <p>当前使用 cpa-bps 插件，旧版 BPS 模型清单、协议与图片开关不作用于此通道。</p>
      <p>
        模型发布方式：{mode}。自动模式在 OAuth
        保存注册后发现并发布账号支持的模型；空手动列表不等于清空注册目录。
      </p>
      <label>
        插件凭证{' '}
        <select
          aria-label="BPS插件凭证"
          value={selected}
          disabled={loading || !files.length}
          onChange={(event) => {
            setModels(undefined);
            setDiscovery(undefined);
            setModelError('');
            setModelLoading(true);
            setSelected(event.target.value);
          }}
        >
          {!files.length && (
            <option value="">
              {loading ? '正在读取插件凭证' : error ? '凭证状态未确认' : '暂无 cpa-bps 插件凭证'}
            </option>
          )}
          {files.map((item) => (
            <option key={credentialKey(item)} value={credentialKey(item)}>
              {item.name}
            </option>
          ))}
        </select>
      </label>
      {file && (
        <p>
          本凭证本站已注册：{models?.length ?? (modelLoading ? '读取中' : '未确认')}
          。注册目录不代表实时官方权限、剩余额度或推理成功。
        </p>
      )}
      {file && !modelLoading && !modelError && (
        <ModelDiscoveryNotice
          discovery={discovery}
          modelCount={models?.length ?? 0}
          disabled={file.disabled === true}
        />
      )}
      {models?.length === 0 && discovery?.status !== 'failed' && (
        <p>此凭证尚无本站注册模型；请核对插件配置与账号注册状态，不要反复重新登录。</p>
      )}
      {models && (
        <ul>
          {models.map((id) => (
            <li key={id}>{id}</li>
          ))}
        </ul>
      )}
      {error && <p role="status">{error}</p>}
      {modelError && <p role="status">{modelError}</p>}
      <Button variant="secondary" disabled={loading} onClick={refresh}>
        刷新凭证与注册目录
      </Button>
      <p>
        <a href="#/plugins">前往插件管理 → cpa-bps → 配置</a>
      </p>
      <p>
        本页仅读取本站目录和插件配置，不重新授权、不查询官方额度、不发推理请求，也不修改模型发布规则。
      </p>
    </Card>
  );
}
