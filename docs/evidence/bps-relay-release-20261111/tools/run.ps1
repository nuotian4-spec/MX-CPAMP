param([Parameter(Mandatory=$true)][string]$BundleRoot,[switch]$CaddyReloadFailure)
$ErrorActionPreference='Stop'
$prefix='mx-bps-relay-boundary-'+[guid]::NewGuid().ToString('N').Substring(0,10)
$rootVolume=$prefix+'-root'
$repoRoot=(Resolve-Path (Join-Path $PSScriptRoot '../../../..')).Path
$toolRoot=Join-Path $repoRoot 'deploy'
$bundleRoot=(Resolve-Path -LiteralPath $BundleRoot).Path
$runnerImage='sha256:d03ace6e4638d1be235650b443a81e1cd518538e7ed059141c157ca5615c7b48'
$log=Join-Path $PSScriptRoot $(if($CaddyReloadFailure){'caddy-reload-failure.log'}else{'run.log'})
$exitCode=1
try {
  $null=docker volume create --label "mx.bpsrelayboundary=$prefix" $rootVolume
  if($LASTEXITCODE -ne 0){throw '创建本轮隔离卷失败'}
  $root=(docker volume inspect $rootVolume --format '{{.Mountpoint}}').Trim()
  if(-not $root.StartsWith('/var/lib/docker/volumes/'+$rootVolume+'/')){throw '隔离卷路径不符'}
  docker run --rm --name ($prefix+'-runner') --network none --label "mx.bpsrelayboundary=$prefix" --mount 'type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock' --mount "type=volume,source=$rootVolume,target=$root" --mount "type=bind,source=$toolRoot,target=/delivery,readonly" --mount "type=bind,source=$bundleRoot,target=/frozen-bundle,readonly" --mount "type=bind,source=$PSScriptRoot,target=/checks,readonly" --env "FIXTURE_ROOT=$root" --env "FIXTURE_PROJECT=$prefix" --env "FIXTURE_CADDY_FAILURE=$([int]$CaddyReloadFailure.IsPresent)" --entrypoint python3 $runnerImage /checks/run.py *> $log
  $exitCode=$LASTEXITCODE
  Get-Content -LiteralPath $log
} finally {
  $containers=@(docker ps -aq --filter "label=com.docker.compose.project=$prefix")
  if($containers.Count){$null=docker rm --force $containers}
  $runner=@(docker ps -aq --filter "label=mx.bpsrelayboundary=$prefix")
  if($runner.Count){$null=docker rm --force $runner}
  $networks=@(docker network ls -q --filter "label=com.docker.compose.project=$prefix")
  foreach($network in $networks){$null=docker network rm $network}
  $volumes=@(docker volume ls -q --filter "label=com.docker.compose.project=$prefix")
  foreach($volume in $volumes){$null=docker volume rm $volume}
  $owner=(docker volume inspect $rootVolume --format '{{index .Labels "mx.bpsrelayboundary"}}' 2>$null)
  if($owner -eq $prefix){$null=docker volume rm $rootVolume}
  $remaining=@(docker ps -aq --filter "label=com.docker.compose.project=$prefix")+@(docker network ls -q --filter "label=com.docker.compose.project=$prefix")+@(docker volume ls -q --filter "label=com.docker.compose.project=$prefix")+@(docker volume ls -q --filter "label=mx.bpsrelayboundary=$prefix")
  if($remaining.Count){$exitCode=1;Add-Content -LiteralPath $log 'fixture_cleanup=incomplete'}else{Add-Content -LiteralPath $log 'fixture_cleanup=verified'}
  Add-Content -LiteralPath $log "command_exit=$exitCode"
}
exit $exitCode
