$ErrorActionPreference='Stop'
$prefix='mx-preflight-'+[guid]::NewGuid().ToString('N').Substring(0,10)
$volume=$prefix+'-data'
$repo=(Resolve-Path (Join-Path $PSScriptRoot '../../..')).Path
$log=Join-Path $PSScriptRoot 'fixture.log'
$code=1
try {
  $null=docker volume create $volume
  $mount=docker volume inspect --format '{{.Mountpoint}}' $volume
  docker run --rm --network bridge --mount 'type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock' --mount "type=volume,source=$volume,target=$mount" --mount "type=bind,source=$repo/deploy,target=/delivery,readonly" --mount "type=bind,source=$PSScriptRoot,target=/checks,readonly" -e "FIXTURE_ROOT=$mount" -e "FIXTURE_PROJECT=$prefix" --entrypoint python3 cpa-backup-test:20261110 /checks/verify.py *> $log
  $code=$LASTEXITCODE
  Get-Content $log
} finally {
  $ids=@(docker ps -aq --filter "label=com.docker.compose.project=$prefix")
  if($ids.Count -gt 0){$null=docker rm -f $ids 2>$null}
  $networks=@(docker network ls -q --filter "label=com.docker.compose.project=$prefix")
  if($networks.Count -gt 0){$null=docker network rm $networks 2>$null}
  $null=docker volume rm $volume 2>$null
  if((docker ps -aq --filter "label=com.docker.compose.project=$prefix") -or (docker volume ls -q --filter "name=$volume")){$code=1}else{Add-Content $log 'fixture_cleanup=verified'}
  Add-Content $log "exit=$code"
}
exit $code
