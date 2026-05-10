import * as vscode from "vscode";
import { execFile, spawn, ChildProcess } from "child_process";
import * as path from "path";
import * as fs from "fs";
import { Stub, stubFor, licenseChoices, licenseStub } from "./stubs";
import { showOverview, refreshOverviewIfOpen } from "./overview";

interface Finding {
  id: number;
  project: string;
  gate_id: string;
  severity: string;
  title: string;
  message: string;
  file_path: string;
  tags: string;
  status: string;
  created_at: number;
  updated_at: number;
  // blame is populated client-side when the l0-git.showBlame setting is on.
  // Not part of the backend payload.
  blame?: BlameInfo;
}

interface BlameInfo {
  hash: string;       // commit SHA (full)
  author: string;     // author name from `git blame --line-porcelain`
  authorTime: number; // unix seconds
}

interface CheckResult {
  project: string;
  gates_run: string[];
  findings: Finding[];
}

// =============================================================================
// View state — persisted across sessions, drives the tree.
// =============================================================================

type GroupBy = "none" | "severity" | "gate" | "file" | "tag" | "status";
type SortBy = "updated" | "created" | "severity" | "gate" | "file";
type StatusFilter = "open" | "ignored" | "resolved" | "all";

interface ViewState {
  groupBy: GroupBy;
  sortBy: SortBy;
  status: StatusFilter;
  severities: { error: boolean; warning: boolean; info: boolean };
  query: string;
}

// Info findings are off by default — they're audit-trail / nice-to-have
// (TODO comments, missing CONTRIBUTING.md, override_accepted, …) and
// drown out errors and warnings when they share the same tree. Toggle
// them on via the severity filter when you want the full picture.
const DEFAULT_VIEW_STATE: ViewState = {
  groupBy: "severity",
  sortBy: "severity",
  status: "open",
  severities: { error: true, warning: true, info: false },
  query: "",
};

const VIEW_STATE_KEY = "l0-git.viewState";

function loadViewState(context: vscode.ExtensionContext): ViewState {
  const stored = context.globalState.get<Partial<ViewState>>(VIEW_STATE_KEY);
  return {
    ...DEFAULT_VIEW_STATE,
    ...(stored ?? {}),
    severities: { ...DEFAULT_VIEW_STATE.severities, ...(stored?.severities ?? {}) },
  };
}

function saveViewState(context: vscode.ExtensionContext, state: ViewState): void {
  void context.globalState.update(VIEW_STATE_KEY, state);
}

let mcpProcess: ChildProcess | undefined;
let provider: FindingsTreeProvider;
let outputChannel: vscode.OutputChannel;
let diagnostics: vscode.DiagnosticCollection;
let statusBar: vscode.StatusBarItem;
let treeView: vscode.TreeView<TreeNode>;
// findings already shown to the user; lets us notify only on new ones
const seenFindingKeys = new Set<string>();

// Gate IDs for which we know how to generate a stub. Mirrors the cases
// handled in stubs.stubFor / the LICENSE branch — kept here so the
// CodeActionProvider can advertise actions without invoking the generator.
const fixableGates = new Set<string>([
  "readme_present",
  "license_present",
  "contributing_present",
  "security_present",
  "changelog_present",
  "gitignore_present",
  "pr_template_present",
  "issue_template_present",
  "ci_workflow_present",
  "branch_protection_declared",
]);

export function activate(context: vscode.ExtensionContext) {
  outputChannel = vscode.window.createOutputChannel("l0-git");
  context.subscriptions.push(outputChannel);

  diagnostics = vscode.languages.createDiagnosticCollection("l0-git");
  context.subscriptions.push(diagnostics);

  statusBar = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 100);
  statusBar.command = "l0-git.findings.focus";
  statusBar.text = "$(shield) l0-git";
  statusBar.tooltip = "l0-git — click to open the findings view";
  statusBar.show();
  context.subscriptions.push(statusBar);

  // CodeActions: every l0-git diagnostic with a known stub gets a
  // "Generate stub" quick fix in the lightbulb menu.
  context.subscriptions.push(
    vscode.languages.registerCodeActionsProvider({ scheme: "*" }, new L0GitCodeActions(), {
      providedCodeActionKinds: [vscode.CodeActionKind.QuickFix],
    }),
  );

  provider = new FindingsTreeProvider(context);
  treeView = vscode.window.createTreeView("l0-git.findings", {
    treeDataProvider: provider,
    showCollapseAll: true,
  });
  context.subscriptions.push(treeView);

  context.subscriptions.push(
    vscode.commands.registerCommand("l0-git.refresh", () => provider.refresh()),
    vscode.commands.registerCommand("l0-git.runChecks", () => runChecksAndRefresh(context)),
    vscode.commands.registerCommand("l0-git.openFinding", (item: FindingItem) => openFinding(item)),
    vscode.commands.registerCommand("l0-git.ignoreFinding", (item: FindingItem) => ignoreFinding(context, item)),
    vscode.commands.registerCommand("l0-git.deleteFinding", (item: FindingItem) => deleteFinding(context, item)),
    vscode.commands.registerCommand("l0-git.clearProject", () => clearProject(context)),
    vscode.commands.registerCommand("l0-git.startServer", () => startMCP(context)),
    vscode.commands.registerCommand("l0-git.stopServer", () => stopMCP()),
    vscode.commands.registerCommand("l0-git.applyFix", (project: string, gateId: string) => applyFix(context, project, gateId)),
    vscode.commands.registerCommand("l0-git.showRemediation", (item: FindingItem) => showRemediation(context, item)),
    vscode.commands.registerCommand("l0-git.copyClaudePrompt", (item: FindingItem) => copyClaudePrompt(context, item)),
    vscode.commands.registerCommand("l0-git.setGroupBy", () => promptGroupBy()),
    vscode.commands.registerCommand("l0-git.setSortBy", () => promptSortBy()),
    vscode.commands.registerCommand("l0-git.setStatusFilter", () => promptStatusFilter()),
    vscode.commands.registerCommand("l0-git.toggleSeverity", () => promptSeverityFilter()),
    vscode.commands.registerCommand("l0-git.search", () => promptSearch()),
    vscode.commands.registerCommand("l0-git.clearFilters", () => resetFilters()),
    vscode.commands.registerCommand("l0-git.showOverview", () => openOverview(context)),
  );

  // Re-render when settings change so binary/db overrides take effect.
  context.subscriptions.push(
    vscode.workspace.onDidChangeConfiguration((e) => {
      if (!e.affectsConfiguration("l0-git")) return;
      if (e.affectsConfiguration("l0-git.binaryPath")) {
        const cfg = vscode.workspace.getConfiguration("l0-git").get<string>("binaryPath");
        if (cfg && cfg.trim() && !fs.existsSync(cfg.trim())) {
          void vscode.window.showWarningMessage(
            `l0-git: binary not found at '${cfg.trim()}'. Check the l0-git.binaryPath setting.`,
            "Open settings",
          ).then((choice) => {
            if (choice === "Open settings") {
              void vscode.commands.executeCommand("workbench.action.openSettings", "l0-git.binaryPath");
            }
          });
        }
      }
      provider.refresh();
    }),
  );

  // Re-run checks and register new file watchers when workspace folders change.
  context.subscriptions.push(
    vscode.workspace.onDidChangeWorkspaceFolders((e) => {
      // Register watchers for newly added folders so they get the same
      // file-change triggers as folders present at activation time.
      if (e.added.length > 0) {
        registerReadmeWatchersForFolders(context, e.added);
      }
      void runChecksAndRefresh(context);
    }),
  );

  // Watch root-level README* per workspace folder; create/delete should
  // re-run the gate so the sidebar stays current.
  registerReadmeWatchers(context);

  if (vscode.workspace.getConfiguration("l0-git").get<boolean>("autoStartMCP")) {
    startMCP(context).catch((err) => outputChannel.appendLine(`autoStart failed: ${err}`));
  }
  context.subscriptions.push({ dispose: () => stopMCP() });

  if (vscode.workspace.getConfiguration("l0-git").get<boolean>("runOnStartup")) {
    runChecksAndRefresh(context).catch((err) => outputChannel.appendLine(`startup check failed: ${err}`));
  } else {
    provider.refresh();
  }
}

