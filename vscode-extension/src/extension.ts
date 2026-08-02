import * as vscode from 'vscode';
import { ServerClient } from './serverClient';
import { AutoCodeWebviewPanel } from './webviewPanel';
import { WorkspaceManager } from './workspace';

let serverClient: ServerClient | undefined;
let webviewPanel: AutoCodeWebviewPanel | undefined;
let workspaceManager: WorkspaceManager | undefined;
let outputChannel: vscode.OutputChannel | undefined;

export async function activate(context: vscode.ExtensionContext): Promise<void> {
  outputChannel = vscode.window.createOutputChannel('Auto Code');
  context.subscriptions.push(outputChannel);
  log('Auto Code 插件已激活');

  workspaceManager = new WorkspaceManager();
  context.subscriptions.push(workspaceManager);

  // 创建 ServerClient（懒启动，首次 openChat 时启动）
  serverClient = new ServerClient(context, outputChannel);
  context.subscriptions.push(serverClient);

  // 创建 Webview 面板管理器
  webviewPanel = new AutoCodeWebviewPanel(context, serverClient, workspaceManager, outputChannel);
  context.subscriptions.push(webviewPanel);

  // 注册命令
  context.subscriptions.push(
    vscode.commands.registerCommand('auto-code.openChat', () => {
      webviewPanel?.show();
    }),
    vscode.commands.registerCommand('auto-code.restartServer', async () => {
      log('手动重启 server');
      await serverClient?.restart();
      vscode.window.showInformationMessage('Auto Code server 已重启');
    })
  );

  // 工作区切换时通知 server（需要重启 server 才能生效，因为 CWD 在启动时确定）
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
