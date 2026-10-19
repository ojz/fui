$ErrorActionPreference = 'Stop'

Set-Location $PSScriptRoot

Write-Host '==> git pull'
git pull
if ($LASTEXITCODE -ne 0) { throw 'git pull failed' }

Write-Host '==> stopping server'
Stop-Process -Name fui -Force -ErrorAction SilentlyContinue

Write-Host '==> go install'
go install
if ($LASTEXITCODE -ne 0) { throw 'go install failed' }
Remove-Item "$env:USERPROFILE\go\bin\fui.exe~" -Force -ErrorAction SilentlyContinue

Write-Host '==> starting server'
Start-Process -WindowStyle Hidden -FilePath "$env:USERPROFILE\go\bin\fui.exe" `
  -ArgumentList 'serve','--hidden','--port','6219'

Start-Sleep -Seconds 1
$proc = Get-Process fui -ErrorAction SilentlyContinue
if ($proc) {
  Write-Host "==> running (PID $($proc.Id), started $($proc.StartTime))"
} else {
  throw 'server did not start'
}
