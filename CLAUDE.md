# fui

* local HTTP server: renders agent HTML in browser; syncs page state
* driven with curl; the agent contract is `assets/SKILL.md`, served at `/SKILL.md`
* no MCP
* Windows only
* LF only

## Restart after Go changes

Always restart; the old process keeps serving.

```powershell
Stop-Process -Name fui -Force -ErrorAction SilentlyContinue
go install
Remove-Item "$env:USERPROFILE\go\bin\fui.exe~" -Force -ErrorAction SilentlyContinue
Start-Process -WindowStyle Hidden -FilePath "$env:USERPROFILE\go\bin\fui.exe" `
  -ArgumentList 'serve','--hidden','--port','6219'
```

Verify:

```powershell
Get-Process fui | Select-Object Id, Path, StartTime
Get-NetTCPConnection -LocalPort 6219 -State Listen | Select-Object LocalPort, OwningProcess
```

`StartTime` should be recent.

SQLite state survives at `%APPDATA%\fui\fui.db`.

## Autostart

`fui autostart install` writes:

```text
HKCU\Software\Microsoft\Windows\CurrentVersion\Run
  fui = "<absolute-path>\fui.exe" serve --hidden --port 6219
```

* rerun after moving the exe
* use `go install`, not `go build`, for deployment
* inspect with:

```powershell
Get-ItemProperty 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run' -Name fui
```

## Pointing a project at fui

One line in that project's CLAUDE.md:

```markdown
For multi-question input or visual output, use fui: `curl -s http://127.0.0.1:6219/SKILL.md`.
```
