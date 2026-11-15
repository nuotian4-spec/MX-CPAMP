import { parseDocument } from 'yaml';
import type { ClientKeyChannel, ClientKeyScope } from '@/types/visualConfig';

export type ClientKeyMutation =
  | { type: 'create'; apiKey: string; channel?: ClientKeyChannel }
  | { type: 'replace'; oldApiKey: string; newApiKey: string; channel?: ClientKeyChannel }
  | { type: 'delete'; apiKey: string };

const record = (value: unknown): Record<string, unknown> | undefined =>
  value !== null && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined;

export function readClientKeyConfig(root: Record<string, unknown>) {
  const access = record(root.access);
  const provider = record(record(record(root.auth)?.providers)?.['config-api-key']);
  const rawKeys =
    access && Object.prototype.hasOwnProperty.call(access, 'api-keys')
      ? access['api-keys']
      : Array.isArray(root['api-keys'])
        ? root['api-keys']
        : (provider?.['api-key-entries'] ?? provider?.['api-keys'] ?? []);
  if (rawKeys != null && !Array.isArray(rawKeys)) throw new Error('客户端Key列表格式错误');
  const keys = ((rawKeys ?? []) as unknown[]).map((item) => {
    const value =
      typeof item === 'string'
        ? item
        : (record(item)?.['api-key'] ??
          record(item)?.apiKey ??
          record(item)?.key ??
          record(item)?.Key);
    if (typeof value !== 'string' || !value.trim()) throw new Error('客户端Key为空或格式错误');
    return value.trim();
  });
  if (new Set(keys).size !== keys.length) throw new Error('客户端Key重复');
  const rawScopes =
    access && Object.prototype.hasOwnProperty.call(access, 'client-key-scopes')
      ? access['client-key-scopes']
      : (root['client-key-scopes'] ?? []);
  if (!Array.isArray(rawScopes)) throw new Error('客户端通道绑定格式错误');
  const seen = new Set<string>();
  const scopes: ClientKeyScope[] = rawScopes.map((item) => {
    const entry = record(item);
    const key = typeof entry?.['api-key'] === 'string' ? entry['api-key'].trim() : '';
    const channel = entry?.channel;
    if (!keys.includes(key) || seen.has(key))
      throw new Error('客户端通道绑定重复或引用不存在的Key');
    if (channel !== 'kilo' && channel !== 'codex' && channel !== 'bps')
      throw new Error('客户端通道无效');
    seen.add(key);
    return { ...entry, 'api-key': key, channel };
  });
  return { keys, scopes };
}

export function readClientKeyYaml(yaml: string) {
  const doc = parseDocument(yaml, { merge: true });
  if (doc.errors.length) throw new Error('配置YAML格式错误');
  const root = record(doc.toJS());
  if (!root) throw new Error('配置必须是映射');
  return { doc, root, ...readClientKeyConfig(root) };
}

export function mutateClientKeyYaml(yaml: string, mutation: ClientKeyMutation): string {
  const { doc, root, keys, scopes } = readClientKeyYaml(yaml);
  const oldKey = mutation.type === 'replace' ? mutation.oldApiKey.trim() : mutation.apiKey.trim();
  const newKey = mutation.type === 'replace' ? mutation.newApiKey.trim() : oldKey;
  if (!newKey) throw new Error('客户端Key为空');
  if (mutation.type !== 'create' && !keys.includes(oldKey))
    throw new Error('客户端Key已变化，请刷新');
  if (
    mutation.type === 'create' ? keys.includes(newKey) : newKey !== oldKey && keys.includes(newKey)
  ) {
    throw new Error('客户端Key重复');
  }
  const oldScope = scopes.find((scope) => scope['api-key'] === oldKey);
  const channel = mutation.type === 'delete' ? '' : (mutation.channel ?? oldScope?.channel ?? '');
  if (!['', 'kilo', 'codex', 'bps'].includes(channel)) throw new Error('客户端通道无效');
  const createsKey =
    mutation.type === 'create' || (mutation.type === 'replace' && oldKey !== newKey);
  if (mutation.type !== 'delete' && !channel && (createsKey || oldScope))
    throw new Error('新增或轮换Key必须选择Codex、CPA-BPS或Kilo通道；撤销请删除Key');
  const nextKeys =
    mutation.type === 'create'
      ? [...keys, newKey]
      : mutation.type === 'delete'
        ? keys.filter((key) => key !== oldKey)
        : keys.map((key) => (key === oldKey ? newKey : key));
  const nextScopes = scopes.filter((scope) => scope['api-key'] !== oldKey);
  if (channel) nextScopes.push({ ...oldScope, 'api-key': newKey, channel });
  doc.set(
    'access',
    doc.createNode({
      ...record(root.access),
      'api-keys': nextKeys,
      'client-key-scopes': nextScopes,
    })
  );
  if (Array.isArray(root['api-keys'])) doc.delete('api-keys');
  doc.delete('client-key-scopes');
  return doc.toString({ indent: 2, lineWidth: 120 });
}
