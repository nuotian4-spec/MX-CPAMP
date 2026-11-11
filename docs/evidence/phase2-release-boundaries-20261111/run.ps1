param([switch]$SetupOnly,[switch]$MaintenanceFailure,[switch]$SourceChecks,[switch]$ReusePassed)
$ErrorActionPreference='Stop'
$prefix='mx-phase2-boundary-'+[guid]::NewGuid().ToString('N').Substring(0,10)
$rootVolume=$prefix+'-root'
$toolRoot='D:/MX-CPAMP-worktrees/phase2-release-tooling/deploy'
$bundleRoot='C:/Users/子羽/.codex/artifacts/mx-cpamp-release-phase2-20261111-candidate'
$runnerImage='sha256:d03ace6e4638d1be235650b443a81e1cd518538e7ed059141c157ca5615c7b48'
$log=Join-Path $PSScriptRoot $(if($MaintenanceFailure){'maintenance-failure.log'}else{'run.log'})
$exitCode=1
try {
  $null=docker volume create --label "mx.phase2boundary=$prefix" $rootVolume
  if($LASTEXITCODE -ne 0){throw '创建本轮隔离卷失败'}
  $root=(docker volume inspect $rootVolume --format '{{.Mountpoint}}').Trim()
  if(-not $root.StartsWith('/var/lib/docker/volumes/'+$rootVolume+'/')){throw '隔离卷路径不符'}
  docker run --rm --name ($prefix+'-runner') --network none --label "mx.phase2boundary=$prefix" --mount 'type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock' --mount "type=volume,source=$rootVolume,target=$root" --mount "type=bind,source=$toolRoot,target=/delivery,readonly" --mount "type=bind,source=$bundleRoot,target=/frozen-bundle,readonly" --mount "type=bind,source=$PSScriptRoot,target=/checks,readonly" --env "FIXTURE_ROOT=$root" --env "FIXTURE_PROJECT=$prefix" --env "FIXTURE_SETUP_ONLY=$([int]$SetupOnly.IsPresent)" --env "FIXTURE_SOURCE_CHECKS=$([int]$SourceChecks.IsPresent)" --env "FIXTURE_MAINTENANCE_FAILURE=$([int]$MaintenanceFailure.IsPresent)" --env "FIXTURE_REUSE_PASSED=$([int]$ReusePassed.IsPresent)" --entrypoint python3 $runnerImage /checks/run.py *> $log
  $exitCode=$LASTEXITCODE
  Get-Content -LiteralPath $log
} finally {
  $containers=@(docker ps -aq --filter "label=com.docker.compose.project=$prefix")
  if($containers.Count){$null=docker rm --force $containers}
  $runner=@(docker ps -aq --filter "label=mx.phase2boundary=$prefix")
  if($runner.Count){$null=docker rm --force $runner}
  $networks=@(docker network ls -q --filter "label=com.docker.compose.project=$prefix")
  foreach($network in $networks){$null=docker network rm $network}
  $volumes=@(docker volume ls -q --filter "label=com.docker.compose.project=$prefix")
  foreach($volume in $volumes){$null=docker volume rm $volume}
  $owner=(docker volume inspect $rootVolume --format '{{index .Labels "mx.phase2boundary"}}' 2>$null)
  if($owner -eq $prefix){$null=docker volume rm $rootVolume}
  $remaining=@(docker ps -aq --filter "label=com.docker.compose.project=$prefix")+@(docker network ls -q --filter "label=com.docker.compose.project=$prefix")+@(docker volume ls -q --filter "label=com.docker.compose.project=$prefix")+@(docker volume ls -q --filter "label=mx.phase2boundary=$prefix")
  if($remaining.Count){$exitCode=1;Add-Content -LiteralPath $log 'fixture_cleanup=incomplete'}else{Add-Content -LiteralPath $log 'fixture_cleanup=verified'}
  Add-Content -LiteralPath $log "command_exit=$exitCode"
}
exit $exitCode
