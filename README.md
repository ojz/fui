# fui

A tiny local server that lets a coding agent render **interactive HTML pages in your browser** and read your input back. The agent drives it with plain `curl`; there is no MCP server to register.

Instead of answering twenty questions one by one in the terminal, your agent publishes a form with checkboxes, you fill it in, click **Done**, and the agent gets everything back in one JSON response. The same mechanism runs the other way: the agent posts progress keys and a dashboard page updates live. All page state is persisted in SQLite, so it survives browser refreshes, server restarts, and reboots.

## How it works

One Go binary runs an HTTP server on `127.0.0.1:6219` that serves:

- **Stored pages** at `/p/<page-id>`, HTML documents the agent PUTs to `/api/pages/<page-id>`.
- **An overview** at `/`, every stored document with open and delete buttons.
- **The agent contract** at `/SKILL.md`. An agent that reads it once knows the whole API.

### The contract

The agent includes one line in the HTML it generates:

```html
<script src="/sharedLocalStorage.js"></script>
```

That script replaces `window.localStorage` with a server-backed store scoped to the page. Everything the page writes with `localStorage.setItem()` is persisted in SQLite and readable by the agent. The script also provides `window.done()`, wired to a submit button to unblock the agent's wait. When state changes on the server the script fires the standard `storage` event, and when the agent republishes the page the tab reloads itself.

That's the whole API. No SDK, no framework, no build step.

### A round trip

```sh
# 1. publish (the browser opens the page, or open tabs reload)
curl -s -X PUT -H "X-Fui: 1" --data-binary @page.html http://127.0.0.1:6219/api/pages/deploy-checklist

# 2. wait for the Done click (long-poll; you own the timeout)
curl -s -H "X-Fui: 1" "http://127.0.0.1:6219/api/wait/deploy-checklist?timeout=540"
# -> {"done":true,"page_id":"deploy-checklist","state":{"run-tests":"true","notes":"..."}}

# 3. or peek at the state any time
curl -s http://127.0.0.1:6219/api/state/deploy-checklist

# 4. or push state for a live dashboard
curl -s -X POST -H "X-Fui: 1" -H "Content-Type: application/json" \
  -d '{"key":"progress","value":"42%"}' http://127.0.0.1:6219/api/state/migration
```

A wait that times out is not an error: the state is already persisted and the done click is remembered, so the agent simply calls again. The full API, the page contract and the styling rules are in [`assets/SKILL.md`](assets/SKILL.md), which the server serves at `/SKILL.md`.

### Styling: one optional line

The server also ships a **classless stylesheet**:

```html
<link rel="stylesheet" href="/style.css">
```

With that link, plain semantic HTML is already styled: `h1`–`h6`, `p`, `a`, `label`, `input`, `textarea`, `select`, `button`, `table`, `fieldset`/`legend`, `details`, `code`, `hr` and friends. The look is a warm, light-only Solarized paper with square edges and one accent family used only to mark real state. It exists mostly to save tokens: without it, every page has to carry its own CSS.

## Install

**Windows only.** The autostart, console-hiding and browser-launch paths are all Win32-specific. Requires [Go](https://go.dev/dl/) 1.25+:

```powershell
go install github.com/ojz/fui@latest

# start it now
fui serve

# start it automatically at every logon (HKCU Run key)
fui autostart install
```

`go install` puts the binary in `%USERPROFILE%\go\bin` (make sure that's on your `PATH`).

## Point your agent at it

No registration step. Add one line to the project's `CLAUDE.md` (or `AGENTS.md`, or just say it in the prompt):

```markdown
For multi-question input or visual output, use fui: `curl -s http://127.0.0.1:6219/SKILL.md`.
```

Agents with persistent memory will typically note the base URL after the first read and stop needing even that line. If you prefer a Claude Code skill, the same file installs as one:

```powershell
New-Item -ItemType Directory -Force $env:USERPROFILE\.claude\skills\fui | Out-Null
curl.exe -s http://127.0.0.1:6219/SKILL.md -o $env:USERPROFILE\.claude\skills\fui\SKILL.md
```

## Configuration

| Setting | Flag | Env | Default |
|---|---|---|---|
| Port | `--port` | `FUI_PORT` | `6219` |
| Database | `--db` | `FUI_DB` | `%APPDATA%\fui\fui.db` |

## Security

This is a personal, local tool, but "localhost" alone is not a security boundary, so it ships with three defenses:

- **Loopback bind**: the server listens on `127.0.0.1` only; nothing on your network can reach it.
- **Host header check**: every request must carry a `localhost`/`127.0.0.1` Host header, which defeats DNS-rebinding attacks.
- **Custom header on anything with side effects**: publishing, state writes, done signals, deletes and the long-poll wait (which consumes the done signal) require an `X-Fui` header. Cross-origin pages can't send custom headers without a CORS preflight, which the server never grants, so a random website can't blindly POST into your state. The read-only endpoints, including the SSE feed, are covered by the browser's own same-origin policy: the server never sends `Access-Control-Allow-Origin`.

What it deliberately does *not* have: authentication between local processes. Any program already running on your machine can talk to the server, the same trust model as your SSH agent or Docker socket.

## HTTP API

| Method | Path | Description |
|---|---|---|
| `PUT` | `/api/pages/{id}` | Store the HTML body, open the browser unless a tab is already viewing (`?open=0/1`, `?title=`) † |
| `GET` | `/api/wait/{id}` | Long-poll until `done()`; `?timeout=` seconds, default 600, max 86400 † |
| `GET` | `/api/state/{id}` | Full state map, or `?keys=a,b` |
| `POST` | `/api/state/{id}` | Upsert one key: `{"key": "...", "value": "..."}`, `value: null` deletes † |
| `DELETE` | `/api/state/{id}` | Clear all state † |
| `POST` | `/api/done/{id}` | Signal completion (what `done()` calls) † |
| `GET` | `/api/pages` | List pages |
| `DELETE` | `/api/pages/{id}` | Delete a page and its state † |
| `GET` | `/p/{id}` | Serve a stored page |
| `GET` | `/events/{id}` | SSE: state map on every change, `reload` event on republish |
| `GET` | `/SKILL.md` | The agent contract |
| `GET` | `/style.css` | Classless default stylesheet |

† requires the `X-Fui: 1` header.

## License

MIT
