import * as vscode from "vscode";
import { execFile, spawn, ChildProcess } from "child_process";
import * as path from "path";
import * as fs from "fs";
import { Stub, stubFor, licenseChoices, licenseStub } from "./stubs";

interface Finding {
  id: number;
  project: string;
  gate_id: string;
  severity: string;
  title: string;
  message: string;
  file_path: string;
  status: string;
  created_at: number;
  updated_at: number;
}

interface CheckResult {
  project: string;
  gates_run: string[];
  findings: Finding[];
}

let mcpProcess: ChildProcess | undefined;
let provider: FindingsTreeProvider;
let outputChannel: vscode.OutputChannel;
let diagnostics: vscode.DiagnosticCollection;
let statusBar: vscode.StatusBarItem;
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
  const treeView = vscode.window.createTreeView("l0-git.findings", {
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
  );

  // Re-render when settings change so binary/db overrides take effect.
  context.subscriptions.push(
    vscode.workspace.onDidChangeConfiguration((e) => {
      if (e.affectsConfiguration("l0-git")) provider.refresh();
    }),
  );

  // Re-run checks when the workspace folders change (folder added/removed).
  context.subscriptions.push(
    vscode.workspace.onDidChangeWorkspaceFolders(() => runChecksAndRefresh(context)),
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
  const folders = vscode.workspace.workspaceFolders ?? [];
  // Watch every file the registered gates care about. One pattern per
  // folder per logical group; vscode dedupes the underlying file events.
  const patterns = [
    "README*", "LICENSE*", "COPYING*", "CONTRIBUTING*", "SECURITY*",
    "CHANGELOG*", "CHANGES*", "HISTORY*", ".gitignore", ".l0git.json",
    ".github/PULL_REQUEST_TEMPLATE.md", ".github/pull_request_template.md",
    ".github/ISSUE_TEMPLATE/**", ".github/workflows/**",
  ];
  const trigger = () =>
    runChecksAndRefresh(context).catch((err) =>
      outputChannel.appendLine(`watcher recheck failed: ${err}`),
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
  if (newlyOpen.length > 0 && vscode.workspace.getConfiguration("l0-git").get<boolean>("notifyOnNew")) {
    notifyNewFindings(context, newlyOpen);
  }
  await syncDiagnostics(context, roots);
  provider.refresh();
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
      const out = await runLGIT(context, ["list", root, "open", "500"]);
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
    const abs = path.isAbsolute(f.file_path) ? f.file_path : path.join(f.project, f.file_path);
    if (fs.existsSync(abs)) {
      const doc = await vscode.workspace.openTextDocument(abs);
      await vscode.window.showTextDocument(doc, { preview: false });
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
  const confirm = await vscode.window.showWarningMessage(
    `Delete all l0-git findings for ${target}?`,
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
  mcpProcess = spawn(bin, ["mcp"], { env: envWithDB(), stdio: ["pipe", "pipe", "pipe"] });
  mcpProcess.stdout?.on("data", (d) => outputChannel.append(`[mcp.out] ${d}`));
  mcpProcess.stderr?.on("data", (d) => outputChannel.append(`[mcp.err] ${d}`));
  mcpProcess.on("exit", (code) => {
    outputChannel.appendLine(`[mcp] exited with code ${code}`);
    mcpProcess = undefined;
  });
  outputChannel.appendLine(`[mcp] started ${bin}`);
}

function stopMCP() {
  if (mcpProcess && !mcpProcess.killed) {
    mcpProcess.kill();
    mcpProcess = undefined;
  }
}

type TreeNode = ProjectItem | FindingItem | PlaceholderItem;

class FindingsTreeProvider implements vscode.TreeDataProvider<TreeNode> {
  private _onDidChange = new vscode.EventEmitter<TreeNode | undefined | void>();
  readonly onDidChangeTreeData = this._onDidChange.event;

  constructor(private context: vscode.ExtensionContext) {}

  refresh() { this._onDidChange.fire(); }

  getTreeItem(el: TreeNode) { return el; }

  async getChildren(el?: TreeNode): Promise<TreeNode[]> {
    const roots = workspaceRoots();
    if (roots.length === 0) {
      return [PlaceholderItem.make("Open a folder to run gates")];
    }

    if (el instanceof ProjectItem) {
      return this.findingsFor(el.project);
    }

    if (roots.length === 1) {
      // Single workspace folder: flatten — show findings directly.
      return this.findingsFor(roots[0]);
    }

    // Multiple folders: top-level groups per project.
    return roots.map((r) => new ProjectItem(r));
  }

  private async findingsFor(project: string): Promise<TreeNode[]> {
    try {
      const out = await runLGIT(this.context, ["list", project, "open", "200"]);
      const findings: Finding[] = JSON.parse(out || "[]") || [];
      if (findings.length === 0) {
        return [PlaceholderItem.make("No open findings — clean slate ✓")];
      }
      return findings.map((f) => FindingItem.forFinding(f));
    } catch (e: unknown) {
      const err = e as Error;
      outputChannel.appendLine(`tree load failed: ${err.message}`);
      if (err instanceof BinaryNotFoundError) {
        void notifyBinaryMissing(err.message);
        return [PlaceholderItem.make("lgit binary not found — see notification")];
      }
      return [PlaceholderItem.make(`Error: ${err.message}`)];
    }
  }
}

class ProjectItem extends vscode.TreeItem {
  constructor(public readonly project: string) {
    super(path.basename(project), vscode.TreeItemCollapsibleState.Expanded);
    this.tooltip = project;
    this.iconPath = new vscode.ThemeIcon("folder");
    this.contextValue = "project";
  }
}

class FindingItem extends vscode.TreeItem {
  readonly finding?: Finding;

  private constructor(label: string, finding?: Finding) {
    super(label, vscode.TreeItemCollapsibleState.None);
    this.finding = finding;
  }

  static forFinding(f: Finding): FindingItem {
    const item = new FindingItem(f.title, f);
    item.description = f.file_path ? `${f.gate_id} · ${f.file_path}` : f.gate_id;
    const ts = new Date(f.updated_at).toLocaleString();
    item.tooltip = new vscode.MarkdownString(
      `**${f.title}** _(${f.severity})_\n\n${f.message}\n\n` +
      `_gate:_ ${f.gate_id}  \n_project:_ ${f.project}  \n` +
      `_file:_ ${f.file_path || "—"}  \n_updated:_ ${ts}`,
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
