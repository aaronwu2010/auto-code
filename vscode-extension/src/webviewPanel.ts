import * as crypto from 'crypto';
import * as vscode from 'vscode';
import { ServerClient } from './serverClient';
import { WorkspaceManager } from './workspace';

function getNonce(): string {
  return crypto.randomBytes(16).toString('base64');
}

function buildHtml(
  webview: vscode.Webview,
  extensionUri: vscode.Uri
): string {
  const distUri = vscode.Uri.joinPath(extensionUri, 'webview-ui', 'dist');
  const scriptUri = webview.asWebviewUri(
    vscode.Uri.joinPath(distUri, 'assets', 'index.js')
  );
  const styleUri = webview.asWebviewUri(
    vscode.Uri.joinPath(distUri, 'assets', 'index.css')
  );
  const nonce = getNonce();

  return /* html */ `<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <meta http-equiv="Content-Security-Policy"
        content="default-src 'none'; style-src ${webview.cspSource} 'unsafe-inline'; script-src 'nonce-${nonce}'; font-src ${webview.cspSource};" />
  <title>Auto Code</title>
  <link rel="stylesheet" href="${styleUri}" />
</head>
<body>
  <div id="root"></div>
  <script nonce="${nonce}" src="${scriptUri}"></script>
</body>
</html>`;
}

/**
 * WebviewHost：把 Webview<->server 桥接逻辑抽离为共享类，
 * 同时适用于 WebviewPanel（独立面板）和 WebviewView（侧边栏视图）。
 */
export class AutoCodeWebviewHost implements vscode.Disposable {
  private disposables: vscode.Disposable[] = [];

  constructor(
    private readonly context: vscode.ExtensionContext,
    private readonly webview: vscode.Webview,
    private readonly server: ServerClient,
    private readonly workspace: WorkspaceManager,
    private readonly onPostingMessage: (msg: unknown) => Thenable<boolean>,
    initialWorkspaceDir: string
  ) {
    this.setupListeners();
    this.postMessage({ type: 'workspace', dir: initialWorkspaceDir });
  }

  /** 构造 HTML */
  getHtml(): string {
    return buildHtml(this.webview, this.context.extensionUri);
  }

  /** 处理来自 Webview 的消息 */
  async handleMessage(msg: unknown): Promise<void> {
    if (typeof msg !== 'object' || msg === null) {
      return;
    }
    const m = msg as { type?: string; id?: string; method?: string; params?: unknown };

    if (m.type === 'request' && m.method) {
      try {
        const result = await this.server.request(m.method, m.params);
        this.postMessage({ type: 'response', id: m.id, result });
      } catch (err) {
        this.postMessage({
          type: 'response',
          id: m.id,
          error: (err as Error).message,
        });
      }
      return;
    }

    if (m.type === 'getWorkspace') {
      this.postMessage({
        type: 'workspace',
        dir: this.workspace.getCurrentWorkspace() ?? '',
      });
      return;
    }
  }

  dispose(): void {
    for (const d of this.disposables) {
      d.dispose();
    }
    this.disposables.length = 0;
  }

  private postMessage(message: unknown): void {
    void this.onPostingMessage(message);
  }

  private setupListeners(): void {
    this.disposables.push(
      this.webview.onDidReceiveMessage((msg) => void this.handleMessage(msg)),
      this.server.onQueryMessage((data) => {
        this.postMessage({ type: 'event', event: 'query:message', data });
      }),
      this.server.onStateChange((data) => {
        this.postMessage({ type: 'event', event: 'state:change', data });
      }),
      this.workspace.onDidChangeWorkspaceFolders(() => {
        this.postMessage({
          type: 'workspace',
          dir: this.workspace.getCurrentWorkspace() ?? '',
        });
      })
    );
  }
}

/**
 * 独立 Webview 面板（命令面板 `Auto Code: Open Chat`、Explorer 标题栏图标）。
 */
export class AutoCodeWebviewPanel implements vscode.Disposable {
  public static readonly viewType = 'autoCodeChat';
  private panel: vscode.WebviewPanel | undefined;
  private host: AutoCodeWebviewHost | undefined;

  constructor(
    private readonly context: vscode.ExtensionContext,
    private readonly server: ServerClient,
    private readonly workspace: WorkspaceManager
  ) {}