export function deactivate() {
  stopMCP();
}

function registerReadmeWatchers(context: vscode.ExtensionContext) {
  registerReadmeWatchersForFolders(context, vscode.workspace.workspaceFolders ?? []);
}

function registerReadmeWatchersForFolders(
  context: vscode.ExtensionContext,
  folders: readonly vscode.WorkspaceFolder[],
) {
  // Watch every file the registered gates care about as INPUT (presence
  // or content used by the gate's decision). Source files are NOT
  // watched here — content scanners (secrets, network, conn_strings,
  // dead_placeholders, html/css/markdown) re-run as part of every full
  // check and would otherwise re-trigger on every keystroke.
  //
  // Each pattern is a RelativePattern under a workspace folder; VSCode
  // dedupes file events so listing the same file under multiple patterns
  // is fine.
  const patterns = [
    // Project-hygiene presence gates
    "README*", "LICENSE*", "COPYING*", "CONTRIBUTING*", "SECURITY*",
    "CHANGELOG*", "CHANGES*", "HISTORY*",
    // Project config + configuration
    ".gitignore", ".gitattributes", ".l0git.json",
    // Toolchain pinning
    ".nvmrc", ".node-version",
    // Governance
    "CODE_OF_CONDUCT*", "CODEOWNERS",
    ".github/CODEOWNERS", "docs/CODEOWNERS",
    ".github/CODE_OF_CONDUCT*", "docs/CODE_OF_CONDUCT*",
    // .env contract
    ".env.example", ".env.sample", ".env.template", ".env.dist",
    // Containers
    "Dockerfile", "Dockerfile.*",
    "docker-compose.yml", "docker-compose.yaml",
    "docker-compose.override.yml", "docker-compose.override.yaml",
    "compose.yml", "compose.yaml",
    "compose.override.yml", "compose.override.yaml",
    // Manifests (version_drift inputs)
    "package.json", "Cargo.toml", "pyproject.toml", "setup.py",
    "mix.exs", "pom.xml", "VERSION", "version.txt",
    // GitHub-specific
    ".github/PULL_REQUEST_TEMPLATE.md", ".github/pull_request_template.md",
    ".github/ISSUE_TEMPLATE/**", ".github/workflows/**",
  ];
  const trigger = () =>
    runChecksAndRefresh(context).catch((err) =>
      outputChannel.appendLine(`watcher recheck failed: ${(err as Error).message}`),
    );
  for (const f of folders) {
    for (const p of patterns) {
      const watcher = vscode.workspace.createFileSystemWatcher(new vscode.RelativePattern(f, p));
      watcher.onDidCreate(trigger);
      watcher.onDidDelete(trigger);
      watcher.onDidChange(trigger);
      context.subscriptions.push(watcher);
    }
  }
}

function bundledBinaryPath(extensionPath: string): string {
  const goOS = process.platform === "win32" ? "windows" : process.platform;
  const goArch = process.arch === "x64" ? "amd64" : process.arch;
  const exe = process.platform === "win32" ? "lgit.exe" : "lgit";
  return path.join(extensionPath, "bin", `${goOS}-${goArch}`, exe);
}

function resolveBinary(context: vscode.ExtensionContext): string {
  const cfg = vscode.workspace.getConfiguration("l0-git").get<string>("binaryPath");
  if (cfg && cfg.trim()) return cfg.trim();

  const bundled = bundledBinaryPath(context.extensionPath);
  if (fs.existsSync(bundled)) return bundled;

  // Dev layout: server folder next to extension (this repo's git layout).
  const devExe = process.platform === "win32" ? "lgit.exe" : "lgit";
  const dev = path.join(context.extensionPath, "..", "server", devExe);
  if (fs.existsSync(dev)) return dev;

  const home = process.env.HOME || process.env.USERPROFILE || "";
  const candidates = [
    "/usr/local/bin/lgit",
    "/opt/homebrew/bin/lgit",
    path.join(home, ".local", "bin", "lgit"),
    path.join(home, "go", "bin", "lgit"),
  ];
  for (const c of candidates) if (fs.existsSync(c)) return c;

  return process.platform === "win32" ? "lgit.exe" : "lgit";
}

async function notifyBinaryMissing(message: string): Promise<void> {
  const choice = await vscode.window.showErrorMessage(
    `l0-git: ${message}`,
    "Set binary path",
    "Open output",
  );
  if (choice === "Set binary path") {
    await vscode.commands.executeCommand("workbench.action.openSettings", "l0-git.binaryPath");
  } else if (choice === "Open output") {
    outputChannel.show(true);
  }
}

function envWithDB(): NodeJS.ProcessEnv {
  const db = vscode.workspace.getConfiguration("l0-git").get<string>("dbPath");
  const env = { ...process.env };
  if (db && db.trim()) env.LGIT_DB = db.trim();
  return env;
}

class BinaryNotFoundError extends Error {
  constructor(public readonly binPath: string) {
    super(`lgit binary not found at '${binPath}'. Set 'l0-git.binaryPath' or install the binary.`);
    this.name = "BinaryNotFoundError";
  }
}

// lgitQueue serialises every shell-out to the lgit binary so the extension
// never spawns two processes against the same SQLite DB at once. SQLite WAL
// tolerates multiple writers via busy_timeout, but the extension can easily
// fan out (watcher → check + list × N folders + tree refresh) and hit
// SQLITE_BUSY_RECOVERY on cold opens. Keeping it single-file fixes that
// at the source.
let lgitQueue: Promise<unknown> = Promise.resolve();

function runLGIT(context: vscode.ExtensionContext, args: string[]): Promise<string> {
  const job = lgitQueue.then(() => spawnLGIT(context, args));
  // Mask rejections on the chain so one failure doesn't poison every
  // subsequent call. Each caller still sees its own rejection via `job`.
  lgitQueue = job.catch(() => undefined);
  return job;
}

