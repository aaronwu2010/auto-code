import * as vscode from 'vscode';
import * as path from 'path';
import { ServerClient } from './serverClient';
import { WorkspaceManager } from './workspace';

/**
 * AutoCodeWebviewPanel 管理 Webview 面板的生命周期，
 * 并在 Webview <-> 扩展主进程 <-> Go server 之间桥接消息。
 */
export class AutoCodeWebviewPanel implements vscode.Disposable {
  public static readonly viewType = 'autoCodeChat';
  private panel: vscode.WebviewPanel | undefined;
  private readonly disposables: vscode.Disposable[] = [];

  constructor(
    private readonly context: vscode.ExtensionContext,
    private readonly server: ServerClient,
    private readonly workspace: WorkspaceManager,
    private readonly output: vscode.OutputChannel | undefined
  ) {}

  /** 显示面板。如不存在则创建。 */
  async show(): Promise<void> {
    if (this.panel) {
      this.panel.reveal(vscode.ViewColumn.Active);
      return;
    }

    // 确保 server 已启动
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

    this.panel.webview.html = this.getHtmlForWebview();

    this.disposables.push(
      this.panel.onDidDispose(() => {
        this.panel = undefined;
      }),
      // Webview -> 扩展主进程 -> server
      this.panel.webview.onDidReceiveMessage((msg) => this.onMessageFromWebview(msg)),
      // server 事件 -> Webview
      this.server.onQueryMessage((data) => {
        this.postMessageToWebview({ type: 'event', event: 'query:message', data });
      }),
      this.server.onStateChange((data) => {
        this.postMessageToWebview({ type: 'event', event: 'state:change', data });
      }),
      // 工作区切换 -> 通知 Webview
      this.workspace.onDidChangeWorkspaceFolders(() => {
        this.postMessageToWebview({
          type: 'workspace',
          dir: this.workspace.getCurrentWorkspace() ?? '',
        });
      })
    );

    // 首次加载时推送当前工作区
    this.postMessageToWebview({ type: 'workspace', dir: workspaceDir });
  }

  /** @inheritdoc */
  dispose(): void {
    this.panel?.dispose();
    this.panel = undefined;
    for (const d of this.disposables) {
      d.dispose();
    }
    this.disposables.length = 0;
  }

  // ===== 私有方法 =====

  private async onMessageFromWebview(msg: unknown): Promise<void> {
    if (typeof msg !== 'object' || msg === null) {
      return;
    }
    const m = msg as { type?: string; id?: string; method?: string; params?: unknown };

    // 请求转发
    if (m.type === 'request' && m.method) {
      try {
        const result = await this.server.request(m.method, m.params);
        this.postMessageToWebview({ type: 'response', id: m.id, result });
      } catch (err) {
        this.postMessageToWebview({
          type: 'response',
          id: m.id,
          error: (err as Error).message,
        });
      }
      return;
    }

    // 工作区查询
    if (m.type === 'getWorkspace') {
      this.postMessageToWebview({
        type: 'workspace',
        dir: this.workspace.getCurrentWorkspace() ?? '',
      });
      return;
    }
  }

  private postMessageToWebview(message: unknown): void {
    this.panel?.webview.postMessage(message);
  }

  private getHtmlForWebview(): string {
    const distUri = vscode.Uri.joinPath(this.context.extensionUri, 'webview-ui', 'dist');
    const scriptUri = this.panel!.webview.asWebviewUri(
      vscode.Uri.joinPath(distUri, 'assets', 'index.js')
    );
    const styleUri = this.panel!.webview.asWebviewUri(
      vscode.Uri.joinPath(distUri, 'assets', 'index.css')
    );
    const nonce = getNonce();

    return /* html */ `<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <meta http-equiv="Content-Security-Policy"
        content="default-src 'none'; style-src ${this.panel!.webview.cspSource} 'unsafe-inline'; script-src 'nonce-${nonce}'; font-src ${this.panel!.webview.cspSource};" />
  <title>Auto Code</title>
  <link rel="stylesheet" href="${styleUri}" />
</head>
<body>
  <div id="root"></div>
  <script nonce="${nonce}" src="${scriptUri}"></script>
</body>
</html>`;
  }
}

function getNonce(): string {
  let text = '';
  const possible = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
  for (let i = 0; i < 32; i++) {
    text += possible.charAt(Math.floor(Math.random() * possible.length));
  }
  return text;
}
