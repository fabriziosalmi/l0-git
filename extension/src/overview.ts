import * as vscode from "vscode";
import * as path from "path";

// Stats matches server/store.go FindingsStats. Kept loose-typed where the
// shape isn't load-bearing in the renderer.
interface Stats {
  project: string;
  total: number;
  by_severity: Record<string, number>;
  by_status: Record<string, number>;
  by_gate: KeyCount[];
  by_tag: KeyCount[];
  top_files: KeyCount[];
  last_7_days: DayCount[];
}
interface KeyCount { key: string; count: number; }
interface DayCount { date: string; count: number; }

export interface OverviewDeps {
  runLGIT: (args: string[]) => Promise<string>;
  log: (msg: string) => void;
  runChecks: () => Promise<void>;
  setSearchQuery: (q: string) => void;
}

let panel: vscode.WebviewPanel | undefined;
let lastDeps: OverviewDeps | undefined;
let lastProject: string | undefined;

// refreshOverviewIfOpen lets external code (e.g. the tree's
// runChecksAndRefresh) keep the dashboard live without reaching into the
// panel internals. No-op when the panel is closed.
export async function refreshOverviewIfOpen(): Promise<void> {
  if (panel) await refreshOverview();
}

// showOverview opens (or focuses) the singleton Overview panel and pushes
// a fresh render. Re-callable from any command — second invocations just
// reveal the existing panel.
export async function showOverview(
  context: vscode.ExtensionContext,
  project: string,
  deps: OverviewDeps,
): Promise<void> {
  lastDeps = deps;
  lastProject = project;
  if (!panel) {
    panel = vscode.window.createWebviewPanel(
      "l0-git.overview",
      "l0-git: Overview",
      vscode.ViewColumn.One,
      {
        enableScripts: true,
        retainContextWhenHidden: true,
      },
    );
    panel.iconPath = new vscode.ThemeIcon("shield");
    panel.onDidDispose(() => { panel = undefined; }, undefined, context.subscriptions);
    panel.webview.onDidReceiveMessage(handleMessage, undefined, context.subscriptions);
  }
  panel.reveal(vscode.ViewColumn.One, true);
  await refreshOverview();
}

async function handleMessage(msg: { cmd: string; payload?: unknown }) {
  if (!panel || !lastDeps) return;
  switch (msg.cmd) {
    case "refresh":
      await refreshOverview();
      break;
    case "runChecks":
      await lastDeps.runChecks();
      await refreshOverview();
      break;
    case "filterGate": {
      const gateId = String(msg.payload ?? "");
      if (gateId) {
        lastDeps.setSearchQuery(gateId);
        await vscode.commands.executeCommand("l0-git.findings.focus");
      }
      break;
    }
    case "filterTag": {
      const tag = String(msg.payload ?? "");
      if (tag) {
        lastDeps.setSearchQuery(tag);
        await vscode.commands.executeCommand("l0-git.findings.focus");
      }
      break;
    }
    case "openFile": {
      const file = String(msg.payload ?? "");
      if (file && lastProject) {
        const abs = path.isAbsolute(file) ? file : path.join(lastProject, file);
        try {
          const doc = await vscode.workspace.openTextDocument(abs);
          await vscode.window.showTextDocument(doc, { preview: false });
        } catch (e) {
          lastDeps.log(`overview openFile: ${(e as Error).message}`);
        }
      }
      break;
    }
  }
}

async function refreshOverview(): Promise<void> {
  if (!panel || !lastDeps || !lastProject) return;
  let stats: Stats;
  try {
    const out = await lastDeps.runLGIT(["stats", `-project=${lastProject}`]);
    stats = JSON.parse(out || "{}") as Stats;
  } catch (e) {
    lastDeps.log(`overview refresh failed: ${(e as Error).message}`);
    panel.webview.html = renderError(panel.webview, (e as Error).message);
    return;
  }
  panel.webview.html = renderOverview(panel.webview, stats);
}

// =============================================================================
// HTML rendering
// =============================================================================