function spawnLGIT(context: vscode.ExtensionContext, args: string[]): Promise<string> {
  return new Promise((resolve, reject) => {
    const bin = resolveBinary(context);
    execFile(bin, args, { env: envWithDB(), maxBuffer: 32 * 1024 * 1024 }, (err, stdout, stderr) => {
      if (err) {
        outputChannel.appendLine(`[lgit ${args.join(" ")}] error: ${err.message}\n${stderr}`);
        if ((err as NodeJS.ErrnoException).code === "ENOENT") {
          reject(new BinaryNotFoundError(bin));
          return;
        }
        reject(new Error(stderr || err.message));
        return;
      }
      resolve(stdout);
    });
  });
}

function workspaceRoots(): string[] {
  const folders = vscode.workspace.workspaceFolders ?? [];
  return folders.map((f) => f.uri.fsPath);
}

// Coalesce watcher bursts. While one run is in flight, additional triggers
// just set the "rerun" flag — when the current pass finishes we kick off
// exactly one more. Eliminates the N-events-fire-N-runs pattern when files
// land in quick succession (e.g. on workspace open or a multi-file save).
let activeRun: Promise<void> | null = null;
let pendingRerun = false;

function runChecksAndRefresh(context: vscode.ExtensionContext): Promise<void> {
  if (activeRun) {
    pendingRerun = true;
    return activeRun;
  }
  activeRun = doRunChecksAndRefresh(context).finally(() => {
    activeRun = null;
    if (pendingRerun) {
      pendingRerun = false;
      void runChecksAndRefresh(context);
    }
  });
  return activeRun;
}

async function doRunChecksAndRefresh(context: vscode.ExtensionContext): Promise<void> {
  statusBar.text = "$(loading~spin) l0-git: checking…";
  statusBar.tooltip = "l0-git — running gates…";
  const roots = workspaceRoots();
  if (roots.length === 0) {
    provider.refresh();
    return;
  }
  const newlyOpen: Finding[] = [];
  for (const root of roots) {
    try {
      const out = await runLGIT(context, ["check", root]);
      const res = JSON.parse(out || "{}") as CheckResult;
      for (const f of res.findings ?? []) {
        if (f.status !== "open") continue;
        const key = findingKey(f);
        if (!seenFindingKeys.has(key)) {
          seenFindingKeys.add(key);
          newlyOpen.push(f);
        }
      }
    } catch (e: unknown) {
      const err = e as Error;
      if (err instanceof BinaryNotFoundError) {
        await notifyBinaryMissing(err.message);
        return;
      }
      vscode.window.showErrorMessage(`l0-git check failed for ${root}: ${err.message}`);
    }
  }
  // Toasts are reserved for errors. Warning/info toasts on every workspace
  // open trained users to dismiss without reading — defeating the point.
  // Errors-only keeps the interruption budget for things that actually need
  // a human now (leaked secret, merge conflict marker, …).
  const newErrors = newlyOpen.filter((f) => f.severity === "error");
  if (newErrors.length > 0 && vscode.workspace.getConfiguration("l0-git").get<boolean>("notifyOnNew")) {
    notifyNewFindings(context, newErrors);
  }
  await syncDiagnostics(context, roots);
  provider.refresh();
  // Keep the Overview dashboard live if it's currently open. No-op when
  // the panel is closed.
  void refreshOverviewIfOpen();
}

// syncDiagnostics rebuilds the DiagnosticCollection from the current set of
// open findings across the given workspace roots. Findings without a
// file_path are pinned to the project root URI so the Problems pane still
// surfaces them (clicking opens the folder).
async function syncDiagnostics(context: vscode.ExtensionContext, roots: string[]) {
  diagnostics.clear();
  const byUri = new Map<string, vscode.Diagnostic[]>();
  const counts = { error: 0, warning: 0, info: 0 };
  for (const root of roots) {
    let findings: Finding[];
    try {
      // Diagnostics always reflect the full set of open findings,
      // independent of the user's tree view filters — the Problems
      // pane is the universal "what's broken" surface.
      const out = await runLGIT(context, ["list", `-project=${root}`, "-status=open", "-limit=500"]);
      findings = JSON.parse(out || "[]") as Finding[];
    } catch (e: unknown) {
      outputChannel.appendLine(`diagnostics fetch failed for ${root}: ${(e as Error).message}`);
      continue;
    }
    for (const f of findings) {
      const target = findingTargetUri(f);
      const range = new vscode.Range(0, 0, 0, 0);
      const diag = new vscode.Diagnostic(range, `${f.title} — ${f.message}`, severityToDiag(f.severity));
      diag.source = "l0-git";
      diag.code = f.gate_id;
      const key = target.toString();
      const arr = byUri.get(key) ?? [];
      arr.push(diag);
      byUri.set(key, arr);
      if (f.severity === "error") counts.error++;
      else if (f.severity === "warning") counts.warning++;
      else counts.info++;
    }
  }
  for (const [uri, diags] of byUri) {
    diagnostics.set(vscode.Uri.parse(uri), diags);
  }
  updateStatusBar(counts);
}

// updateStatusBar reflects the open finding totals in the bottom-left bar.
// The icon changes by worst severity; the tooltip lists the breakdown.
function updateStatusBar(counts: { error: number; warning: number; info: number }) {
  const total = counts.error + counts.warning + counts.info;
  if (total === 0) {
    statusBar.text = "$(check) l0-git: clean";
    statusBar.tooltip = "l0-git — no open findings";
    statusBar.backgroundColor = undefined;
    return;
  }
  const icon = counts.error > 0 ? "$(error)" : counts.warning > 0 ? "$(warning)" : "$(info)";
  statusBar.text = `${icon} l0-git: ${total}`;
  const lines = [
    counts.error > 0   ? `errors: ${counts.error}`     : "",
    counts.warning > 0 ? `warnings: ${counts.warning}` : "",
    counts.info > 0    ? `info: ${counts.info}`        : "",
  ].filter(Boolean);
  statusBar.tooltip = `l0-git — ${lines.join(", ")} (click to open)`;
  // Don't paint the bar red — that's reserved by VSCode for blocking issues
  // and causes visual fatigue when warnings dominate. Tooltip + icon is
  // enough signal.
  statusBar.backgroundColor = undefined;
}

function findingTargetUri(f: Finding): vscode.Uri {
  if (f.file_path) {
    const abs = path.isAbsolute(f.file_path) ? f.file_path : path.join(f.project, f.file_path);
    return vscode.Uri.file(abs);
  }
  return vscode.Uri.file(f.project);
}

function severityToDiag(sev: string): vscode.DiagnosticSeverity {
  switch (sev) {
    case "error":   return vscode.DiagnosticSeverity.Error;
    case "warning": return vscode.DiagnosticSeverity.Warning;
    default:        return vscode.DiagnosticSeverity.Information;
  }
}

function findingKey(f: Finding): string {
  return `${f.project}|${f.gate_id}|${f.file_path}`;
}

