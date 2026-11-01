$ErrorActionPreference = 'Stop'
$goCommand = Get-Command go -ErrorAction SilentlyContinue
$goBinary = if ($goCommand) { $goCommand.Source } else { Join-Path $env:USERPROFILE 'go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.3.windows-amd64/bin/go.exe' }
$nsis = (Get-Command makensis -ErrorAction Stop).Source
$version = (Get-Content -LiteralPath (Join-Path $PSScriptRoot 'mygo.json') -Raw | ConvertFrom-Json).version
$payload = Join-Path $PSScriptRoot 'build/installer-payload'
$output = Join-Path $PSScriptRoot "build/Disklens-$version-Setup.exe"
$null = New-Item -ItemType Directory -Force -Path (Join-Path $payload 'cli')
Push-Location $PSScriptRoot
try {
    & (Join-Path $PSScriptRoot 'build-resources.ps1')
    & $goBinary build -trimpath -tags mygo_noinspector -ldflags '-H=windowsgui' -o (Join-Path $payload 'Disklens.exe') .
    if ($LASTEXITCODE -ne 0) { throw 'Desktop build failed.' }
    & $goBinary build -trimpath -tags cli -o (Join-Path $payload 'cli/disklens.exe') .
    if ($LASTEXITCODE -ne 0) { throw 'CLI build failed.' }
    & $nsis /V2 "/DVERSION=$version" "/DPAYLOAD=$payload" "/DOUTPUT=$output" (Join-Path $PSScriptRoot 'installer/windows.nsi')
    if ($LASTEXITCODE -ne 0) { throw 'Installer build failed.' }
} finally { Pop-Location }
Write-Output "Installer: $output"