  /** 显示面板。默认在编辑器最右侧列（ViewColumn.Beside）打开，大方框效果。 */
  async show(targetColumn: vscode.ViewColumn = vscode.ViewColumn.Beside): Promise<void> {
    if (this.panel) {
      // 若面板已经存在但列不同，先切列再 reveal
      const current = (this.panel as WebviewPanelWithColumn).viewColumn;
      if (current && current !== targetColumn) {
        this.host?.dispose();
        this.panel.dispose();
        this.panel = undefined;
        this.host = undefined;
      } else {
        this.panel.reveal(targetColumn);
        return;
      }
    }

    try {
      await this.server.start();
    } catch (err) {
      vscode.window.showErrorMessage(
        `启动 Auto Code server 失败: ${(err as Error).message}\n请检查 auto-code.serverPath 配置`
      );
      return;
    }

    const workspaceDir = this.workspace.getCurrentWorkspace() ?? '';

    this.panel = vscode.window.createWebviewPanel(
      AutoCodeWebviewPanel.viewType,
      'Auto Code',
      { viewColumn: targetColumn, preserveFocus: false },
      {
        enableScripts: true,
        retainContextWhenHidden: true,
        localResourceRoots: [
          vscode.Uri.joinPath(this.context.extensionUri, 'webview-ui', 'dist'),
        ],
      }
    );

    this.host = new AutoCodeWebviewHost(
      this.context,
      this.panel.webview,
      this.server,
      this.workspace,
      (m) => this.panel!.webview.postMessage(m),
      workspaceDir
    );
    this.panel.webview.html = this.host.getHtml();

    this.panel.onDidDispose(() => {
      this.host?.dispose();
      this.host = undefined;
      this.panel = undefined;
    });
  }

  dispose(): void {
    this.host?.dispose();
    this.host = undefined;
    this.panel?.dispose();
    this.panel = undefined;
  }
}

type WebviewPanelWithColumn = vscode.WebviewPanel & { viewColumn?: vscode.ViewColumn };

/**
 * 侧边栏/面板视图（WebviewViewProvider）——通过点击活动栏或面板图标打开。
 * 同一个 Provider 类支持多个 viewId：右侧 Secondary Side Bar（`auto-code.chat`）和底部 Panel（`auto-code.panelChat`）。
 */
export class AutoCodeSidebarProvider implements vscode.WebviewViewProvider {
  public static readonly sideBarViewId = 'auto-code.chat';          // Secondary Side Bar (右侧)
  public static readonly activityViewId = 'auto-code.activityChat'; // Activity Bar (左侧)
  public static readonly panelViewId = 'auto-code.panelChat';       // Panel (底部/拖到右侧)
  private static readonly secondarySidebarContainerId = 'auto-code';

  private readonly hosts = new Map<string, AutoCodeWebviewHost>();
  private readonly views = new Map<string, vscode.WebviewView>();

  constructor(
    private readonly context: vscode.ExtensionContext,
    private readonly server: ServerClient,
    private readonly workspace: WorkspaceManager
  ) {}

  /** 以编程方式打开 Chat 界面：优先展开右侧 Secondary Side Bar → Auto Code（代码旁边的位置） */
  async reveal(): Promise<void> {
    try {
      await this.server.start();
    } catch (err) {
      vscode.window.showErrorMessage(
        `启动 Auto Code server 失败: ${(err as Error).message}\n请检查 auto-code.serverPath 配置`
      );
    }
    // workbench.view.extension.<containerId> 会自动展开对应容器（右侧 Secondary Side Bar 会被自动打开）
    try {
      await vscode.commands.executeCommand(
        `workbench.view.extension.${AutoCodeSidebarProvider.secondarySidebarContainerId}`
      );
      return;
    } catch {
      // ignore, fallthrough
    }
    try {
      // 兜底：直接尝试聚焦具体 viewId
      await vscode.commands.executeCommand(
        `${AutoCodeSidebarProvider.sideBarViewId}.focus`
      );
      return;
    } catch {
      // ignore, fallthrough
    }
    try {
      // 再兜底：打开 panel 容器
      await vscode.commands.executeCommand('workbench.view.extension.auto-code-panel');
    } catch {
      // ignore
    }
  }

  resolveWebviewView(
    webviewView: vscode.WebviewView,
    context: vscode.WebviewViewResolveContext<unknown>,
    _token: vscode.CancellationToken
  ): void | Thenable<void> {
    const viewId = context.viewId ?? webviewView.viewType;
    this.views.set(viewId, webviewView);

    webviewView.webview.options = {
      enableScripts: true,
      localResourceRoots: [
        vscode.Uri.joinPath(this.context.extensionUri, 'webview-ui', 'dist'),
      ],
    };

    void (async () => {
      try {
        await this.server.start();
      } catch (err) {
        vscode.window.showErrorMessage(
          `启动 Auto Code server 失败: ${(err as Error).message}`
        );
      }
      const workspaceDir = this.workspace.getCurrentWorkspace() ?? '';

      this.hosts.get(viewId)?.dispose();
      const host = new AutoCodeWebviewHost(
        this.context,
        webviewView.webview,
        this.server,
        this.workspace,
        (m) => webviewView.webview.postMessage(m),
        workspaceDir
      );
      this.hosts.set(viewId, host);
      webviewView.webview.html = host.getHtml();
    })();

    webviewView.onDidDispose(() => {
      this.hosts.get(viewId)?.dispose();
      this.hosts.delete(viewId);
      this.views.delete(viewId);
    });
  }

  dispose(): void {
    for (const host of this.hosts.values()) host.dispose();
    this.hosts.clear();
    this.views.clear();
  }
}
