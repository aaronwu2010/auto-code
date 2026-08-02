import * as vscode from 'vscode';
import * as cp from 'child_process';
import * as path from 'path';
import * as readline from 'readline';
import { EventEmitter } from 'events';

/** 简易 Disposable 接口，避免依赖 Node events 模块的同名导出。 */
export interface Disposable {
  dispose(): void;
}

/**
 * ServerClient 管理 auto-code-server Go 子进程的生命周期，
 * 并提供基于 NDJSON-RPC 的请求/响应/事件通信。
 *
 * 通信协议：
 *   请求（前端 -> Go）：{"id":"req-1","method":"send_message","params":{...}}
 *   响应（Go -> 前端）：{"id":"req-1","result":{...}}
 *   事件（Go -> 前端）：{"event":"query:message","data":{...}}
 *
 * 每行一个 JSON 对象，行尾 \n。
 */
export class ServerClient implements vscode.Disposable {
  private child: cp.ChildProcess | undefined;
  private readonly pending = new Map<string, {
    resolve: (value: unknown) => void;
    reject: (err: Error) => void;
    timer: NodeJS.Timeout;
  }>();
  private nextId = 1;
  private readonly events = new EventEmitter();
  private disposables: Disposable[] = [];
  private restarting = false;
  private disposed = false;

  constructor(
    private readonly context: vscode.ExtensionContext,
    private readonly output: vscode.OutputChannel | undefined
  ) {}

  /** 启动子进程。已启动则返回当前实例。 */
  async start(): Promise<void> {
    if (this.child && !this.child.killed) {
      return;
    }
    const cwd = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath ?? process.cwd();
    const serverPath = this.resolveServerPath();
    const args = ['--cwd', cwd];

    this.log(`启动 server: ${serverPath} ${args.join(' ')}`);

    try {
      this.child = cp.spawn(serverPath, args, {
        stdio: ['pipe', 'pipe', 'pipe'],
        shell: false,
      });
    } catch (err) {
      this.log(`启动 server 失败: ${(err as Error).message}`);
      throw err;
    }

    this.disposables.push(
      this.onEvent('server:exit', () => {
        // 标记所有 pending 请求失败
        for (const [id, entry] of this.pending) {
          entry.reject(new Error('server exited'));
          clearTimeout(entry.timer);
          this.pending.delete(id);
        }
        if (!this.restarting && !this.disposed) {
          this.log('server 进程意外退出，2s 后自动重启');
          setTimeout(() => this.restart().catch(() => {}), 2000);
        }
      })
    );

    // 处理 stderr（日志）
    if (this.child.stderr) {
      const stderr = readline.createInterface({ input: this.child.stderr });
      stderr.on('line', (line) => this.log(`[server stderr] ${line}`));
    }

    // 处理 stdout（NDJSON 协议）
    if (this.child.stdout) {
      const stdout = readline.createInterface({ input: this.child.stdout });
      stdout.on('line', (line) => this.handleLine(line));
    }

    this.child.on('exit', (code, signal) => {
      this.log(`server 进程退出 code=${code} signal=${signal}`);
      this.events.emit('server:exit', { code, signal });
    });

    this.child.on('error', (err) => {
      this.log(`server 进程错误: ${err.message}`);
      vscode.window.showErrorMessage(`Auto Code server 错误: ${err.message}`);
    });

    // 等待 server 就绪信号（stderr 输出 "ready"）
    await this.waitForReady();
  }

  /** 重启 server。 */
  async restart(): Promise<void> {
    if (this.restarting) {
      return;
    }
    this.restarting = true;
    try {
      this.kill();
      // 短暂等待端口/资源释放
      await new Promise((r) => setTimeout(r, 300));
      await this.start();
    } finally {
      this.restarting = false;
    }
  }