function renderOverview(webview: vscode.Webview, s: Stats): string {
  const nonce = makeNonce();
  const csp = [
    "default-src 'none'",
    `style-src ${webview.cspSource} 'unsafe-inline'`,
    `script-src 'nonce-${nonce}'`,
    "img-src data:",
  ].join("; ");

  const severityBars = renderSeverityBars(s.by_severity, s.total);
  const statusChips = renderStatusChips(s.by_status);
  const gateRows = renderGateRows(s.by_gate.slice(0, 8));
  const fileRows = renderFileRows(s.top_files.slice(0, 8));
  const tagChips = renderTagChips(s.by_tag.slice(0, 12));
  const sparkline = renderSparkline(s.last_7_days);

  const projectLabel = s.project ? path.basename(s.project) : "(all projects)";

  return `<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8">
<meta http-equiv="Content-Security-Policy" content="${csp}">
<style>
:root {
  --row-hover: var(--vscode-list-hoverBackground);
  --muted: var(--vscode-descriptionForeground);
  --error: var(--vscode-errorForeground, #f14c4c);
  --warning: var(--vscode-editorWarning-foreground, #cca700);
  --info: var(--vscode-charts-blue, #3794ff);
  --bar-bg: var(--vscode-input-background);
  --border: var(--vscode-panel-border);
  --chip-bg: var(--vscode-badge-background);
  --chip-fg: var(--vscode-badge-foreground);
}
body {
  font-family: var(--vscode-font-family);
  font-size: var(--vscode-font-size);
  color: var(--vscode-foreground);
  background: var(--vscode-editor-background);
  margin: 0;
  padding: 16px 20px;
}
.header { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; margin-bottom: 16px; flex-wrap: wrap; }
.header h1 { font-size: 1.3em; margin: 0; font-weight: 600; }
.header .project { color: var(--muted); font-size: 0.9em; }
.actions { display: flex; gap: 8px; }
button {
  font: inherit;
  background: var(--vscode-button-background);
  color: var(--vscode-button-foreground);
  border: none;
  padding: 4px 10px;
  border-radius: 2px;
  cursor: pointer;
}
button:hover { background: var(--vscode-button-hoverBackground); }
button.secondary { background: var(--vscode-button-secondaryBackground); color: var(--vscode-button-secondaryForeground); }
button.secondary:hover { background: var(--vscode-button-secondaryHoverBackground); }
.grid { display: grid; grid-template-columns: 1fr 1fr; gap: 24px; }
@media (max-width: 700px) { .grid { grid-template-columns: 1fr; } }
.card { border: 1px solid var(--border); border-radius: 4px; padding: 14px 16px; }
.card h2 { font-size: 0.9em; font-weight: 600; margin: 0 0 10px 0; text-transform: uppercase; letter-spacing: 0.04em; color: var(--muted); }
.totals { display: flex; align-items: baseline; gap: 12px; margin-bottom: 12px; }
.totals .total { font-size: 2.4em; font-weight: 600; line-height: 1; }
.totals .total-label { color: var(--muted); font-size: 0.9em; }
.sev-row { display: flex; align-items: center; gap: 10px; margin: 6px 0; }
.sev-label { width: 78px; font-size: 0.85em; }
.sev-bar { flex: 1; height: 10px; background: var(--bar-bg); border-radius: 2px; overflow: hidden; position: relative; }
.sev-bar-fill { height: 100%; }
.sev-error  { color: var(--error);   } .sev-error  .sev-bar-fill { background: var(--error); }
.sev-warning{ color: var(--warning); } .sev-warning .sev-bar-fill { background: var(--warning); }
.sev-info   { color: var(--info);    } .sev-info   .sev-bar-fill { background: var(--info); }
.sev-count {
  min-width: 78px;
  text-align: right;
  font-variant-numeric: tabular-nums;
  font-size: 0.9em;
  white-space: nowrap;
}
.row { display: flex; align-items: center; gap: 10px; padding: 4px 6px; border-radius: 2px; cursor: pointer; }
.row:hover { background: var(--row-hover); }
.row .key { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 0.9em; }
.row .count { font-variant-numeric: tabular-nums; color: var(--muted); font-size: 0.85em; min-width: 30px; text-align: right; }
.row .row-bar { width: 60px; height: 6px; background: var(--bar-bg); border-radius: 1px; overflow: hidden; }
.row .row-bar-fill { height: 100%; background: var(--info); }
.chips { display: flex; flex-wrap: wrap; gap: 6px; }
.chip {
  background: var(--chip-bg); color: var(--chip-fg);
  padding: 2px 8px; border-radius: 10px; font-size: 0.8em; cursor: pointer;
}
.chip:hover { opacity: 0.85; }
.chip .chip-count { opacity: 0.7; margin-left: 4px; }
.status-chips { display: flex; gap: 8px; flex-wrap: wrap; margin-top: 6px; font-size: 0.85em; color: var(--muted); }
.status-chips .status-chip { padding: 1px 8px; border: 1px solid var(--border); border-radius: 10px; }
.spark { display: flex; align-items: flex-end; gap: 4px; height: 70px; padding: 4px 0; }
.spark .bar { flex: 1; min-width: 14px; background: var(--info); opacity: 0.85; border-radius: 2px 2px 0 0; position: relative; }
.spark .bar.zero { opacity: 0.15; min-height: 2px; }
.spark .bar:hover { opacity: 1; }
.spark .bar .bar-tip { position: absolute; top: -16px; left: 50%; transform: translateX(-50%); font-size: 0.7em; color: var(--muted); white-space: nowrap; }
.spark-axis { display: flex; gap: 4px; margin-top: 4px; font-size: 0.7em; color: var(--muted); }
.spark-axis div { flex: 1; min-width: 14px; text-align: center; font-variant-numeric: tabular-nums; }
.empty { color: var(--muted); font-style: italic; padding: 6px 0; font-size: 0.9em; }
.full-row { grid-column: 1 / -1; }
</style>
</head>
<body>
<div class="header">
  <div>
    <h1>l0-git Overview</h1>
    <div class="project">${escapeHtml(projectLabel)}</div>
  </div>
  <div class="actions">
    <button id="run">▶ Run all checks</button>
    <button id="refresh" class="secondary">Refresh</button>
  </div>
</div>

<div class="grid">

  <div class="card">
    <h2>Total (all statuses)</h2>
    <div class="totals">
      <div class="total">${s.total}</div>
      <div class="total-label">${s.total === 1 ? "finding" : "findings"} stored — across open / ignored / resolved</div>
    </div>
    <div class="status-chips">
      ${statusChips}
    </div>
  </div>

  <div class="card">
    <h2>By severity (open)</h2>
    ${severityBars || `<div class="empty">No open findings.</div>`}
  </div>

  <div class="card">
    <h2>Top gates (open)</h2>
    ${gateRows || `<div class="empty">No open findings.</div>`}
  </div>

  <div class="card">
    <h2>Top files (open)</h2>
    ${fileRows || `<div class="empty">No file-scoped open findings.</div>`}
  </div>

  <div class="card full-row">
    <h2>Tags (open)</h2>
    ${tagChips || `<div class="empty">No tagged open findings.</div>`}
  </div>

  <div class="card full-row">
    <h2>Last 7 days (created_at)</h2>
    ${sparkline}
  </div>

</div>

<script nonce="${nonce}">
const vscode = acquireVsCodeApi();
document.getElementById("run").addEventListener("click", () => vscode.postMessage({ cmd: "runChecks" }));
document.getElementById("refresh").addEventListener("click", () => vscode.postMessage({ cmd: "refresh" }));
document.querySelectorAll("[data-gate]").forEach(el => {
  el.addEventListener("click", () => vscode.postMessage({ cmd: "filterGate", payload: el.getAttribute("data-gate") }));
});
document.querySelectorAll("[data-tag]").forEach(el => {
  el.addEventListener("click", () => vscode.postMessage({ cmd: "filterTag", payload: el.getAttribute("data-tag") }));
});
document.querySelectorAll("[data-file]").forEach(el => {
  el.addEventListener("click", () => vscode.postMessage({ cmd: "openFile", payload: el.getAttribute("data-file") }));
});
</script>
</body>
</html>`;
}