function notifyNewFindings(context: vscode.ExtensionContext, findings: Finding[]) {
  // One toast per finding keeps the message specific; cap at 3 to avoid noise.
  const shown = findings.slice(0, 3);
  for (const f of shown) {
    const fn = severityToToast(f.severity);
    void fn(`l0-git: ${f.title} — ${path.basename(f.project)}`, "View", "Ignore").then((choice) => {
      if (choice === "View") {
        void vscode.commands.executeCommand("l0-git.findings.focus");
      } else if (choice === "Ignore") {
        void runLGIT(context, ["ignore", String(f.id)])
          .then(() => provider.refresh())
          .catch((err) => outputChannel.appendLine(`ignore failed: ${(err as Error).message}`));
      }
    });
  }
  if (findings.length > shown.length) {
    void vscode.window.showInformationMessage(
      `l0-git: ${findings.length - shown.length} more findings — see the l0-git view.`,
    );
  }
}

function severityToToast(sev: string): typeof vscode.window.showWarningMessage {
  switch (sev) {
    case "error": return vscode.window.showErrorMessage;
    case "info":  return vscode.window.showInformationMessage;
    default:      return vscode.window.showWarningMessage;
  }
}

async function openFinding(item: FindingItem) {
  if (!item || !item.finding) return;
  const f = item.finding;
  if (f.file_path) {
    const fl = findingFileLine(f);
    const absFile = fl
      ? (path.isAbsolute(fl.file) ? fl.file : path.join(f.project, fl.file))
      : (path.isAbsolute(f.file_path) ? f.file_path : path.join(f.project, f.file_path));
    if (fs.existsSync(absFile)) {
      const doc = await vscode.workspace.openTextDocument(absFile);
      const editor = await vscode.window.showTextDocument(doc, { preview: false });
      if (fl && fl.line > 1) {
        const lineIdx = fl.line - 1;
        const range = new vscode.Range(lineIdx, 0, lineIdx, 0);
        editor.revealRange(range, vscode.TextEditorRevealType.InCenterIfOutsideViewport);
        editor.selection = new vscode.Selection(lineIdx, 0, lineIdx, 0);
      }
      return;
    }
  }
  const ts = new Date(f.updated_at).toLocaleString();
  const body =
    `# ${f.title}\n\n` +
    `_gate:_ ${f.gate_id}  \n` +
    `_severity:_ ${f.severity}  \n` +
    `_project:_ ${f.project}  \n` +
    `_file:_ ${f.file_path || "—"}  \n` +
    `_status:_ ${f.status}  \n` +
    `_updated:_ ${ts}\n\n` +
    `---\n\n` +
    `${f.message}\n`;
  const doc = await vscode.workspace.openTextDocument({ content: body, language: "markdown" });
  await vscode.window.showTextDocument(doc, { preview: false });
}

async function ignoreFinding(context: vscode.ExtensionContext, item: FindingItem) {
  if (!item || !item.finding) return;
  try {
    await runLGIT(context, ["ignore", String(item.finding.id)]);
    provider.refresh();
  } catch (e: unknown) {
    const err = e as Error;
    if (err instanceof BinaryNotFoundError) return notifyBinaryMissing(err.message);
    vscode.window.showErrorMessage(`Ignore failed: ${err.message}`);
  }
}

async function deleteFinding(context: vscode.ExtensionContext, item: FindingItem) {
  if (!item || !item.finding) return;
  try {
    await runLGIT(context, ["delete", String(item.finding.id)]);
    seenFindingKeys.delete(findingKey(item.finding));
    provider.refresh();
  } catch (e: unknown) {
    const err = e as Error;
    if (err instanceof BinaryNotFoundError) return notifyBinaryMissing(err.message);
    vscode.window.showErrorMessage(`Delete failed: ${err.message}`);
  }
}

async function clearProject(context: vscode.ExtensionContext) {
  const roots = workspaceRoots();
  if (roots.length === 0) {
    vscode.window.showInformationMessage("l0-git: no workspace folder open.");
    return;
  }
  const target = roots.length === 1
    ? roots[0]
    : await vscode.window.showQuickPick(roots, { placeHolder: "Project to clear" });
  if (!target) return;
  let countLabel = "";
  try {
    const out = await runLGIT(context, ["list", `-project=${target}`, "-status=all", "-limit=5000"]);
    const all = JSON.parse(out || "[]") as Finding[];
    countLabel = ` (${all.length} finding${all.length === 1 ? "" : "s"})`;
  } catch { /* count fetch is best-effort */ }
  const confirm = await vscode.window.showWarningMessage(
    `Delete all l0-git findings for ${path.basename(target)}${countLabel}?`,
    { modal: true },
    "Delete",
  );
  if (confirm !== "Delete") return;
  try {
    await runLGIT(context, ["clear", target]);
    for (const k of [...seenFindingKeys]) {
      if (k.startsWith(target + "|")) seenFindingKeys.delete(k);
    }
    provider.refresh();
  } catch (e: unknown) {
    const err = e as Error;
    if (err instanceof BinaryNotFoundError) return notifyBinaryMissing(err.message);
    vscode.window.showErrorMessage(`Clear failed: ${err.message}`);
  }
}

async function startMCP(context: vscode.ExtensionContext) {
  if (mcpProcess && !mcpProcess.killed) {
    vscode.window.showInformationMessage("MCP server already running.");
    return;
  }
  const bin = resolveBinary(context);
  if (!fs.existsSync(bin)) {
    await notifyBinaryMissing(`lgit binary not found at '${bin}'. MCP server not started.`);
    return;
  }
  const proc = spawn(bin, ["mcp"], { env: envWithDB(), stdio: ["pipe", "pipe", "pipe"] });
  proc.stdout?.on("data", (d) => outputChannel.append(`[mcp.out] ${d}`));
  proc.stderr?.on("data", (d) => outputChannel.append(`[mcp.err] ${d}`));
  proc.on("error", (err) => {
    outputChannel.appendLine(`[mcp] spawn error: ${err.message}`);
    if (mcpProcess === proc) mcpProcess = undefined;
  });
  proc.on("exit", (code) => {
    outputChannel.appendLine(`[mcp] exited with code ${code}`);
    if (mcpProcess === proc) mcpProcess = undefined;
  });
  mcpProcess = proc;
  outputChannel.appendLine(`[mcp] started ${bin}`);
}

function stopMCP() {
  if (mcpProcess && !mcpProcess.killed) {
    mcpProcess.kill();
    mcpProcess = undefined;
  }
}

type TreeNode = ProjectItem | GroupItem | FindingItem | PlaceholderItem;

class FindingsTreeProvider implements vscode.TreeDataProvider<TreeNode> {
  private _onDidChange = new vscode.EventEmitter<TreeNode | undefined | void>();
  readonly onDidChangeTreeData = this._onDidChange.event;
  state: ViewState;

  constructor(private context: vscode.ExtensionContext) {
    this.state = loadViewState(context);
  }

  refresh() { this._onDidChange.fire(); }

