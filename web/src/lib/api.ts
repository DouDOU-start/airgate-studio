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
  status: string;
  progress: number;
  prompt: string;
  model?: string;
  operation?: string;
  size?: string;
  quality?: string;
  input_images?: string[];
  input_mask?: string;
  /** 产物图片 URL 列表（画廊消费）。 */
  images?: string[];
  /** 统一产物形态；视频/音乐模态就绪后画廊按 type 渲染。 */
  assets?: GeneratedAsset[];
  error_message?: string;
  created_at: string;
  completed_at?: string;
}

export interface ModelInfo {
  id: string;
  object?: string;
  owned_by?: string;
  /** core 非标扩展：模型支持的协议（"openai" | "anthropic" | "gemini" ...）。 */
  protocols?: string[];
  /** 分组货架模式：管理员配置的展示名（缺省同 id）。 */
  display_name?: string;
  /** 模型模态（image/video/music）；前端按此归类创作模式，当前仅 image 可创建任务。 */
  modality?: string;
}

/**
 * 统一产物形态；视频/音乐模态就绪后画廊按 type 渲染对应播放器。
 * type 是「产物文件类型」词表（audio），与模型 modality 词表（music）刻意区分——
 * 音乐模型的产物是 audio 文件。
 */
export interface GeneratedAsset {
  type: 'image' | 'video' | 'audio' | string;
  url: string;
}

export interface UserInfo {
  user_id: number;
  airgate_user_id: number;
  username: string;
  email: string;
  api_key_ready: boolean;
  is_admin?: boolean;
  balance: number | null;
  balance_detail?: Record<string, unknown>;
}

/** 用户可见的已开放分组。key_ready=false 表示分组在上次登录后才开放，重新登录即可用。 */
export interface GroupOption {
  core_group_id: number;
  name: string;
  rate_multiplier: number;
  note: string;
  key_ready: boolean;
}

/** 管理端：分组镜像行。 */
export interface AdminGroup {
  core_group_id: number;
  name: string;
  rate_multiplier: number;
  note: string;
  enabled: boolean;
  synced_at: string;
}

/** 管理端：货架模型行。 */
export interface AdminModel {
  id: number;
  core_group_id: number;
  model_name: string;
  display_name: string;
  protocols: string[];
  modality: string;
  enabled: boolean;
  sort_order: number;
  missing_at_core: boolean;
  synced_at: string;
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

  // ── 分组与模型 ────────────────────────────────────────────────────────────

  /** 已开放分组列表；空列表 = 管理员尚未开放任何分组（无模型可用）。 */
  listGroups(): Promise<GroupOption[]> {
    return request<{ groups?: GroupOption[] }>('GET', '/groups').then(r => r.groups || []);
  },

  /** 该分组已上架的货架模型（模型供给只有货架一条路，group_id 必填）。 */
  listModels(groupId: number): Promise<ModelInfo[]> {
    return request<{ data?: ModelInfo[] }>('GET', `/models?group_id=${groupId}`).then(r => r.data || []);
  },

  // ── 管理端（管理员白名单可见） ────────────────────────────────────────────

  adminListGroups(): Promise<AdminGroup[]> {
    return request<{ groups?: AdminGroup[] }>('GET', '/admin/groups').then(r => r.groups || []);
  },

  adminSetGroupEnabled(coreGroupID: number, enabled: boolean): Promise<void> {
    return request('PUT', `/admin/groups/${coreGroupID}`, { enabled });
  },

  adminSyncModels(coreGroupID: number): Promise<{ synced: number }> {
    return request('POST', `/admin/groups/${coreGroupID}/sync-models`);
  },

  adminListModels(coreGroupID: number): Promise<AdminModel[]> {
    return request<{ models?: AdminModel[] }>('GET', `/admin/models?group_id=${coreGroupID}`).then(r => r.models || []);
  },

  adminUpdateModel(id: number, patch: { display_name?: string; modality?: string; enabled?: boolean; sort_order?: number }): Promise<AdminModel> {
    return request('PUT', `/admin/models/${id}`, patch);
  },
};
