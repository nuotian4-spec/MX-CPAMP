param([Parameter(Mandatory=$true)][string]$ManagerBinary)
$ErrorActionPreference = 'Stop'
$evidence = $PSScriptRoot
$cpa = 'C:/Users/子羽/.codex/artifacts/cpa-core-v817-20261127/CLIProxyAPI-final'
$manager = (Resolve-Path -LiteralPath $ManagerBinary).Path
if (!(Test-Path -LiteralPath $cpa -PathType Leaf)) { throw '已验 CPA 产物不存在' }
if (!(Test-Path -LiteralPath $manager -PathType Leaf)) { throw '最终 Manager 产物不存在' }
$dockerArgs = @('run','--rm','--pull','never','--network','none',
  '--mount',"type=bind,source=$cpa,target=/inputs/CLIProxyAPI,readonly",
  '--mount',"type=bind,source=$manager,target=/inputs/manager,readonly",
  '--mount',"type=bind,source=$evidence,target=/evidence",
  'python:3.12-bookworm','python3','/evidence/run-real-chain.py')
[ordered]@{tool='docker';arguments=$dockerArgs;manager_binary=$manager;cpa_binary=$cpa} | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $evidence 'real-chain-command.json') -Encoding utf8
& docker @dockerArgs *> (Join-Path $evidence 'real-chain-run.log')
$code = $LASTEXITCODE
$code | Set-Content -LiteralPath (Join-Path $evidence 'real-chain-exit-code.txt')
Get-Content -LiteralPath (Join-Path $evidence 'real-chain-run.log')
exit $code
