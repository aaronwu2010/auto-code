import * as vscode from 'vscode';
import { ServerClient } from './serverClient';
import { AutoCodeWebviewPanel, AutoCodeSidebarProvider } from './webviewPanel';
import { WorkspaceManager } from './workspace';

let serverClient: ServerClient | undefined;
let webviewPanel: AutoCodeWebviewPanel | undefined;
let sidebarProvider: AutoCodeSidebarProvider | undefined;
let workspaceManager: WorkspaceManager | undefined;
let outputChannel: vscode.OutputChannel | undefined;

export async function activate(context: vscode.ExtensionContext): Promise<void> {
  outputChannel = vscode.window.createOutputChannel('Auto Code');
  context.subscriptions.push(outputChannel);
  log('Auto Code 插件已激活');

  workspaceManager = new WorkspaceManager();
  context.subscriptions.push(workspaceManager);

  serverClient = new ServerClient(context, outputChannel);
  context.subscriptions.push(serverClient);

  // 侧边栏视图
  sidebarProvider = new AutoCodeSidebarProvider(context, serverClient, workspaceManager);
  context.subscriptions.push(
    vscode.window.registerWebviewViewProvider(
      AutoCodeSidebarProvider.viewId,
      sidebarProvider,
      { webviewOptions: { retainContextWhenHidden: true } }
    )
  );

  // 独立面板（保留命令面板入口）
  webviewPanel = new AutoCodeWebviewPanel(context, serverClient, workspaceManager);
  context.subscriptions.push(webviewPanel);

  context.subscriptions.push(
    vscode.commands.registerCommand('auto-code.openChat', () => {
      // 优先聚焦侧边栏视图（用户可见度更高）；同时保留面板以备用户调用。
      sidebarProvider?.reveal().catch(() => {
        webviewPanel?.show();
      });
    }),
    vscode.commands.registerCommand('auto-code.restartServer', async () => {
      log('手动重启 server');
      await serverClient?.restart();
      vscode.window.showInformationMessage('Auto Code server 已重启');
    })
  );

  workspaceManager.onDidChangeWorkspaceFolders(() => {
    log('工作区切换，重启 server 以更新 CWD');
    serverClient?.restart();
  });
}

export function deactivate(): Thenable<void> | undefined {
  log('Auto Code 插件已停用');
  return undefined;
}

function log(message: string): void {
  outputChannel?.appendLine(`[extension] ${message}`);
}

export { serverClient as _serverClientForDebug, outputChannel as _outputChannelForDebug };
