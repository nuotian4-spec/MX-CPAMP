import { useEffect, useRef, useState } from 'react';
import { Button } from '@/components/ui/Button';
import { Drawer } from '@/components/ui/Drawer';
import { Input } from '@/components/ui/Input';
import {
  createBpsLicenseApi,
  classifyBpsLicenseError,
  type BpsLicenseFailure,
  type BpsLicenseStatus,
} from '@/services/api/bpsLicense';
import type { ApiClientRequestScope } from '@/services/api/client';
import styles from './BpsLicensePanel.module.scss';

interface Props {
  scope: ApiClientRequestScope;
  connected: boolean;
  registered: boolean;
}

const failureText: Record<BpsLicenseFailure, string> = {
  missing: '许可接口未提供（404），许可未核实',
  unauthorized: '管理鉴权未通过，许可未核实',
  network: '管理连接请求失败，许可未核实',
  timeout: '读取许可状态超时，许可未核实',
  invalid: '许可响应缺失或格式未知，许可未核实',
  server: '许可接口返回错误，许可未核实',
};

export function bpsLicenseSummary(status: BpsLicenseStatus): string {
  if (!status.required) return '此版本无需商业许可';
  if (status.active) return '商业许可有效';
  if (status.code === 'license_expired') return '商业许可已过期';
  if (status.code === 'license_invalid') return '商业许可无效';
  if (status.code === 'license_required' || status.code === 'not_activated')
    return '商业许可待激活';
  return '商业许可未生效，服务状态代码未识别';
}

export function BpsLicensePanel(props: Props) {
  return (
    <BpsLicenseSession
      key={JSON.stringify([
        props.scope.apiBase,
        props.scope.managementKey,
        props.connected,
        props.registered,
      ])}
      {...props}
    />
  );
}

function BpsLicenseSession({ scope, connected, registered }: Props) {
  const [api] = useState(() => createBpsLicenseApi(scope));
  const [status, setStatus] = useState<BpsLicenseStatus | null>(null);
  const [failure, setFailure] = useState<BpsLicenseFailure | null>(null);
  const [open, setOpen] = useState(false);
  const [licenseKey, setLicenseKey] = useState('');
  const [reading, setReading] = useState(false);
  const [writing, setWriting] = useState(false);
  const [notice, setNotice] = useState('');
  const mounted = useRef(false);
  const epoch = useRef(0);
  const pendingWrite = useRef(false);
  const available = connected && registered && Boolean(scope.apiBase && scope.managementKey);
  const current = (token: number) => mounted.current && epoch.current === token;

  const read = async (token: number) => {
    if (!available || !current(token)) return;
    setReading(true);
    try {
      const next = await api.get();
      if (!current(token)) return;
      setStatus(next);
      setFailure(null);
    } catch (error: unknown) {
      if (!current(token)) return;
      setStatus(null);
      setFailure(classifyBpsLicenseError(error));
    } finally {
      if (current(token)) setReading(false);
    }
  };

  useEffect(() => {
    mounted.current = true;
    void read(epoch.current);
    return () => {
      mounted.current = false;
      epoch.current += 1;
    };
    // 本会话由连接、凭证、注册状态共同定界，改变时整体卸载。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const close = () => {
    epoch.current += 1;
    setLicenseKey('');
    setReading(false);
    setNotice('');
    setOpen(false);
  };

  const write = async (action: 'activate' | 'refresh') => {
    if (!available || !status || reading || pendingWrite.current) return;
    const key = licenseKey.trim();
    if (action === 'activate' && !key) return;
    pendingWrite.current = true;
    setWriting(true);
    setLicenseKey('');
    setNotice('');
    const token = ++epoch.current;
    try {
      if (action === 'activate') await api.activate(key);
      else await api.refresh();
      if (current(token)) setNotice('操作已返回，已只读核实许可状态。');
    } catch {
      if (current(token)) setNotice('操作结果未确认；仅查询许可状态，不会自动重发。');
    } finally {
      if (current(token)) await read(token);
      pendingWrite.current = false;
      if (mounted.current) setWriting(false);
    }
  };

  const summary = !registered
    ? '插件未注册，许可未核实'
    : !connected
      ? '管理连接未就绪，许可未核实'
      : failure
        ? failureText[failure]
        : status
          ? bpsLicenseSummary(status)
          : '许可尚未核实';
  const disabled = !available || !status || reading || writing;

  return (
    <>
      <div className={styles.summary} data-bps-license>
        <span>{summary}</span>
        <Button
          variant="secondary"
          size="sm"
          onClick={() => {
            setOpen(true);
            void read(++epoch.current);
          }}
        >
          管理许可
        </Button>
      </div>
      <Drawer
        open={open}
        title="BPS 商业许可"
        width={460}
        onBeforeClose={() => {
          close();
          return true;
        }}
        onClose={close}
      >
        <div className={styles.detail}>
          <p role="status">{summary}</p>
          <p className={styles.hint}>
            商业许可独立于插件注册与账号 OAuth；有效许可不代表账号或模型业务已经验证。
          </p>
          {status && (
            <dl className={styles.facts}>
              <dt>授权码提示</dt>
              <dd>{status.keyHint || '未提供脱敏提示'}</dd>
              <dt>绑定 IP</dt>
              <dd>{status.boundIp || '未提供'}</dd>
              <dt>许可有效期</dt>
              <dd>{status.licenseExpiresAt || '未提供'}</dd>
              <dt>授权有效期</dt>
              <dd>{status.expiresAt || '未提供'}</dd>
              <dt>最近错误</dt>
              <dd>{status.hasLastError ? '插件报告许可错误，请只读核实状态。' : '无报告错误'}</dd>
            </dl>
          )}
          {(!status || status.required) && (
            <Input
              label="当次授权码"
              type="password"
              value={licenseKey}
              onChange={(event) => setLicenseKey(event.target.value)}
              autoComplete="new-password"
              spellCheck={false}
              disabled={disabled}
              hint="仅用于本次激活，提交或关闭后清空。"
            />
          )}
          <div className={styles.actions}>
            <Button
              variant="secondary"
              disabled={!available || reading || writing}
              loading={reading}
              onClick={() => void read(++epoch.current)}
            >
              读取状态
            </Button>
            {(!status || status.required) && (
              <Button
                disabled={disabled || !licenseKey.trim()}
                onClick={() => void write('activate')}
              >
                激活许可
              </Button>
            )}
            <Button variant="secondary" disabled={disabled} onClick={() => void write('refresh')}>
              刷新许可
            </Button>
          </div>
          <p className={styles.hint}>刷新许可可能连接供应商；打开此页和读取状态只发送 GET。</p>
          {notice && <p role="status">{notice}</p>}
        </div>
      </Drawer>
    </>
  );
}