// =============================================================================
// fragment renderers
// =============================================================================

function renderSeverityBars(sev: Record<string, number>, total: number): string {
  const order: Array<keyof Record<string, number>> = ["error", "warning", "info"];
  const max = Math.max(1, ...order.map((k) => sev[k as string] || 0));
  return order
    .map((key) => {
      const n = sev[key as string] || 0;
      const pct = max > 0 ? Math.round((n / max) * 100) : 0;
      const totalPct = total > 0 ? Math.round((n / total) * 100) : 0;
      return `<div class="sev-row sev-${key}">
        <div class="sev-label">${cap(String(key))}</div>
        <div class="sev-bar"><div class="sev-bar-fill" style="width:${pct}%"></div></div>
        <div class="sev-count">${n}${total > 0 ? `<span style="opacity:0.6"> · ${totalPct}%</span>` : ""}</div>
      </div>`;
    })
    .join("");
}

function renderStatusChips(byStatus: Record<string, number>): string {
  const labels = ["open", "ignored", "resolved"];
  const parts = labels
    .filter((s) => (byStatus[s] || 0) > 0)
    .map((s) => `<span class="status-chip">${cap(s)}: ${byStatus[s]}</span>`);
  return parts.length > 0 ? parts.join("") : `<span class="empty">no findings yet</span>`;
}

