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

  // 共享的 WebviewViewProvider：同时挂到 3 个容器
  //  - Secondary Side Bar (代码旁边，用户首选位置)  viewId = auto-code.chat
  //  - Activity Bar (左侧，保留原有图标入口)       viewId = auto-code.activityChat
  //  - Panel (底部面板，可拖拽到右侧)               viewId = auto-code.panelChat
  sidebarProvider = new AutoCodeSidebarProvider(context, serverClient, workspaceManager);
  const commonViewOptions = { webviewOptions: { retainContextWhenHidden: true } };
  context.subscriptions.push(
    vscode.window.registerWebviewViewProvider(
      AutoCodeSidebarProvider.sideBarViewId,
      sidebarProvider,
      commonViewOptions
    ),
    vscode.window.registerWebviewViewProvider(
      AutoCodeSidebarProvider.activityViewId,
      sidebarProvider,
      commonViewOptions
    ),
    vscode.window.registerWebviewViewProvider(
      AutoCodeSidebarProvider.panelViewId,
      sidebarProvider,
      commonViewOptions
    )
  );

  // 独立面板（保留命令面板入口）
  webviewPanel = new AutoCodeWebviewPanel(context, serverClient, workspaceManager);
  context.subscriptions.push(webviewPanel);

  context.subscriptions.push(
    // 主要入口（Explorer 标题栏图标 + 命令面板默认）→ 在最右侧编辑器列打开（大方框效果）
    vscode.commands.registerCommand('auto-code.openChat', () => {
      webviewPanel?.show(vscode.ViewColumn.Beside);
    }),
    // 备用入口：在 Secondary Side Bar 显示（如果用户喜欢右侧列窄视图）
    vscode.commands.registerCommand('auto-code.openChatSidebar', () => {
      sidebarProvider?.reveal().catch(() => {
        webviewPanel?.show(vscode.ViewColumn.Beside);
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