  // mutate updates the in-memory view state, persists it, and refreshes
  // the tree. Centralises the three-step ritual every command performs.
  mutate(patch: Partial<ViewState>) {
    this.state = {
      ...this.state,
      ...patch,
      severities: { ...this.state.severities, ...(patch.severities ?? {}) },
    };
    saveViewState(this.context, this.state);
    this.refresh();
  }

  getTreeItem(el: TreeNode) { return el; }

  async getChildren(el?: TreeNode): Promise<TreeNode[]> {
    const roots = workspaceRoots();
    if (roots.length === 0) {
      return [PlaceholderItem.make("Open a folder to run gates")];
    }

    if (el instanceof FindingItem) return [];
    if (el instanceof GroupItem) {
      return el.findings.map((f) => FindingItem.forFinding(f, this.state.groupBy));
    }
    if (el instanceof ProjectItem) {
      return this.renderProject(el.project);
    }

    if (roots.length === 1) {
      return this.renderProject(roots[0]);
    }
    return roots.map((r) => new ProjectItem(r));
  }

  // renderProject is the per-project entry point: fetches with the
  // backend filter, drops anything outside the active severity set, then
  // either flattens or groups by the selected dimension.
  private async renderProject(project: string): Promise<TreeNode[]> {
    let findings: Finding[];
    try {
      findings = await this.fetchFindings(project);
    } catch (e: unknown) {
      const err = e as Error;
      outputChannel.appendLine(`tree load failed: ${err.message}`);
      if (err instanceof BinaryNotFoundError) {
        void notifyBinaryMissing(err.message);
        return [PlaceholderItem.make("lgit binary not found — see notification")];
      }
      return [PlaceholderItem.make(`Error: ${err.message}`)];
    }

    // override_accepted is audit-trail noise in the working surface — it
    // exists so silent / unjustified overrides land in the DB and dashboard.
    // Hide it from the tree unconditionally (the warning-bumped variant for
    // missing-reason overrides also gets suppressed; query the dashboard
    // or `lgit list -gate=override_accepted` to audit them).
    const filtered = findings.filter(
      (f) => f.gate_id !== "override_accepted" && severityIncluded(f.severity, this.state.severities),
    );
    refreshTreeViewDescription(filtered.length, this.state);

    if (filtered.length === 0) {
      // When the default-hidden info layer is the only thing keeping the
      // tree non-empty, say so explicitly — otherwise the user thinks the
      // project is clean while N info findings sit waiting.
      const hiddenInfoCount = !this.state.severities.info
        ? findings.filter((f) => f.severity === "info" && f.gate_id !== "override_accepted").length
        : 0;
      let empty: string;
      if (hiddenInfoCount > 0) {
        empty = `No actionable findings — ${hiddenInfoCount} info hidden (toggle severity to view)`;
      } else if (anyFilterActive(this.state)) {
        empty = `No findings match the active filters — adjust or clear them`;
      } else {
        empty = `No ${this.state.status === "open" ? "open " : this.state.status + " "}findings — clean slate ✓`;
      }
      return [PlaceholderItem.make(empty)];
    }

    // Optional blame enrichment. Best-effort: failures are logged and the
    // tree still renders, just without per-row commit annotations.
    if (vscode.workspace.getConfiguration("l0-git").get<boolean>("showBlame")) {
      try {
        await enrichWithBlame(project, filtered);
      } catch (e) {
        outputChannel.appendLine(`blame enrichment failed: ${(e as Error).message}`);
      }
    }

    if (this.state.groupBy === "none") {
      return filtered.map((f) => FindingItem.forFinding(f, "none"));
    }
    return groupFindings(filtered, this.state.groupBy);
  }

  private async fetchFindings(project: string): Promise<Finding[]> {
    const args = listArgs(project, this.state);
    const out = await runLGIT(this.context, args);
    return JSON.parse(out || "[]") as Finding[];
  }
}

// listArgs converts the persisted view state into the backend's flag
// vocabulary. Severity is intentionally NOT pushed to the backend — the
// view supports multi-select severities and we union them client-side.
function listArgs(project: string, state: ViewState, limit = 1000): string[] {
  const args = ["list", `-project=${project}`, `-limit=${limit}`, `-sort=${state.sortBy}`];
  args.push(`-status=${state.status === "all" ? "all" : state.status}`);
  if (state.query) args.push(`-query=${state.query}`);
  return args;
}

// =============================================================================
// blame enrichment (opt-in via l0-git.showBlame)
// =============================================================================

// findingFileLine extracts (relative file path, line number) from a
// finding's FilePath field. Scan-style gates encode `<file>:<line>:<rule>`;
// presence gates carry just `<file>`; history gates carry `history:<sha>:…`
// which is not a working-tree file. Returns null for un-blamable findings.
function findingFileLine(f: Finding): { file: string; line: number } | null {
  const fp = f.file_path;
  if (!fp) return null;
  if (fp.startsWith("history:")) return null;
  const parts = fp.split(":");
  if (parts.length === 1) return { file: parts[0], line: 1 };
  const n = parseInt(parts[1], 10);
  if (!Number.isFinite(n) || n <= 0) return { file: parts[0], line: 1 };
  return { file: parts[0], line: n };
}

// enrichWithBlame mutates the findings array in-place, attaching a
// BlameInfo for each row whose file_path resolves to a real file. One git
// blame per unique file, fired in parallel — the cost is bound by the
// slowest blame, not the sum.
async function enrichWithBlame(project: string, findings: Finding[]): Promise<void> {
  // Group findings by file so each blame call covers many findings.
  const byFile = new Map<string, Array<{ finding: Finding; line: number }>>();
  for (const f of findings) {
    const fl = findingFileLine(f);
    if (!fl) continue;
    const arr = byFile.get(fl.file) ?? [];
    arr.push({ finding: f, line: fl.line });
    byFile.set(fl.file, arr);
  }
  if (byFile.size === 0) return;

  await Promise.all(
    Array.from(byFile.entries()).map(async ([file, entries]) => {
      const map = await runBlame(project, file).catch(() => new Map<number, BlameInfo>());
      for (const e of entries) {
        const info = map.get(e.line);
        if (info) e.finding.blame = info;
      }
    }),
  );
}

function runBlame(project: string, file: string): Promise<Map<number, BlameInfo>> {
  return new Promise((resolve, reject) => {
    execFile(
      "git",
      ["-C", project, "blame", "--line-porcelain", "--", file],
      { maxBuffer: 32 * 1024 * 1024 },
      (err, stdout) => {
        if (err) {
          reject(err);
          return;
        }
        resolve(parseBlame(stdout));
      },
    );
  });
}

