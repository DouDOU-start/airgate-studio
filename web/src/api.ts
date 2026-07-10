// 独立部署 API 层：同源 cookie 会话（不再携带 core JWT），基路径 /api。
// 401 统一跳转 /auth/login 走 core 单点登录。

function redirectToLogin(): void {
  window.location.href = '/auth/login';
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const url = `/api${path}`;
  const headers: Record<string, string> = {};
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  const options: RequestInit = {
    method,
    headers,
    credentials: 'same-origin',
  };
  if (body !== undefined) {
    options.body = JSON.stringify(body);
  }
  const resp = await fetch(url, options);
  if (resp.status === 401) {
    redirectToLogin();
    throw new Error('unauthorized');
  }
  if (!resp.ok) {
    const err = await resp.json().catch(() => ({ error: resp.statusText }));
    throw new Error(
      (typeof err?.error === 'string' ? err.error : err?.error?.message) || `HTTP ${resp.status}`,
    );
  }
  return resp.json();
}

export interface GenerationTask {
  id: number;
  task_id: number;
  public_task_id?: string;
  status: string;
  progress: number;
  prompt: string;
  model?: string;
  operation?: string;
  size?: string;
  quality?: string;
  input_images?: string[];
  input_mask?: string;
  result_content?: string;
  images?: string[];
  error_message?: string;
  created_at: string;
  updated_at?: string;
  completed_at?: string;
}

export interface ModelInfo {
  id: string;
  object?: string;
  owned_by?: string;
  /** core 非标扩展：模型支持的协议（"openai" | "anthropic" | "gemini" ...）。 */
  protocols?: string[];
}

export interface UserInfo {
  user_id: number;
  airgate_user_id: number;
  username: string;
  email: string;
  api_key_ready: boolean;
  balance: number | null;
  balance_detail?: Record<string, unknown>;
}

export const api = {
  // ── 会话 ──────────────────────────────────────────────────────────────────

  getUserInfo(): Promise<UserInfo> {
    return request('GET', '/user/info');
  },

  async logout(): Promise<void> {
    await fetch('/auth/logout', { method: 'POST', credentials: 'same-origin' });
  },

  // ── 生成任务 ──────────────────────────────────────────────────────────────

  createGenerationTask(params: {
    kind: string;
    operation: string;
    platform: string;
    model: string;
    prompt: string;
    group_id?: number;
    parameters?: Record<string, unknown>;
    inputs?: Array<{ type: string; role: string; url: string }>;
    mask?: { type: string; role: string; url: string };
  }): Promise<GenerationTask> {
    return request('POST', '/generation-tasks', params);
  },

  getGenerationTask(taskId: number): Promise<GenerationTask> {
    return request('GET', `/generation-tasks/${taskId}`);
  },

  listGenerationTasks(params?: { limit?: number; offset?: number; status?: string }): Promise<{ tasks: GenerationTask[]; total: number }> {
    const qs = new URLSearchParams();
    if (params?.limit) qs.set('limit', String(params.limit));
    if (params?.offset) qs.set('offset', String(params.offset));
    if (params?.status) qs.set('status', params.status);
    const suffix = qs.toString() ? `?${qs}` : '';
    return request<{ tasks: GenerationTask[]; total: number }>('GET', `/generation-tasks${suffix}`)
      .then(r => ({ tasks: r.tasks || [], total: r.total || 0 }));
  },

  deleteGenerationTask(taskId: number): Promise<void> {
    return request('DELETE', `/generation-tasks/${taskId}`);
  },

  // ── 模型 ─────────────────────────────────────────────────────────────────

  // 透传 core 的 GET /v1/models（OpenAI 形态 {object:"list", data:[...]}）
  listModels(): Promise<ModelInfo[]> {
    return request<{ data?: ModelInfo[] }>('GET', '/models').then(r => r.data || []);
  },
};