function renderGateRows(rows: KeyCount[]): string {
  if (rows.length === 0) return "";
  const max = Math.max(1, ...rows.map((r) => r.count));
  return rows
    .map((r) => {
      const pct = Math.round((r.count / max) * 100);
      return `<div class="row" data-gate="${escapeAttr(r.key)}" title="Filter findings by ${escapeAttr(r.key)}">
        <div class="key">${escapeHtml(r.key)}</div>
        <div class="row-bar"><div class="row-bar-fill" style="width:${pct}%"></div></div>
        <div class="count">${r.count}</div>
      </div>`;
    })
    .join("");
}

function renderFileRows(rows: KeyCount[]): string {
  if (rows.length === 0) return "";
  const max = Math.max(1, ...rows.map((r) => r.count));
  return rows
    .map((r) => {
      const pct = Math.round((r.count / max) * 100);
      return `<div class="row" data-file="${escapeAttr(r.key)}" title="Open ${escapeAttr(r.key)}">
        <div class="key">${escapeHtml(r.key)}</div>
        <div class="row-bar"><div class="row-bar-fill" style="width:${pct}%"></div></div>
        <div class="count">${r.count}</div>
      </div>`;
    })
    .join("");
}

function renderTagChips(rows: KeyCount[]): string {
  if (rows.length === 0) return "";
  return `<div class="chips">${rows
    .map(
      (r) =>
        `<span class="chip" data-tag="${escapeAttr(r.key)}" title="Search findings tagged ${escapeAttr(r.key)}">${escapeHtml(r.key)}<span class="chip-count">${r.count}</span></span>`,
    )
    .join("")}</div>`;
}

function renderSparkline(days: DayCount[]): string {
  if (!days || days.length === 0) {
    return `<div class="empty">No created-at history yet.</div>`;
  }
  const max = Math.max(1, ...days.map((d) => d.count));
  const bars = days
    .map((d) => {
      const h = max > 0 ? Math.round((d.count / max) * 100) : 0;
      const cls = d.count === 0 ? "bar zero" : "bar";
      return `<div class="${cls}" style="height:${Math.max(h, 2)}%" title="${escapeAttr(d.date)}: ${d.count}">${
        d.count > 0 ? `<span class="bar-tip">${d.count}</span>` : ""
      }</div>`;
    })
    .join("");
  const axis = days.map((d) => `<div>${escapeHtml(shortDate(d.date))}</div>`).join("");
  return `<div class="spark">${bars}</div><div class="spark-axis">${axis}</div>`;
}

function renderError(_webview: vscode.Webview, msg: string): string {
  return `<!DOCTYPE html><html><body style="font-family:var(--vscode-font-family);padding:16px;">
    <h2>l0-git Overview</h2>
    <p>Failed to load stats: <code>${escapeHtml(msg)}</code></p>
  </body></html>`;
}

// =============================================================================
// helpers
// =============================================================================

function cap(s: string): string {
  return s.length === 0 ? s : s[0].toUpperCase() + s.slice(1);
}

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

function escapeAttr(s: string): string {
  return escapeHtml(s);
}

function shortDate(iso: string): string {
  // YYYY-MM-DD → MM-DD for axis labels.
  return iso.length === 10 ? iso.slice(5) : iso;
}

function makeNonce(): string {
  const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
  let out = "";
  for (let i = 0; i < 32; i++) out += chars.charAt(Math.floor(Math.random() * chars.length));
  return out;
}
