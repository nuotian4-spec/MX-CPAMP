import { describe, expect, it } from 'vitest';
import { parse } from 'yaml';
import { mutateClientKeyYaml, readClientKeyYaml } from './clientKeyConfig';

const input = `config-version: 8
access:
  api-keys: [old, k, c, b]
  client-key-scopes:
    - {api-key: k, channel: kilo, note: keep}
    - {api-key: c, channel: codex}
    - {api-key: b, channel: bps}
  custom: {keep: true}
api-keys: {codex: [{api-key: upstream}]}
unknown: {nested: null}
`;

describe('客户端Key同一配置事务', () => {
  it('替换保归属、删除撤引用、禁止仅解除scope', () => {
    const replaced = mutateClientKeyYaml(input, {
      type: 'replace',
      oldApiKey: 'k',
      newApiKey: 'new-k',
    });
    expect(readClientKeyYaml(replaced).scopes).toContainEqual({
      'api-key': 'new-k',
      channel: 'kilo',
      note: 'keep',
    });
    const deleted = mutateClientKeyYaml(replaced, { type: 'delete', apiKey: 'c' });
    expect(readClientKeyYaml(deleted).keys).toEqual(['old', 'new-k', 'b']);
    expect(readClientKeyYaml(deleted).scopes.some((scope) => scope['api-key'] === 'c')).toBe(false);
    expect(() =>
      mutateClientKeyYaml(deleted, {
        type: 'replace',
        oldApiKey: 'b',
        newApiKey: 'b',
        channel: '',
      })
    ).toThrow('必须选择');
    expect(parse(deleted)['api-keys']).toEqual(parse(input)['api-keys']);
    expect(parse(deleted).unknown).toEqual({ nested: null });
    expect(parse(deleted).access.custom).toEqual({ keep: true });
  });
  it.each(['kilo', 'codex', 'bps'] as const)('新增%s保留旧Key未绑定', (channel) => {
    const saved = readClientKeyYaml(
      mutateClientKeyYaml(input, { type: 'create', apiKey: 'added', channel })
    );
    expect(saved.keys).toContain('old');
    expect(saved.scopes.some((scope) => scope['api-key'] === 'old')).toBe(false);
    expect(saved.scopes).toContainEqual({ 'api-key': 'added', channel });
  });
  it.each([
    ['重复Key', 'access: {api-keys: [k, k]}'],
    ['非法channel', 'access: {api-keys: [k], client-key-scopes: [{api-key: k, channel: other}]}'],
    ['悬空引用', 'access: {api-keys: [k], client-key-scopes: [{api-key: missing, channel: kilo}]}'],
    [
      '重复绑定',
      'access: {api-keys: [k], client-key-scopes: [{api-key: k, channel: kilo}, {api-key: k, channel: codex}]}',
    ],
  ])('%s拒绝而非静默修剪', (_, yaml) => {
    expect(() => mutateClientKeyYaml(yaml, { type: 'create', apiKey: 'new' })).toThrow();
  });
});

it('新增与legacy轮换必须显式通道，原值保存兼容', () => {
  expect(() => mutateClientKeyYaml(input, { type: 'create', apiKey: 'new' })).toThrow('必须选择');
  expect(() =>
    mutateClientKeyYaml(input, { type: 'replace', oldApiKey: 'old', newApiKey: 'new' })
  ).toThrow('必须选择');
  expect(
    readClientKeyYaml(
      mutateClientKeyYaml(input, { type: 'replace', oldApiKey: 'old', newApiKey: 'old' })
    ).keys
  ).toContain('old');
});
