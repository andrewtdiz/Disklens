param([string]$Folder)
$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot
& (Join-Path $PSScriptRoot 'build-resources.ps1')
$goCommand = Get-Command go -ErrorAction SilentlyContinue
if ($goCommand) {
    $goBinary = $goCommand.Source
} else {
    $goBinary = Join-Path $env:USERPROFILE 'go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.3.windows-amd64/bin/go.exe'
    if (-not (Test-Path -LiteralPath $goBinary)) { throw 'Install Go 1.27.3 or later and put go on PATH.' }
}
& $goBinary build -ldflags '-H=windowsgui' -o build/windows-amd64/Disklens.exe .
if ($LASTEXITCODE -ne 0) { throw 'Disklens build failed.' }
& $goBinary build -tags cli -o build/cli/disklens.exe .
if ($LASTEXITCODE -ne 0) { throw 'Disklens CLI build failed.' }
$binary = Join-Path $PSScriptRoot 'build/windows-amd64/Disklens.exe'
if ($Folder) { Start-Process -FilePath $binary -ArgumentList ('"' + (Join-Path $Folder '.') + '"') } else { Start-Process -FilePath $binary }
