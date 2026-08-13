$ErrorActionPreference = 'Stop'

$webBase = if ($env:PERMIT_WEB_BASE_URL) { $env:PERMIT_WEB_BASE_URL.TrimEnd('/') } else { 'http://localhost:3000' }
$gatewayBase = if ($env:PERMIT_GATEWAY_BASE_URL) { $env:PERMIT_GATEWAY_BASE_URL.TrimEnd('/') } else { 'http://localhost:8081' }

function Assert-True([bool] $Condition, [string] $Message) {
    if (-not $Condition) { throw $Message }
}

function Wait-Http([string] $Uri, [int] $Attempts = 30) {
    for ($attempt = 1; $attempt -le $Attempts; $attempt++) {
        try {
            $response = Invoke-WebRequest -Uri $Uri -UseBasicParsing
            if ($response.StatusCode -eq 200) { return $response }
        }
        catch {
            if ($attempt -eq $Attempts) { throw }
        }
        Start-Sleep -Milliseconds 500
    }
    throw "Timed out waiting for $Uri"
}

$homeResponse = Wait-Http $webBase
Assert-True ($homeResponse.StatusCode -eq 200) 'Web home did not return HTTP 200.'
Assert-True ($homeResponse.Content -match 'Website address') 'The single URL input was not rendered.'
Assert-True ($homeResponse.Content -notmatch 'Sign in|>Register<|type="checkbox"') 'Account or authorization controls reappeared.'

$allowedBody = @{ input_url = 'http://demo-target:9000/' } | ConvertTo-Json
$allowed = Invoke-RestMethod -Method Post -Uri "$webBase/api/access/check" -ContentType 'application/json' -Body $allowedBody
Assert-True ($allowed.decision -eq 'allowed') 'The registered public demo resource was not allowed.'
Assert-True ($allowed.launch_url -match '^http://localhost:8081/_launch/') 'The gateway returned an unexpected launch URL.'

$session = New-Object Microsoft.PowerShell.Commands.WebRequestSession
$resource = Invoke-WebRequest -Uri $allowed.launch_url -WebSession $session -UseBasicParsing
Assert-True ($resource.StatusCode -eq 200) 'The one-time launch did not reach the authorized resource.'
Assert-True ($resource.BaseResponse.RequestMessage.RequestUri.AbsolutePath -eq '/') 'The one-time launch did not redirect to the approved resource path.'
Assert-True ($resource.Content -match 'The controlled route is working') 'The authorized fixture response was not received.'

$deniedTargets = @(
    'https://example.com/',
    'http://127.0.0.1:9000/',
    'http://169.254.169.254/latest/meta-data/',
    'http://demo-target:9001/'
)
foreach ($target in $deniedTargets) {
    $body = @{ input_url = $target } | ConvertTo-Json
    try {
        $response = Invoke-RestMethod -Method Post -Uri "$webBase/api/access/check" -ContentType 'application/json' -Body $body
        throw "Unsafe or unregistered target was unexpectedly allowed: $target ($($response.decision))"
    }
    catch {
        if ($_.Exception.Message -like 'Unsafe or unregistered target*') { throw }
        $status = [int]$_.Exception.Response.StatusCode
        Assert-True ($status -in @(403, 422)) "Unexpected denial status for $target`: $status"
    }
}

Write-Host 'Permit demo E2E passed: single-input UI, registered-resource access, one-time launch, proxying, and fail-closed denials.'