  /** 发起一次 RPC 请求并等待响应。 */
  async request<T = unknown>(method: string, params?: unknown, timeoutMs = 30000): Promise<T> {
    if (!this.child || this.child.killed) {
      await this.start();
    }
    const id = `req-${this.nextId++}`;
    const line = JSON.stringify({ id, method, params: params ?? {} }) + '\n';

    return new Promise<T>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error(`请求超时: ${method} (${timeoutMs}ms)`));
      }, timeoutMs);

      this.pending.set(id, {
        resolve: (v) => {
          clearTimeout(timer);
          this.pending.delete(id);
          resolve(v as T);
        },
        reject: (err) => {
          clearTimeout(timer);
          this.pending.delete(id);
          reject(err);
        },
        timer,
      });

      this.child!.stdin!.write(line);
    });
  }

  /** 监听一个事件。返回 Disposable 用于取消监听。 */
  onEvent(event: string, listener: (...args: unknown[]) => void): Disposable {
    this.events.on(event, listener);
    return { dispose: () => this.events.off(event, listener) };
  }

  /** 监听 query:message 事件（流式回复）。 */
  onQueryMessage(listener: (data: unknown) => void): Disposable {
    return this.onEvent('query:message', listener as (...args: unknown[]) => void);
  }

  /** 监听 state:change 事件。 */
  onStateChange(listener: (data: unknown) => void): Disposable {
    return this.onEvent('state:change', listener as (...args: unknown[]) => void);
  }

  /** @inheritdoc */
  dispose(): void {
    this.disposed = true;
    for (const d of this.disposables) {
      d.dispose();
    }
    this.disposables = [];
    for (const [, entry] of this.pending) {
      entry.reject(new Error('client disposed'));
      clearTimeout(entry.timer);
    }
    this.pending.clear();
    this.kill();
  }

  // ===== 私有方法 =====

  private handleLine(line: string): void {
    if (!line.trim()) {
      return;
    }
    let obj: Record<string, unknown>;
    try {
      obj = JSON.parse(line);
    } catch (err) {
      this.log(`无法解析 server 输出: ${(err as Error).message}, line=${line.slice(0, 200)}`);
      return;
    }

    // 响应（带 id 且无 event 字段）
    if (typeof obj.id === 'string' && obj.event === undefined) {
      const entry = this.pending.get(obj.id);
      if (!entry) {
        this.log(`收到未知 id 的响应: ${obj.id}`);
        return;
      }
      if (obj.error) {
        entry.reject(new Error((obj.error as { message?: string }).message ?? 'unknown error'));
      } else {
        entry.resolve(obj.result);
      }
      return;
    }

    // 事件（带 event 字段）
    if (typeof obj.event === 'string') {
      this.events.emit(obj.event, obj.data);
      return;
    }

    this.log(`无法识别的 server 输出: ${line.slice(0, 200)}`);
  }

  private resolveServerPath(): string {
    const config = vscode.workspace.getConfiguration('auto-code');
    const configured = config.get<string>('serverPath', '');
    if (configured) {
      return configured;
    }
    // 默认使用扩展自带的 bin 目录
    const platform = process.platform;
    const exeName = platform === 'win32' ? 'auto-code-server.exe' : 'auto-code-server';
    return path.join(this.context.extensionPath, 'bin', exeName);
  }

  private async waitForReady(): Promise<void> {
    // server 启动后会向 stderr 输出 "ready"，但我们在 stderr handler 中只是打日志。
    // 这里简单地轮询健康检查：发送 check_health，成功即视为就绪。
    // 给 server 一点启动时间。
    await new Promise((r) => setTimeout(r, 200));
    for (let i = 0; i < 10; i++) {
      try {
        await this.request('get_session_id', {}, 2000);
        this.log('server 已就绪');
        return;
      } catch {
        await new Promise((r) => setTimeout(r, 300));
      }
    }
    this.log('server 启动后健康检查未通过，但继续运行');
  }

  private kill(): void {
    if (this.child && !this.child.killed) {
      try {
        this.child.kill();
      } catch {
        // ignore
      }
    }
    this.child = undefined;
  }

  private log(message: string): void {
    this.output?.appendLine(`[serverClient] ${message}`);
  }
}
