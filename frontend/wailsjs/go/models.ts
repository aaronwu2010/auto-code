export namespace registry {
	
	export class ToolRegistry {
	
	
	    static createFrom(source: any = {}) {
	        return new ToolRegistry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}

}

export namespace state {
	
	export class MCPServerState {
	    name: string;
	    status: string;
	    type: string;
	
	    static createFrom(source: any = {}) {
	        return new MCPServerState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.status = source["status"];
	        this.type = source["type"];
	    }
	}
	export class MCPState {
	    clients: MCPServerState[];
	    tools: any[];
	    commands: any[];
	    resources: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new MCPState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.clients = this.convertValues(source["clients"], MCPServerState);
	        this.tools = source["tools"];
	        this.commands = source["commands"];
	        this.resources = source["resources"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TodoState {
	    id: string;
	    content: string;
	    status: string;
	
	    static createFrom(source: any = {}) {
	        return new TodoState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.content = source["content"];
	        this.status = source["status"];
	    }
	}
	export class TaskState {
	    id: string;
	    type: string;
	    status: string;
	    command?: string;
	    output?: string;
	    exit_code?: number;
	    agent_id?: string;
	
	    static createFrom(source: any = {}) {
	        return new TaskState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.type = source["type"];
	        this.status = source["status"];
	        this.command = source["command"];
	        this.output = source["output"];
	        this.exit_code = source["exit_code"];
	        this.agent_id = source["agent_id"];
	    }
	}
	export class ToolUseState {
	    tool_name: string;
	    tool_use_id: string;
	    input?: any;
	    status: string;
	
	    static createFrom(source: any = {}) {
	        return new ToolUseState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tool_name = source["tool_name"];
	        this.tool_use_id = source["tool_use_id"];
	        this.input = source["input"];
	        this.status = source["status"];
	    }
	}
	export class AppStateSnapshot {
	    mainLoopModel: string;
	    isProcessing: boolean;
	    currentToolUse?: ToolUseState;
	    statusLineText: string;
	    remoteConnectionStatus: string;
	    thinkingEnabled: boolean;
	    fastMode: boolean;
	    tasks: Record<string, TaskState>;
	    todos: Record<string, TodoState>;
	    mcp: MCPState;
	    toolPermissionMode: string;
	
	    static createFrom(source: any = {}) {
	        return new AppStateSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mainLoopModel = source["mainLoopModel"];
	        this.isProcessing = source["isProcessing"];
	        this.currentToolUse = this.convertValues(source["currentToolUse"], ToolUseState);
	        this.statusLineText = source["statusLineText"];
	        this.remoteConnectionStatus = source["remoteConnectionStatus"];
	        this.thinkingEnabled = source["thinkingEnabled"];
	        this.fastMode = source["fastMode"];
	        this.tasks = this.convertValues(source["tasks"], TaskState, true);
	        this.todos = this.convertValues(source["todos"], TodoState, true);
	        this.mcp = this.convertValues(source["mcp"], MCPState);
	        this.toolPermissionMode = source["toolPermissionMode"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class FileInfo {
	    name: string;
	    path: string;
	    is_dir: boolean;
	    size: number;
	    mod_time: string;
	
	    static createFrom(source: any = {}) {
	        return new FileInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.is_dir = source["is_dir"];
	        this.size = source["size"];
	        this.mod_time = source["mod_time"];
	    }
	}
	export class GetMessagesResponse {
	    messages: types.Message[];
	
	    static createFrom(source: any = {}) {
	        return new GetMessagesResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.messages = this.convertValues(source["messages"], types.Message);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ModelInfoUI {
	    name: string;
	    size?: string;
	    family?: string;
	    parameter_size?: string;
	    quantization?: string;
	    context_length?: number;
	
	    static createFrom(source: any = {}) {
	        return new ModelInfoUI(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.size = source["size"];
	        this.family = source["family"];
	        this.parameter_size = source["parameter_size"];
	        this.quantization = source["quantization"];
	        this.context_length = source["context_length"];
	    }
	}
	export class ListModelsResponse {
	    models: ModelInfoUI[];
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new ListModelsResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.models = this.convertValues(source["models"], ModelInfoUI);
	        this.error = source["error"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	
	export class OllamaConfigRequest {
	    base_url: string;
	    api_key: string;
	    model: string;
	
	    static createFrom(source: any = {}) {
	        return new OllamaConfigRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.base_url = source["base_url"];
	        this.api_key = source["api_key"];
	        this.model = source["model"];
	    }
	}
	export class OllamaHealthResponse {
	    connected: boolean;
	    error?: string;
	    is_local: boolean;
	    base_url: string;
	    model: string;
	    available_models: number;
	
	    static createFrom(source: any = {}) {
	        return new OllamaHealthResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connected = source["connected"];
	        this.error = source["error"];
	        this.is_local = source["is_local"];
	        this.base_url = source["base_url"];
	        this.model = source["model"];
	        this.available_models = source["available_models"];
	    }
	}
	export class RegisterToolRequest {
	    name: string;
	    description: string;
	
	    static createFrom(source: any = {}) {
	        return new RegisterToolRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	    }
	}
	export class SendMessageRequest {
	    prompt: string;
	
	    static createFrom(source: any = {}) {
	        return new SendMessageRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.prompt = source["prompt"];
	    }
	}
	export class SendMessageResponse {
	    success: boolean;
	    error?: string;
	    session_id?: string;
	
	    static createFrom(source: any = {}) {
	        return new SendMessageResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.error = source["error"];
	        this.session_id = source["session_id"];
	    }
	}
	export class SetFastModeRequest {
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SetFastModeRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	    }
	}
	export class SetModelRequest {
	    model: string;
	
	    static createFrom(source: any = {}) {
	        return new SetModelRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.model = source["model"];
	    }
	}
	export class SetPermissionModeRequest {
	    mode: string;
	
	    static createFrom(source: any = {}) {
	        return new SetPermissionModeRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	    }
	}
	export class SetThinkingRequest {
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SetThinkingRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	    }
	}
	
	export class TodoRequest {
	    id: string;
	    content: string;
	    status: string;
	
	    static createFrom(source: any = {}) {
	        return new TodoRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.content = source["content"];
	        this.status = source["status"];
	    }
	}
	
	export class ToolInfo {
	    name: string;
	    description: string;
	    isReadOnly: boolean;
	    isEnabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ToolInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.isReadOnly = source["isReadOnly"];
	        this.isEnabled = source["isEnabled"];
	    }
	}

}

export namespace types {
	
	export class ContentBlock {
	    type: string;
	    text?: string;
	    tool_use_id?: string;
	    tool_name?: string;
	    tool_input?: number[];
	    tool_output?: string;
	    is_error?: boolean;
	    thinking?: string;
	
	    static createFrom(source: any = {}) {
	        return new ContentBlock(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.text = source["text"];
	        this.tool_use_id = source["tool_use_id"];
	        this.tool_name = source["tool_name"];
	        this.tool_input = source["tool_input"];
	        this.tool_output = source["tool_output"];
	        this.is_error = source["is_error"];
	        this.thinking = source["thinking"];
	    }
	}
	export class FunctionCall {
	    name: string;
	    arguments?: number[];
	
	    static createFrom(source: any = {}) {
	        return new FunctionCall(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.arguments = source["arguments"];
	    }
	}
	export class ToolCall {
	    function: FunctionCall;
	
	    static createFrom(source: any = {}) {
	        return new ToolCall(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.function = this.convertValues(source["function"], FunctionCall);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Message {
	    id: string;
	    role: string;
	    content: string;
	    tool_calls?: ToolCall[];
	    thinking?: string;
	    images?: string[];
	    model?: string;
	    timestamp: number;
	    is_meta?: boolean;
	    uuid?: string;
	    content_blocks?: ContentBlock[];
	
	    static createFrom(source: any = {}) {
	        return new Message(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.role = source["role"];
	        this.content = source["content"];
	        this.tool_calls = this.convertValues(source["tool_calls"], ToolCall);
	        this.thinking = source["thinking"];
	        this.images = source["images"];
	        this.model = source["model"];
	        this.timestamp = source["timestamp"];
	        this.is_meta = source["is_meta"];
	        this.uuid = source["uuid"];
	        this.content_blocks = this.convertValues(source["content_blocks"], ContentBlock);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

