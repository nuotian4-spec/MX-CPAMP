$ErrorActionPreference='Stop'
$prefix='mx-preflight-smoke-'+[guid]::NewGuid().ToString('N').Substring(0,10)
$containerName=$prefix+'-container'
$volumeName=$prefix+'-data'
$sentinel='MX_PREFLIGHT_FAKE_ENV_DO_NOT_PRINT'
$scriptPath=Join-Path $PSScriptRoot '../../../deploy/readonly-preflight.sh'
$scriptText=[System.IO.File]::ReadAllText((Resolve-Path $scriptPath))
$bindSource=(Resolve-Path $PSScriptRoot).Path
try {
  $null=docker volume create $volumeName
  if($LASTEXITCODE -ne 0){throw 'fixture_volume_create_failed'}
  $null=docker create --name $containerName --network none --env "PREFLIGHT_TEST_SECRET=$sentinel" --mount "type=volume,source=$volumeName,target=/fixture-volume" --mount "type=bind,source=$bindSource,target=/fixture-bind,readonly" eceasy/cli-proxy-api:v8.0.63
  if($LASTEXITCODE -ne 0){throw 'fixture_container_create_failed'}
  $labels=docker inspect --format '{{with index .Config "Labels"}}declared{{else}}absent{{end}}' $containerName
  if($LASTEXITCODE -ne 0 -or $labels -ne 'absent'){throw 'fixture_must_have_no_labels'}
  foreach($match in [regex]::Matches($scriptText,"--format '([^']+)'",[System.Text.RegularExpressions.RegexOptions]::Singleline)) {
    $format=$match.Groups[1].Value
    if($format.StartsWith('image_identity=')) {
      $formatted=docker image inspect --format $format eceasy/cli-proxy-api:v8.0.63
    } else {
      $formatted=docker inspect --format $format $containerName
    }
    if($LASTEXITCODE -ne 0){throw 'actual_docker_format_failed'}
    if(($formatted -join "`n").Contains($sentinel)){throw 'env_sentinel_leaked'}
    Write-Output $formatted
  }
  $labelMatch=[regex]::Match($scriptText,'--format "([^\r\n]+)" "\$1"')
  if(-not $labelMatch.Success){throw 'label_format_missing'}
  $labelFormat=$labelMatch.Groups[1].Value.Replace('\"','"').Replace('$2','com.docker.compose.project')
  $labelValue=docker inspect --format $labelFormat $containerName
  if($LASTEXITCODE -ne 0 -or $labelValue){throw 'optional_label_format_failed'}
  Write-Output 'actual_docker_formats_exit=0 labels=absent env_sentinel=absent'
} finally {
  $null=docker container rm $containerName 2>$null
  $null=docker volume rm $volumeName 2>$null
  $null=docker container inspect --format '{{.Id}}' $containerName 2>$null
  $containerGone=$LASTEXITCODE -ne 0
  $null=docker volume inspect --format '{{.Name}}' $volumeName 2>$null
  $volumeGone=$LASTEXITCODE -ne 0
  if(-not $containerGone -or -not $volumeGone){throw 'fixture_cleanup_failed'}
  Write-Output 'actual_docker_fixture_cleanup=verified'
}
