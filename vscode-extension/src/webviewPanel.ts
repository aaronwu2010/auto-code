import * as vscode from 'vscode';
import { ServerClient } from './serverClient';
import { WorkspaceManager } from './workspace';

function getNonce(): string {
  let text = '';
  const possible = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
  for (let i = 0; i < 32; i++) {
    text += possible.charAt(Math.floor(Math.random() * possible.length));
  }
  return text;
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
 * 独立 Webview 面板（命令面板 `Auto Code: Open Chat`）。
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

  async show(): Promise<void> {
    if (this.panel) {
      this.panel.reveal(vscode.ViewColumn.Active);
      return;
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
      vscode.ViewColumn.Active,
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

/**
 * 侧边栏视图（WebviewViewProvider）——通过点击活动栏图标打开。
 */
export class AutoCodeSidebarProvider implements vscode.WebviewViewProvider {
  public static readonly viewId = 'auto-code.chat';
  private view: vscode.WebviewView | undefined;
  private host: AutoCodeWebviewHost | undefined;

  constructor(
    private readonly context: vscode.ExtensionContext,
    private readonly server: ServerClient,
    private readonly workspace: WorkspaceManager
  ) {}

  /** 以编程方式聚焦侧边栏视图。 */
  async reveal(): Promise<void> {
    try {
      await this.server.start();
    } catch (err) {
      vscode.window.showErrorMessage(
        `启动 Auto Code server 失败: ${(err as Error).message}\n请检查 auto-code.serverPath 配置`
      );
    }
    await vscode.commands.executeCommand(
      'setContext',
      'auto-code:sidebarFocused',
      true
    );
  }

  resolveWebviewView(
    webviewView: vscode.WebviewView,
    _context: vscode.WebviewViewResolveContext<unknown>,
    _token: vscode.CancellationToken
  ): void | Thenable<void> {
    this.view = webviewView;

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

      this.host?.dispose();
      this.host = new AutoCodeWebviewHost(
        this.context,
        webviewView.webview,
        this.server,
        this.workspace,
        (m) => webviewView.webview.postMessage(m),
        workspaceDir
      );
      webviewView.webview.html = this.host.getHtml();
    })();

    webviewView.onDidDispose(() => {
      this.host?.dispose();
      this.host = undefined;
      this.view = undefined;
    });
  }
}
