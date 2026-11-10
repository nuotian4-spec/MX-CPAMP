$ErrorActionPreference='Stop'
$prefix='mx-release-boundaries-'+[guid]::NewGuid().ToString('N').Substring(0,10)
$secretVolume=$prefix+'-secret'
$pluginVolume=$prefix+'-plugin'
$rootPath=(Resolve-Path (Join-Path $PSScriptRoot '../../..')).Path
$delivery=Join-Path $rootPath 'deploy'
$log=Join-Path $PSScriptRoot 'release-boundaries.log'
$releaseManifest='C:/Users/子羽/.codex/artifacts/mx-cpamp-release-20261110-rc1/mx-cpamp-images.manifest.json'
$exitCode=1
try {
  $null=docker volume create $secretVolume
  if($LASTEXITCODE -ne 0){throw 'fixture_secret_volume_failed'}
  $null=docker volume create $pluginVolume
  if($LASTEXITCODE -ne 0){throw 'fixture_plugin_volume_failed'}
  docker run --rm --network none --mount "type=volume,source=$pluginVolume,target=/out" --entrypoint sh docker:29.7.2-dind -c 'cp /usr/local/libexec/docker/cli-plugins/docker-compose /out/docker-compose'
  if($LASTEXITCODE -ne 0){throw 'cached_compose_plugin_failed'}
  docker run --rm --name $prefix --network bridge --mount 'type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock' --mount "type=volume,source=$secretVolume,target=/opt/cpa-stack/secrets" --mount "type=volume,source=$pluginVolume,target=/root/.docker/cli-plugins,readonly" --mount "type=bind,source=$delivery,target=/delivery,readonly" --mount "type=bind,source=$PSScriptRoot,target=/checks,readonly" --mount "type=bind,source=$releaseManifest,target=/release/mx-cpamp-images.manifest.json,readonly" -e "FIXTURE_SECRET_VOLUME=$secretVolume" -e "FIXTURE_PREFIX=$prefix" --entrypoint python3 cpa-backup-test:20261110 /checks/release-boundaries.py *> $log
  $exitCode=$LASTEXITCODE
  Get-Content $log
} finally {
  foreach($container in @($prefix,$prefix+'-manager',$prefix+'-manager-cpa')){$null=docker container rm -f $container 2>$null}
  $null=docker volume rm $secretVolume 2>$null
  $null=docker volume rm $pluginVolume 2>$null
  $remainingContainers=docker container ls -aq --filter "name=$prefix"
  $remainingVolumes=docker volume ls -q --filter "name=$prefix"
  if($remainingContainers -or $remainingVolumes){$exitCode=1; Add-Content $log 'fixture_cleanup=incomplete'}else{Add-Content $log 'fixture_cleanup=verified'}
  Add-Content $log "release_boundaries_command_exit=$exitCode"
}
exit $exitCode