// parseBlame consumes `git blame --line-porcelain` output. Each chunk
// starts with "<sha> <orig> <final> [group]" then key:value lines, then
// a single tab-prefixed content line. We only keep hash/author/time and
// key the result by the line number IN THE FINAL FILE.
function parseBlame(out: string): Map<number, BlameInfo> {
  const result = new Map<number, BlameInfo>();
  const lines = out.split("\n");
  let i = 0;
  while (i < lines.length) {
    const m = /^([0-9a-f]{40}) (\d+) (\d+)/.exec(lines[i]);
    if (!m) { i++; continue; }
    const hash = m[1];
    const finalLine = parseInt(m[3], 10);
    let author = "?";
    let authorTime = 0;
    i++;
    while (i < lines.length && !lines[i].startsWith("\t")) {
      const ln = lines[i];
      if (ln.startsWith("author ")) author = ln.slice(7);
      else if (ln.startsWith("author-time ")) authorTime = parseInt(ln.slice(12), 10);
      i++;
    }
    if (i < lines.length) i++; // skip the tab-content line
    if (Number.isFinite(finalLine) && finalLine > 0) {
      result.set(finalLine, { hash, author, authorTime });
    }
  }
  return result;
}

function blameSummary(b: BlameInfo): string {
  if (!b.hash || /^0+$/.test(b.hash)) {
    return "uncommitted";
  }
  const short = b.hash.slice(0, 7);
  const when = b.authorTime > 0 ? relativeTime(b.authorTime * 1000) : "";
  const author = b.author || "?";
  return when ? `${short} · ${author} · ${when}` : `${short} · ${author}`;
}

// relativeTime formats a millis timestamp as "3d ago" / "5h ago" /
// "2 months ago". Plain English, no dep — matches what GitLens shows.
function relativeTime(ms: number): string {
  const delta = Date.now() - ms;
  const sec = Math.round(delta / 1000);
  if (sec < 60) return `${sec}s ago`;
  const min = Math.round(sec / 60);
  if (min < 60) return `${min}m ago`;
  const hr = Math.round(min / 60);
  if (hr < 24) return `${hr}h ago`;
  const day = Math.round(hr / 24);
  if (day < 30) return `${day}d ago`;
  const mo = Math.round(day / 30);
  if (mo < 12) return `${mo}mo ago`;
  const yr = Math.round(mo / 12);
  return `${yr}y ago`;
}

function severityIncluded(sev: string, sevs: ViewState["severities"]): boolean {
  switch (sev) {
    case "error":   return sevs.error;
    case "warning": return sevs.warning;
    case "info":    return sevs.info;
    default:        return true; // unknown severity: surface rather than hide
  }
}

function anyFilterActive(state: ViewState): boolean {
  if (state.status !== "open") return true;
  if (state.query) return true;
  if (!state.severities.error || !state.severities.warning || !state.severities.info) return true;
  return false;
}

// =============================================================================
// grouping
// =============================================================================

// groupFindings turns a flat finding list into a list of GroupItem nodes
// keyed by the active grouping dimension. Group ordering is stable per
// dimension (severity by gravity, others alphabetical). Tag groups
// "explode": a finding with `security,git-hygiene` lives under both.
function groupFindings(findings: Finding[], by: GroupBy): TreeNode[] {
  const buckets = new Map<string, Finding[]>();
  for (const f of findings) {
    for (const key of groupKeysFor(f, by)) {
      const arr = buckets.get(key) ?? [];
      arr.push(f);
      buckets.set(key, arr);
    }
  }
  const keys = Array.from(buckets.keys()).sort(groupKeyComparator(by));
  return keys.map((k) => new GroupItem(k, buckets.get(k)!, by));
}

function groupKeysFor(f: Finding, by: GroupBy): string[] {
  switch (by) {
    case "severity": return [f.severity || "info"];
    case "gate":     return [f.gate_id];
    case "file":     return [filePathStem(f.file_path) || "(project-level)"];
    case "tag": {
      const tags = (f.tags || "").split(",").map((s) => s.trim()).filter(Boolean);
      return tags.length === 0 ? ["(no tags)"] : tags;
    }
    case "status":   return [f.status || "open"];
    default:         return [""];
  }
}

// filePathStem strips the `:line:rule_id` suffix the scan-style gates
// add to FilePath, so all findings inside the same file group together.
function filePathStem(filePath: string): string {
  if (!filePath) return "";
  const colon = filePath.indexOf(":");
  return colon >= 0 ? filePath.slice(0, colon) : filePath;
}

const severityRank: Record<string, number> = { error: 0, warning: 1, info: 2 };

function groupKeyComparator(by: GroupBy): (a: string, b: string) => number {
  if (by === "severity") {
    return (a, b) => (severityRank[a] ?? 99) - (severityRank[b] ?? 99);
  }
  return (a, b) => a.localeCompare(b);
}

// =============================================================================
// tree view title description
// =============================================================================

function refreshTreeViewDescription(count: number, state: ViewState): void {
  if (!treeView) return;
  const parts: string[] = [`${count} finding${count === 1 ? "" : "s"}`];
  if (state.groupBy !== "none") parts.push(`group: ${state.groupBy}`);
  if (state.sortBy !== "updated") parts.push(`sort: ${state.sortBy}`);
  if (state.status !== "open") parts.push(`status: ${state.status}`);
  const sevs = (Object.keys(state.severities) as Array<keyof ViewState["severities"]>).filter((k) => state.severities[k]);
  if (sevs.length < 3) parts.push(`severity: ${sevs.join("+") || "none"}`);
  if (state.query) parts.push(`query: "${state.query}"`);
  treeView.description = parts.join(" · ");
}

class ProjectItem extends vscode.TreeItem {
  constructor(public readonly project: string) {
    super(path.basename(project), vscode.TreeItemCollapsibleState.Expanded);
    this.tooltip = project;
    this.iconPath = new vscode.ThemeIcon("folder");
    this.contextValue = "project";
  }
}

class GroupItem extends vscode.TreeItem {
  constructor(
    public readonly key: string,
    public readonly findings: Finding[],
    public readonly groupBy: GroupBy,
  ) {
    super(formatGroupLabel(key, groupBy), vscode.TreeItemCollapsibleState.Expanded);
    this.description = `${findings.length}`;
    this.tooltip = `${findings.length} finding${findings.length === 1 ? "" : "s"} grouped by ${groupBy}`;
    this.iconPath = groupIcon(groupBy, key);
    this.contextValue = "group";
  }
}

function formatGroupLabel(key: string, by: GroupBy): string {
  if (by === "severity") {
    switch (key) {
      case "error":   return "Errors";
      case "warning": return "Warnings";
      case "info":    return "Info";
    }
  }
  return key;
}

function groupIcon(by: GroupBy, key: string): vscode.ThemeIcon {
  switch (by) {
    case "severity": return severityIcon(key);
    case "gate":     return new vscode.ThemeIcon("shield");
    case "file":     return new vscode.ThemeIcon("file");
    case "tag":      return new vscode.ThemeIcon("tag");
    case "status":   return new vscode.ThemeIcon("circle-large-outline");
    default:         return new vscode.ThemeIcon("symbol-namespace");
  }
}

class FindingItem extends vscode.TreeItem {
  readonly finding?: Finding;

  private constructor(label: string, finding?: Finding) {
    super(label, vscode.TreeItemCollapsibleState.None);
    this.finding = finding;
  }

