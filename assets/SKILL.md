---
name: fui
description: Render interactive HTML pages in the user's browser and read their input back over plain HTTP with curl. Use when asking the user several questions at once, gathering structured input (checklists, forms, ratings, triage decisions), showing live progress of a long-running task, or presenting rich visual output locally.
---

# fui: browser UIs over curl

fui is a local HTTP server at `http://127.0.0.1:6219`. You PUT an HTML page, the user's browser opens it, everything the page writes to `localStorage` is persisted on the server, and you read it back with GET. No MCP, no SDK: every call is one `curl`.

This document is served at `http://127.0.0.1:6219/SKILL.md`. It is worth a memory note along the lines of: *"fui renders HTML for user input and live dashboards; base URL http://127.0.0.1:6219; fetch /SKILL.md for the contract."*

Calls with side effects need the header `X-Fui: 1`. It is a CSRF guard against other websites, not a secret; curl just sends it. In PowerShell write `curl.exe`, since `curl` there may alias Invoke-WebRequest.

## Round trip

1. **Write the page to a file, then publish it.** Never inline a whole document on the command line.

   ```sh
   curl -s -X PUT -H "X-Fui: 1" --data-binary @page.html \
     "http://127.0.0.1:6219/api/pages/deploy-checklist"
   # {"page_id":"deploy-checklist","url":"http://127.0.0.1:6219/p/deploy-checklist","opened":true,"viewers":0}
   ```

   The browser opens the page unless it is already open somewhere; open tabs reload themselves to show the new version instead. `?open=1` forces a new tab, `?open=0` suppresses it. `?title=` overrides the `<title>` shown on the overview.

2. **Wait for the user to click a `done()` element.**

   ```sh
   curl -s -H "X-Fui: 1" "http://127.0.0.1:6219/api/wait/deploy-checklist?timeout=540"
   # {"done":true,"page_id":"deploy-checklist","state":{"notes":"...","run-tests":"true"}}
   # or {"done":false,"page_id":"...","reason":"timeout: ...","state":{...}}
   ```

   You own the timeout (seconds, default 600, max 86400). Keep it under your shell tool's limit, or run the curl in the background with a long timeout and get notified when it exits. A timeout is not an error: state is already persisted, just call again. Tell the user the page is waiting in their browser. The done signal is persisted, so a click that lands between two waits is returned by the next one.

3. **Read state at any time without waiting.**

   ```sh
   curl -s "http://127.0.0.1:6219/api/state/deploy-checklist"                  # whole map
   curl -s "http://127.0.0.1:6219/api/state/deploy-checklist?keys=notes,run-tests"
   ```

## Live dashboards: write state from the agent

```sh
curl -s -X POST -H "X-Fui: 1" -H "Content-Type: application/json" \
  -d '{"key":"progress","value":"3/10 files migrated"}' \
  "http://127.0.0.1:6219/api/state/migration"
```

`"value": null` deletes the key. Every open page on that id receives the standard `storage` event immediately (see *Live update pattern*). For a long-running task, publish a dashboard once, then POST a key per step; the user watches it move with no further page work from you. Subagents can share one page id as a blackboard; no page is needed for that, state exists independently of HTML.

## The page contract

1. Include `<script src="/sharedLocalStorage.js"></script>` as the FIRST script in your HTML.
2. `localStorage.setItem(key, value)` / `getItem` / `removeItem` / `clear` now read and write server-persisted, per-page state. Always use the method calls; direct property assignment (`localStorage.foo = "x"`) does NOT sync.
3. `window.done()` signals completion and unblocks `/api/wait`. Wire it to your submit button. **Submitting only sends**, see below.
4. Include `<link rel="stylesheet" href="/style.css">` and write plain semantic HTML. This replaces writing your own CSS.

## Submitting must only send

Clicking submit calls `done()` and does **nothing else**. Do not assign to `document.body.innerHTML`, hide or remove the form, navigate away, or otherwise tear the page down in the click handler. The user goes on reading, checking and revising their answers after they submit; a page that blanks itself destroys exactly the thing they wanted to look at, and hides the values the server is still holding.

Confirm in place: leave every answer on screen and change the button label to "Sent ✓", optionally disabling it or adding one short line of feedback beside it. Re-submitting is harmless, `done()` just fires again, so leaving the button live is fine.

## Styling: don't write CSS, link the stylesheet

The server serves a **classless stylesheet** at `/style.css`. One link tag and bare semantic tags are already styled.

