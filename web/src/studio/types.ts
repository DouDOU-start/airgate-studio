export type ImageMode = 'text2img' | 'img2img' | 'inpaint' | 'batch';

export interface GalleryItem {
  id: string;
  taskId?: number; // 后端任务 ID（删除/去重用）
  url: string;
  prompt: string;
  model: string;
  mode: ImageMode;
  size?: string;
  createdAt: string;
  sourceUrl?: string; // img2img/inpaint 的参考图
}

export interface StudioGenerationTask {
  id: string;
  prompt: string;
  mode: ImageMode;
  status: 'queued' | 'processing' | 'completed' | 'failed';
  result?: GalleryItem[];
  remoteTaskIds?: number[];
  error?: string;
  createdAt: string;
  model?: string;
  size?: string;
}