  // forFinding tailors the row's secondary text (description) to the
  // active grouping: when grouped by file, the file is implied by the
  // parent so we only show the gate; when grouped by gate, the file
  // becomes the description; etc.
  static forFinding(f: Finding, groupedBy: GroupBy): FindingItem {
    const item = new FindingItem(f.title, f);
    let desc = findingDescription(f, groupedBy);
    if (f.blame) {
      desc = `${desc} · ${blameSummary(f.blame)}`;
    }
    item.description = desc;
    const ts = new Date(f.updated_at).toLocaleString();
    const blameLine = f.blame ? `_blame:_ ${blameSummary(f.blame)}  \n` : "";
    item.tooltip = new vscode.MarkdownString(
      `**${f.title}** _(${f.severity})_\n\n${f.message}\n\n` +
      `_gate:_ ${f.gate_id}  \n_project:_ ${f.project}  \n` +
      `_file:_ ${f.file_path || "—"}  \n` +
      `_tags:_ ${f.tags || "—"}  \n` +
      blameLine +
      `_updated:_ ${ts}`,
    );
    item.contextValue = "finding";
    item.iconPath = severityIcon(f.severity);
    item.command = {
      command: "l0-git.openFinding",
      title: "Open finding",
      arguments: [item],
    };
    return item;
  }
}

function findingDescription(f: Finding, by: GroupBy): string {
  switch (by) {
    case "severity":
    case "status":
    case "tag":
      return f.file_path ? `${f.gate_id} · ${f.file_path}` : f.gate_id;
    case "gate":
      return f.file_path || "(project-level)";
    case "file":
      return f.gate_id;
    default:
      return f.file_path ? `${f.gate_id} · ${f.file_path}` : f.gate_id;
  }
}

class PlaceholderItem extends vscode.TreeItem {
  static make(label: string): PlaceholderItem {
    const item = new PlaceholderItem(label, vscode.TreeItemCollapsibleState.None);
    item.iconPath = new vscode.ThemeIcon("info");
    return item;
  }
}

function severityIcon(sev: string): vscode.ThemeIcon {
  switch (sev) {
    case "error":   return new vscode.ThemeIcon("error", new vscode.ThemeColor("errorForeground"));
    case "warning": return new vscode.ThemeIcon("warning", new vscode.ThemeColor("editorWarning.foreground"));
    default:        return new vscode.ThemeIcon("info");
  }
}

// =============================================================================
// view-state prompt commands
// =============================================================================

async function promptGroupBy(): Promise<void> {
  const options: Array<{ label: string; value: GroupBy; description: string }> = [
    { label: "Severity", value: "severity", description: "errors / warnings / info" },
    { label: "Gate",     value: "gate",     description: "one node per gate ID" },
    { label: "File",     value: "file",     description: "one node per source file" },
    { label: "Tag",      value: "tag",      description: "explode by tag — security / git-hygiene / …" },
    { label: "Status",   value: "status",   description: "open / ignored / resolved" },
    { label: "None",     value: "none",     description: "flat list" },
  ];
  const current = provider.state.groupBy;
  const picked = await vscode.window.showQuickPick(
    options.map((o) => ({ label: o.label, description: o.description, picked: o.value === current, value: o.value })),
    { placeHolder: `Group by — currently: ${current}`, matchOnDescription: true },
  );
  if (!picked) return;
  provider.mutate({ groupBy: picked.value });
}

async function promptSortBy(): Promise<void> {
  const options: Array<{ label: string; value: SortBy; description: string }> = [
    { label: "Updated (newest first)", value: "updated",  description: "default — show what just changed" },
    { label: "Created (newest first)", value: "created",  description: "by first-seen time" },
    { label: "Severity (worst first)", value: "severity", description: "error → warning → info" },
    { label: "Gate (alphabetical)",    value: "gate",     description: "groups same-gate findings together" },
    { label: "File (alphabetical)",    value: "file",     description: "walk the tree by source file" },
  ];
  const current = provider.state.sortBy;
  const picked = await vscode.window.showQuickPick(
    options.map((o) => ({ label: o.label, description: o.description, picked: o.value === current, value: o.value })),
    { placeHolder: `Sort by — currently: ${current}` },
  );
  if (!picked) return;
  provider.mutate({ sortBy: picked.value });
}

async function promptStatusFilter(): Promise<void> {
  const options: Array<{ label: string; value: StatusFilter }> = [
    { label: "Open",     value: "open" },
    { label: "Ignored",  value: "ignored" },
    { label: "Resolved", value: "resolved" },
    { label: "All",      value: "all" },
  ];
  const current = provider.state.status;
  const picked = await vscode.window.showQuickPick(
    options.map((o) => ({ label: o.label, picked: o.value === current, value: o.value })),
    { placeHolder: `Status — currently: ${current}` },
  );
  if (!picked) return;
  provider.mutate({ status: picked.value });
}

async function promptSeverityFilter(): Promise<void> {
  const current = provider.state.severities;
  const picked = await vscode.window.showQuickPick(
    [
      { label: "$(error) Errors",   value: "error",   picked: current.error },
      { label: "$(warning) Warnings", value: "warning", picked: current.warning },
      { label: "$(info) Info",      value: "info",    picked: current.info },
    ],
    {
      placeHolder: "Toggle which severities to show",
      canPickMany: true,
    },
  );
  if (!picked) return;
  const set = new Set(picked.map((p) => p.value));
  provider.mutate({
    severities: {
      error:   set.has("error"),
      warning: set.has("warning"),
      info:    set.has("info"),
    },
  });
}

async function promptSearch(): Promise<void> {
  const current = provider.state.query;
  const value = await vscode.window.showInputBox({
    prompt: "Search findings (substring across title, message, file path, gate ID). Empty clears the filter.",
    value: current,
  });
  if (value === undefined) return;
  provider.mutate({ query: value.trim() });
}

function resetFilters(): void {
  provider.mutate({
    groupBy:    DEFAULT_VIEW_STATE.groupBy,
    sortBy:     DEFAULT_VIEW_STATE.sortBy,
    status:     DEFAULT_VIEW_STATE.status,
    severities: { ...DEFAULT_VIEW_STATE.severities },
    query:      "",
  });
  vscode.window.showInformationMessage("l0-git: view filters reset to defaults.");
}

// openOverview wires the extension's runtime state into the self-contained
// overview webview module. When multi-folder, prompt for which project to
// analyse — the dashboard shows one project at a time.
async function openOverview(context: vscode.ExtensionContext): Promise<void> {
  const roots = workspaceRoots();
  if (roots.length === 0) {
    vscode.window.showInformationMessage("l0-git: open a folder first.");
    return;
  }
  const target = roots.length === 1
    ? roots[0]
    : await vscode.window.showQuickPick(roots, { placeHolder: "Project for the Overview" });
  if (!target) return;
  await showOverview(context, target, {
    runLGIT: (args: string[]) => runLGIT(context, args),
    log: (m: string) => outputChannel.appendLine(m),
    runChecks: () => runChecksAndRefresh(context),
    setSearchQuery: (q: string) => provider.mutate({ query: q }),
  });
}

