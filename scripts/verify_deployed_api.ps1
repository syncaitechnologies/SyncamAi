param(
    [Parameter(Mandatory)][string]$BaseUrl,
    [Parameter(Mandatory)][string]$BrowserOrigin
)
$ErrorActionPreference = 'Stop'

foreach ($origin in @($BaseUrl, $BrowserOrigin)) {
    $parsed = [Uri]$origin
    if (-not $parsed.IsAbsoluteUri -or $parsed.Scheme -ne 'https' -or
        $parsed.UserInfo -or $parsed.Query -or $parsed.Fragment -or
        $parsed.AbsolutePath -ne '/' -or $origin.EndsWith('/')) {
        throw 'Use bare HTTPS origins without credentials, paths, query strings or trailing slashes.'
    }
}

# Public, non-mutating checks only. No real tokens, users or business data.
$cases = @(
    @{name='health';method='GET';path='/healthz';headers=@{};expected=200;cors=$false},
    @{name='unauthenticated sites';method='GET';path='/v1/sites';headers=@{};expected=401;cors=$false},
    @{name='approved browser still requires authentication';method='GET';path='/v1/sites';headers=@{Origin=$BrowserOrigin};expected=401;cors=$true},
    @{name='foreign origin denied';method='GET';path='/v1/sites';headers=@{Origin='https://unapproved.example'};expected=403;cors=$false},
    @{name='approved preflight';method='OPTIONS';path='/v1/sites';headers=@{Origin=$BrowserOrigin;'Access-Control-Request-Method'='GET';'Access-Control-Request-Headers'='authorization,x-sentinelvision-tenant-id'};expected=204;cors=$true},
    @{name='unapproved preflight header denied';method='OPTIONS';path='/v1/sites';headers=@{Origin=$BrowserOrigin;'Access-Control-Request-Method'='GET';'Access-Control-Request-Headers'='x-unapproved-header'};expected=403;cors=$false},
    @{name='foreign websocket origin denied';method='GET';path='/ws/v1/alerts';headers=@{Origin='https://unapproved.example'};expected=403;cors=$false}
)
foreach ($case in $cases) {
    $response = Invoke-WebRequest -Uri ($BaseUrl + $case.path) -Method $case.method -Headers $case.headers -TimeoutSec 40 -SkipHttpErrorCheck -MaximumRedirection 0
    $allowed = $response.Headers['Access-Control-Allow-Origin'] -contains $BrowserOrigin
    if ($response.StatusCode -ne $case.expected -or $allowed -ne $case.cors -or
        $response.Headers.ContainsKey('Access-Control-Allow-Credentials')) {
        throw ('HTTP verification failed: ' + $case.name)
    }
    if ($case.name -eq 'health' -and ($response.Content | ConvertFrom-Json).data.status -ne 'ok') {
        throw 'Health endpoint returned an unexpected envelope.'
    }
    Write-Output ('PASS: ' + $case.name + ' [' + $response.StatusCode + ']')
}