- Covers `h1`–`h6`, `p`, `a`, `ul`/`ol`, `label`, `input` (incl. checkbox, radio, range), `textarea`, `select`, `button`, `table`, `fieldset`/`legend`, `details`/`summary`, `blockquote`, `code`/`pre`, `hr`, `progress`.
- **Almost no classes needed.** Use `<fieldset><legend>…</legend>` to group fields into a card and `<aside>` for a note or callout; both are already cards, so building one out of a `div` and a class only fights the sheet. A bare `<label>` renders as a small uppercase field caption, while a `<label>` wrapping its own checkbox stays a normal readable sentence.
- **The sheet is light-only. Never write a `prefers-color-scheme: dark` block.** Yours would flip your text to a light grey while the sheet keeps its light paper background, and the page becomes unreadable for anyone whose OS is in dark mode. This is the single most damaging thing you can add.
- The aesthetic is opinionated and self-consistent: warm paper, square edges, generous whitespace, and one accent family reserved for real state (checked, focused, open, primary action). Don't fight it with a competing palette of your own greys, blues and greens.
- **The primary action must be `<button type="submit">` or `<button class="primary">`** to wear the accent. A plain `<button type="button">`, the obvious choice when wiring `onclick="done()"`, is styled as *secondary* chrome, which leaves the one control the page exists for looking like a cancel button.
- **Wrap a wide table in `<figure>`** (`<figure><table>…</table><figcaption>…</figcaption></figure>`): the figure scrolls horizontally on its own, where a bare wide table pushes the whole page sideways. Normal-width tables need no wrapper.
- Radios and checkboxes are both hard squares; the radio carries a small diamond even when empty, so single-choice reads apart from multi-choice. Don't restyle them.
- A fixed or sticky action bar should be painted `var(--fu-bg)` with a `1px solid var(--fu-rule-strong)` edge; a `background: #fff` bar reads as a white slab dropped on the paper.
- You are still free to add a `<style>` block after the link, and your own classes, for anything page-specific: layout grids, a chart, a custom widget. The sheet only targets bare tags, so custom rules win without `!important`. Reuse its palette rather than inventing one: `--fu-bg`, `--fu-panel`, `--fu-panel-strong`, `--fu-sunken`, `--fu-ink`, `--fu-ink-soft`, `--fu-ink-faint`, `--fu-rule`, `--fu-rule-strong`, `--fu-accent`, `--fu-mono`.

## Live update pattern

The sync script holds an `EventSource` on the server and fires the standard `storage` event as soon as state changes elsewhere. A page you publish once keeps updating live when you POST to `/api/state/<id>`, or when another tab on the same id writes with `localStorage.setItem`. No reload, no polling.

```html
<script src="/sharedLocalStorage.js"></script>
<progress id="bar" max="10" value="0"></progress>
<ul id="log"></ul>
<script>
  function render() {
    document.getElementById("bar").value = localStorage.getItem("step") || 0;
  }
  render();
  window.addEventListener("storage", function (e) {
    var li = document.createElement("li");
    li.textContent = e.key + " = " + e.newValue;
    document.getElementById("log").appendChild(li);
    render();
  });
</script>
```

Render once, then POST `step` as the task advances. Always render from `localStorage` on load too, so a tab opened late shows the current values.

## Canonical pattern: a question form

```html
<!DOCTYPE html>
<html><head>
<meta charset="utf-8">
<title>Deploy checklist</title>
<script src="/sharedLocalStorage.js"></script>
<link rel="stylesheet" href="/style.css">
</head><body>
  <h1>Deploy checklist</h1>
  <fieldset>
    <legend>Steps</legend>
    <label><input type="checkbox" data-key="run-tests"> Run tests first</label>
    <label><input type="checkbox" data-key="bump-version"> Bump the version</label>
  </fieldset>
  <label for="notes">Notes</label>
  <textarea id="notes" data-key="notes" placeholder="Anything else?"></textarea>
  <button type="submit" id="submit">Done — send to agent</button>
<script>
  // Restore persisted values and sync every change.
  document.querySelectorAll("[data-key]").forEach(el => {
    const k = el.dataset.key;
    if (el.type === "checkbox") {
      el.checked = localStorage.getItem(k) === "true";
      el.onchange = () => localStorage.setItem(k, el.checked);
    } else {
      el.value = localStorage.getItem(k) ?? "";
      el.oninput = () => localStorage.setItem(k, el.value);
    }
  });
  const btn = document.getElementById("submit");
  btn.onclick = () => {
    done();
    // Confirm in place. Never blank or replace the page.
    btn.textContent = "Sent ✓";
  };
</script>
</body></html>
```

PUT it, then GET `/api/wait/<id>`. The returned `state` contains every `data-key` value.

## Rules of thumb

- Sync state on every change (`onchange`/`oninput`), never only at submit, so nothing is lost on timeout, crash, or accidental tab close.
- Submit sends and nothing more: never blank, replace or navigate away from the page in the click handler.
- Pick a descriptive, task-scoped page id such as `deploy-checklist-2026-10`. Allowed characters: letters, digits, `.`, `_`, `-`. State is scoped per id; reusing an old id inherits its old state, which is sometimes what you want.
- Multiple rounds work: after a wait returns, PUT an updated page under the same id (the open tab reloads itself) and wait again.
- Store structured values as JSON strings: `localStorage.setItem("answers", JSON.stringify(obj))`.
- Link `/style.css` and lean on plain semantic tags instead of writing CSS. Reach for a `<style>` block only for what the sheet genuinely doesn't cover, and never for a dark-mode variant.
- If a wait times out, tell the user the page is waiting in their browser and wait again rather than giving up.

## API reference

Base URL `http://127.0.0.1:6219`. Rows marked † require `X-Fui: 1`.

| Method | Path | Body / query | Returns |
|---|---|---|---|
| `PUT` | `/api/pages/{id}` † | HTML document; `?open=0|1`, `?title=` | `{page_id, url, opened, viewers}` |
| `GET` | `/api/wait/{id}` † | `?timeout=` seconds (default 600, max 86400) | `{done, page_id, state[, reason]}` |
| `GET` | `/api/state/{id}` | `?keys=a,b` | key/value map |
| `POST` | `/api/state/{id}` † | `{"key": "...", "value": "..."}`, `null` deletes | `{ok}` |
| `DELETE` | `/api/state/{id}` † | | `{ok}` |
| `POST` | `/api/done/{id}` † | | `{ok}` (what `done()` calls) |
| `GET` | `/api/pages` | | list of pages |
| `DELETE` | `/api/pages/{id}` † | | `{deleted}` |
| `GET` | `/p/{id}` | | the page itself |
| `GET` | `/events/{id}` | | SSE: state map on change, `reload` on republish |
| `GET` | `/SKILL.md` | | this document |
