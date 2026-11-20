import type { ModelDiscoveryState } from '@/services/api/authFiles';

export function ModelDiscoveryNotice({
  discovery,
  modelCount,
  disabled = false,
}: {
  discovery?: ModelDiscoveryState;
  modelCount: number;
  disabled?: boolean;
}) {
  const status = disabled ? 'disabled' : (discovery?.status ?? 'unknown');
  const message =
    status === 'disabled'
      ? '凭证已停用，模型发现结果不作为当前可用状态。'
      : status === 'failed'
        ? '插件模型发现失败；上游状态未提供。'
        : status === 'succeeded'
          ? modelCount > 0
            ? '最近模型发现成功，模型已登记。'
            : '最近模型发现成功，当前注册目录为空。'
          : '尚无本次运行的模型发现记录；现有目录不代表最近发现成功。';
  return (
    <div role={status === 'failed' ? 'alert' : 'status'} data-model-discovery={status}>
      <p>{message}</p>
      {status === 'failed' && discovery?.error_code && <p>错误代码：{discovery.error_code}</p>}
      {status !== 'disabled' && discovery?.checked_at && (
        <p>最近发现时间：{discovery.checked_at}</p>
      )}
    </div>
  );
}
