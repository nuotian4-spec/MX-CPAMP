$ErrorActionPreference = 'Stop'
$browser = 'C:/Users/子羽/.codex/tmp/unified-update-20261116/preview/browser'
$manager = 'unified-update-manager-browser-20261116'
$fixture = 'unified-update-preview-20261116'
$deadline = (Get-Date).AddMinutes(2)
while (-not (Test-Path -LiteralPath "$browser/core-confirmed.json")) {
    if ((Get-Date) -gt $deadline) { throw '等待合成浏览器更新确认超时' }
    Start-Sleep -Milliseconds 100
}
$before = docker inspect --format '{{.State.Pid}}' $manager
if ($LASTEXITCODE -ne 0) { throw '读取独立Manager进程失败' }
$workerBefore = docker exec $fixture python3 -c 'import json,os,pathlib; r=pathlib.Path(json.load(open("/preview/host-ready.json"))["root"]); p=json.loads((r/"worker-process.json").read_text())["pid"]; os.kill(p,0); print(p)'
if ($LASTEXITCODE -ne 0) { throw '维护前独立worker存活检查失败' }
docker restart --time 3 $manager | Out-Null
if ($LASTEXITCODE -ne 0) { throw '合成Manager重启失败' }
$after = docker inspect --format '{{.State.Pid}}' $manager
if ($before -eq $after -or [int]$after -eq 0) { throw 'Manager未形成新的实际进程' }
$workerAfter = docker exec $fixture python3 -c 'import json,os,pathlib; r=pathlib.Path(json.load(open("/preview/host-ready.json"))["root"]); p=json.loads((r/"worker-process.json").read_text())["pid"]; os.kill(p,0); (r/"continue").touch(); print(p)'
if ($LASTEXITCODE -ne 0 -or $workerBefore -ne $workerAfter) { throw 'Manager重启后worker未保持原进程' }
@{ manager = $manager; old_pid = [int]$before; new_pid = [int]$after; worker_pid_before = [int]$workerBefore; worker_pid_after = [int]$workerAfter; boundary = '仅合成Manager真实重启；站点Docker与发行传输为夹具'; rc = 0 } | ConvertTo-Json | Set-Content -LiteralPath "$browser/manager-restarted.json" -Encoding utf8NoBOM
