import * as vscode from 'vscode';

/**
 * WorkspaceManager 监听 VS Code 工作区变化，
 * 并提供当前工作区根目录。
 */
export class WorkspaceManager implements vscode.Disposable {
  private readonly disposables: vscode.Disposable[] = [];
  private readonly onChangeEmitter = new vscode.EventEmitter<void>();

  constructor() {
    this.disposables.push(
      vscode.workspace.onDidChangeWorkspaceFolders(() => {
        this.onChangeEmitter.fire();
      })
    );
  }

  /** 当前工作区根目录。无工作区时返回 undefined。 */
  getCurrentWorkspace(): string | undefined {
    return vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
  }

  /** 工作区切换事件。 */
  readonly onDidChangeWorkspaceFolders = this.onChangeEmitter.event;

  dispose(): void {
    for (const d of this.disposables) {
      d.dispose();
    }
    this.disposables.length = 0;
    this.onChangeEmitter.dispose();
  }
}