// L0GitCodeActions advertises a "Generate stub" quick fix for any l0-git
// diagnostic whose gate is in fixableGates. We attach the diagnostic itself
// so VSCode lights up the bulb only when it's actionable.
class L0GitCodeActions implements vscode.CodeActionProvider {
  provideCodeActions(
    _document: vscode.TextDocument,
    _range: vscode.Range | vscode.Selection,
    ctx: vscode.CodeActionContext,
  ): vscode.CodeAction[] {
    const actions: vscode.CodeAction[] = [];
    for (const d of ctx.diagnostics) {
      if (d.source !== "l0-git" || typeof d.code !== "string") continue;
      if (!fixableGates.has(d.code)) continue;
      const project = projectFromDiagnosticUri(d);
      if (!project) continue;
      const action = new vscode.CodeAction(`l0-git: Generate stub for ${d.code}`, vscode.CodeActionKind.QuickFix);
      action.diagnostics = [d];
      action.command = {
        command: "l0-git.applyFix",
        title: "Generate stub",
        arguments: [project, d.code],
      };
      actions.push(action);
    }
    return actions;
  }
}

// projectFromDiagnosticUri figures out which workspace folder a diagnostic
// belongs to. Project-level findings target the folder URI directly; file
// findings target a child path — in both cases the workspace folder
// containing the URI is the right answer.
function projectFromDiagnosticUri(d: vscode.Diagnostic): string | undefined {
  // VSCode invokes provideCodeActions with a document; the diagnostic's
  // own URI isn't directly available there but the uri came in via the
  // collection. We use workspace folders as the source of truth instead.
  const folders = vscode.workspace.workspaceFolders ?? [];
  if (folders.length === 1) return folders[0].uri.fsPath;
  // For multi-root, we can't disambiguate from a Diagnostic alone — fall
  // back to the first folder; the user can re-run in their target workspace.
  // This branch is rare; the fix command itself prompts for confirmation.
  void d;
  return folders[0]?.uri.fsPath;
}

// applyFix is the back-end of the CodeAction. For LICENSE we prompt for an
// SPDX choice; for everything else we write the canned stub. After writing
// we re-run gates so the diagnostic clears immediately.
async function applyFix(context: vscode.ExtensionContext, project: string, gateId: string): Promise<void> {
  let stub: Stub | null = null;
  if (gateId === "license_present") {
    const pick = await vscode.window.showQuickPick(licenseChoices, {
      placeHolder: "Pick a license",
      matchOnDescription: true,
    });
    if (!pick) return;
    const holder = await vscode.window.showInputBox({
      prompt: "Copyright holder name",
      value: process.env.USER || process.env.USERNAME || "",
    });
    if (holder === undefined) return;
    stub = licenseStub(pick.spdx, holder.trim() || (process.env.USER ?? "Anonymous"));
  } else {
    stub = stubFor(gateId, project);
  }
  if (!stub) {
    vscode.window.showWarningMessage(`l0-git: no stub generator for gate '${gateId}'.`);
    return;
  }

  const target = path.join(project, stub.relPath);
  if (fs.existsSync(target)) {
    const overwrite = await vscode.window.showWarningMessage(
      `${stub.relPath} already exists. Overwrite?`,
      { modal: true },
      "Overwrite",
    );
    if (overwrite !== "Overwrite") return;
  }
  try {
    await fs.promises.mkdir(path.dirname(target), { recursive: true });
    await fs.promises.writeFile(target, stub.content, { encoding: "utf8" });
  } catch (e: unknown) {
    vscode.window.showErrorMessage(`l0-git: failed to write ${stub.relPath}: ${(e as Error).message}`);
    return;
  }

  const doc = await vscode.workspace.openTextDocument(target);
  await vscode.window.showTextDocument(doc, { preview: false });
  // Watcher will fire too, but trigger an explicit recheck so the user
  // sees the diagnostic clear within a single tick.
  await runChecksAndRefresh(context);
  vscode.window.showInformationMessage(`l0-git: created ${stub.relPath}`);
}

// =============================================================================
// remediation surface (`lgit fix <id>` integrations)
// =============================================================================

interface RemediationPayload {
  finding: Finding;
  remediation: {
    summary: string;
    confidence: "deterministic" | "guided";
    recipe?: {
      commands?: Array<{ run: string; note?: string }>;
      file_edits?: Array<{ path: string; op: string; content: string; line?: number }>;
      caveats?: string[];
    };
    claude_prompt: string;
  };
}

// showRemediation runs `lgit fix <id>` and opens the human-readable output
// in a plaintext doc — same surface as openFinding, but with the recipe
// instead of just the message. Plain text (not markdown) so the literal
// `--- prompt ---` block survives intact for copy-paste into Claude Code.
async function showRemediation(context: vscode.ExtensionContext, item: FindingItem): Promise<void> {
  if (!item || !item.finding) return;
  const id = item.finding.id;
  let body: string;
  try {
    body = await runLGIT(context, ["fix", String(id)]);
  } catch (e: unknown) {
    const err = e as Error;
    if (err instanceof BinaryNotFoundError) return notifyBinaryMissing(err.message);
    vscode.window.showErrorMessage(`l0-git: lgit fix ${id} failed: ${err.message}`);
    return;
  }
  const doc = await vscode.workspace.openTextDocument({ content: body, language: "plaintext" });
  await vscode.window.showTextDocument(doc, { preview: false });
}

// copyClaudePrompt grabs the structured remediation, copies the
// claude_prompt to the system clipboard, and shows a one-line toast.
// No subprocess, no auto-execution — the user pastes into Claude Code (or
// any agent) themselves. This is the safest possible HITL channel.
async function copyClaudePrompt(context: vscode.ExtensionContext, item: FindingItem): Promise<void> {
  if (!item || !item.finding) return;
  const id = item.finding.id;
  let raw: string;
  try {
    raw = await runLGIT(context, ["fix", String(id), "--json"]);
  } catch (e: unknown) {
    const err = e as Error;
    if (err instanceof BinaryNotFoundError) return notifyBinaryMissing(err.message);
    vscode.window.showErrorMessage(`l0-git: lgit fix ${id} --json failed: ${err.message}`);
    return;
  }
  let parsed: RemediationPayload;
  try {
    parsed = JSON.parse(raw) as RemediationPayload;
  } catch (e: unknown) {
    vscode.window.showErrorMessage(`l0-git: could not parse remediation JSON: ${(e as Error).message}`);
    return;
  }
  const prompt = parsed.remediation?.claude_prompt;
  if (!prompt) {
    vscode.window.showWarningMessage(`l0-git: finding #${id} has no claude_prompt.`);
    return;
  }
  await vscode.env.clipboard.writeText(prompt);
  const conf = parsed.remediation.confidence === "deterministic" ? "deterministic recipe" : "guided remediation";
  vscode.window.showInformationMessage(
    `l0-git: copied Claude Code prompt for finding #${id} (${conf}) — paste it into your Claude Code session.`,
  );
}
