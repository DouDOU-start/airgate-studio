import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import { api } from '../api';
import type { GenerationTask } from '../api';
import type { GalleryItem, StudioGenerationTask, ImageMode, MediaType } from './types';
import { getModelConfig, getDefaultModel, MODEL_REGISTRY, type ModelConfig } from './modelConfig';

// ── Constants ─────────────────────────────────────────────────────────────────

const POLL_INTERVAL_MS = 2000;
const POLL_MAX_ATTEMPTS = 300;

// ── Helpers ───────────────────────────────────────────────────────────────────

function uid(): string {
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
}

function parseMarkdownImages(text: string): Array<{ url: string; alt: string }> {
  const regex = /!\[([^\]]*)\]\(([^)\s]+)\)/g;
  const results: Array<{ url: string; alt: string }> = [];
  let match: RegExpExecArray | null;
  while ((match = regex.exec(text)) !== null) {
    results.push({ alt: match[1], url: match[2] });
  }
  return results;
}

function uniqueNumbers(values: Array<number | undefined | null>): number[] {
  const out: number[] = [];
  const seen = new Set<number>();
  for (const value of values) {
    if (!value || seen.has(value)) continue;
    seen.add(value);
    out.push(value);
  }
  return out;
}

function operationToImageMode(operation: string): ImageMode {
  if (operation === 'inpaint') return 'inpaint';
  if (operation === 'edit') return 'img2img';
  return 'text2img';
}

function modeToOperation(mode: ImageMode): 'generate' | 'edit' | 'inpaint' {
  if (mode === 'inpaint') return 'inpaint';
  if (mode === 'img2img') return 'edit';
  return 'generate';
}

interface GenerateOptions {
  mode?: ImageMode;
  sourceImage?: string;
  sourceImages?: string[];
  maskRegion?: { x: number; y: number; width: number; height: number };
  count?: number;
  prompts?: string[];
}

function taskRemoteIds(task: StudioGenerationTask | undefined): number[] {
  if (!task) return [];
  const recoveredId = task.id.startsWith('r-') ? Number(task.id.slice(2)) : undefined;
  return uniqueNumbers([
    ...(task.remoteTaskIds || []),
    recoveredId,
    ...(task.result || []).map(item => item.taskId),
  ]);
}

function resolveGenerationMode(currentMode: ImageMode, options?: GenerateOptions): ImageMode {
  if (options?.mode) return options.mode;
  if (options?.maskRegion) return 'inpaint';
  if (options?.sourceImage || options?.sourceImages?.length) return 'img2img';
  return currentMode;
}

function taskSize(task: GenerationTask): string | undefined {
  return task.size ?? undefined;
}

function taskAssetCreatedAt(task: GenerationTask): string {
  return task.completed_at || task.created_at;
}

function taskSourceUrl(task: GenerationTask): string | undefined {
  return task.input_images?.find(url => !!url);
}

async function delay(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal.aborted) {
      reject(new DOMException('Aborted', 'AbortError'));
      return;
    }
    const timer = setTimeout(() => {
      signal.removeEventListener('abort', onAbort);
      resolve();
    }, ms);
    function onAbort() {
      clearTimeout(timer);
      reject(new DOMException('Aborted', 'AbortError'));
    }
    signal.addEventListener('abort', onAbort, { once: true });
  });
}

async function createMaskDataUrl(
  sourceUrl: string,
  region: { x: number; y: number; width: number; height: number },
): Promise<string> {
  const img = new window.Image();
  img.src = sourceUrl;
  await new Promise<void>((res, rej) => {
    img.onload = () => res();
    img.onerror = () => rej(new Error('Failed to load source image for mask'));
  });
  const canvas = document.createElement('canvas');
  canvas.width = img.naturalWidth;
  canvas.height = img.naturalHeight;
  const ctx = canvas.getContext('2d');
  if (!ctx) throw new Error('Cannot create canvas context');
  const clamp = (value: number, min: number, max: number) => Math.max(min, Math.min(max, value));
  const x1 = clamp(Math.round(region.x * canvas.width), 0, canvas.width);
  const y1 = clamp(Math.round(region.y * canvas.height), 0, canvas.height);
  const x2 = clamp(Math.round((region.x + region.width) * canvas.width), 0, canvas.width);
  const y2 = clamp(Math.round((region.y + region.height) * canvas.height), 0, canvas.height);
  const x = Math.min(x1, x2);
  const y = Math.min(y1, y2);
  const w = Math.max(1, Math.abs(x2 - x1));
  const h = Math.max(1, Math.abs(y2 - y1));
  ctx.fillStyle = '#ffffff';
  ctx.fillRect(0, 0, canvas.width, canvas.height);
  ctx.clearRect(x, y, w, h);
  return canvas.toDataURL('image/png');
}

async function pollGenerationTask(
  taskId: number,
  signal: AbortSignal,
  maxAttempts = POLL_MAX_ATTEMPTS,
): Promise<GenerationTask> {
  let networkErrors = 0;
  for (let attempt = 0; attempt < maxAttempts; attempt++) {
    if (signal.aborted) throw new DOMException('Aborted', 'AbortError');
    let task: GenerationTask | null = null;
    try {
      task = await api.getGenerationTask(taskId);
      networkErrors = 0;
    } catch (err) {
      if (signal.aborted) throw new DOMException('Aborted', 'AbortError');
      networkErrors++;
      if (networkErrors > 150) throw err;
    }
    if (task) {
      if (task.status === 'completed') return task;
      if (task.status === 'failed') {
        throw new Error(task.error_message || 'Image generation task failed');
      }
    }
    const backoff = networkErrors > 0 ? Math.min(POLL_INTERVAL_MS * 2, 6000) : POLL_INTERVAL_MS;
    await delay(backoff, signal);
  }
  throw new Error('Image generation timed out after waiting too long');
}

// ── Context type ──────────────────────────────────────────────────────────────

export interface StudioContextValue {
  // Media type
  mediaType: MediaType;
  setMediaType: (type: MediaType) => void;

  // Image mode
  imageMode: ImageMode;
  setImageMode: (mode: ImageMode) => void;

  // Model config
  currentModel: ModelConfig;
  selectedModelId: string;
  setSelectedModelId: (id: string) => void;
  selectedPlatform: string;
  imageSize: string;
  setImageSize: (size: string) => void;

  // Reference images (for img2img / inpaint).
  // Array so multiple gallery items can be added as references; ComposerBar
  // unions this with its locally uploaded sourceImages.
  referenceImages: string[];
  setReferenceImages: (urls: string[]) => void;

  // Generation
  isGenerating: boolean;
  tasks: StudioGenerationTask[];
  generate: (prompt: string, options?: GenerateOptions) => void;
  cancelGeneration: () => void;

  // Gallery
  gallery: GalleryItem[];
  hasMore: boolean;
  loadingMore: boolean;
  loadMore: () => void;
  generatedAssetRetentionDays: number | null;
  previewItem: GalleryItem | null;
  setPreviewItem: (item: GalleryItem | null) => void;
  deleteGalleryItem: (id: string) => void;
  deleteTask: (uiId: string) => void;
  useAsReference: (item: GalleryItem) => void;
  regenerate: (item: GalleryItem) => void;

}

// ── Context + hook ────────────────────────────────────────────────────────────

const StudioContext = createContext<StudioContextValue | null>(null);

export function useStudio(): StudioContextValue {
  const ctx = useContext(StudioContext);
  if (!ctx) throw new Error('useStudio must be used within StudioProvider');
  return ctx;
}

// ── Provider ──────────────────────────────────────────────────────────────────

export function StudioProvider({ children }: { children: ReactNode }) {
  // Media type & mode
  const [mediaType, setMediaType] = useState<MediaType>('image');
  const [imageMode, setImageMode] = useState<ImageMode>('text2img');

  // Model selection (hardcoded registry)
  const [selectedModelId, setSelectedModelIdRaw] = useState(getDefaultModel().id);
  const [imageSize, setImageSize] = useState(getDefaultModel().defaultSize);

  // Reference images (accumulated via "use as reference" from gallery)
  const [referenceImages, setReferenceImages] = useState<string[]>([]);

  // Generation
  const [isGenerating, setIsGenerating] = useState(false);
  const [tasks, setTasks] = useState<StudioGenerationTask[]>([]);
  const abortRef = useRef<AbortController | null>(null);

  // Gallery
  const [gallery, setGallery] = useState<GalleryItem[]>([]);
  const [previewItem, setPreviewItem] = useState<GalleryItem | null>(null);
  const [hasMore, setHasMore] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [generatedAssetRetentionDays, setGeneratedAssetRetentionDays] = useState<number | null>(null);
  const galleryOffsetRef = useRef(0);

  const recoveryPromiseRef = useRef<Promise<void> | null>(null);

  // Derived from hardcoded registry
  const currentModel = getModelConfig(selectedModelId) ?? getDefaultModel();
  const selectedPlatform = currentModel.platform;

  const setSelectedModelId = useCallback((id: string) => {
    setSelectedModelIdRaw(id);
    const newModel = getModelConfig(id);
    if (newModel && !newModel.sizes.some(s => s.value === imageSize)) {
      setImageSize(newModel.defaultSize);
    }
  }, [imageSize]);

  // ── Initialization ────────────────────────────────────────────────────────

  const PAGE_SIZE = 20;

  useEffect(() => {
    let active = true;
    api.getPublicSettings()
      .then((settings) => {
        if (!active) return;
        const raw = settings.asset_retention_generated_days?.trim();
        const parsed = raw ? Number.parseInt(raw, 10) : Number.NaN;
        setGeneratedAssetRetentionDays(Number.isFinite(parsed) && parsed > 0 ? parsed : null);
      })
      .catch(() => {
        if (active) setGeneratedAssetRetentionDays(null);
      });
    return () => {
      active = false;
    };
  }, []);

  function tasksToGallery(taskList: GenerationTask[]): GalleryItem[] {
    const items: GalleryItem[] = [];
    for (const t of taskList) {
      if (t.status !== 'completed' || !t.result_content) continue;
      for (const img of parseMarkdownImages(t.result_content)) {
        items.push({
          id: uid(),
          taskId: t.id,
          url: img.url,
          alt: img.alt,
          prompt: t.prompt,
          model: t.model ?? '',
          mode: operationToImageMode(t.operation ?? 'generate'),
          size: taskSize(t),
          createdAt: taskAssetCreatedAt(t),
          sourceUrl: taskSourceUrl(t),
        });
      }
    }
    return items;
  }

  const recoverTasks = useCallback(async (signal: AbortSignal) => {
    try {
      const [
        { tasks: completedTasks, total: completedTotal },
        { tasks: recentTasks },
      ] = await Promise.all([
        api.listGenerationTasks({ limit: PAGE_SIZE, offset: 0, status: 'completed' }),
        api.listGenerationTasks({ limit: PAGE_SIZE, offset: 0 }),
      ]);
      if (signal.aborted) return;

      setGallery(tasksToGallery(completedTasks));
      galleryOffsetRef.current = completedTasks.length;
      setHasMore(completedTasks.length < completedTotal);

      const failed = recentTasks.filter(t => t.status === 'failed');
      const inFlight = recentTasks.filter(
        t => t.status === 'pending' || t.status === 'processing',
      );

      setTasks([
        ...failed.map(t => ({
          id: `r-${t.id}`,
          prompt: t.prompt,
          mode: operationToImageMode(t.operation ?? 'generate'),
          status: 'failed' as const,
          error: t.error_message || 'Image generation task failed',
          createdAt: t.created_at,
          model: t.model,
          size: t.size,
          remoteTaskIds: [t.id],
        })),
        ...inFlight.map(t => ({
          id: `r-${t.id}`,
          prompt: t.prompt,
          mode: operationToImageMode(t.operation ?? 'generate'),
          status: 'processing' as const,
          createdAt: t.created_at,
          model: t.model,
          size: t.size,
          remoteTaskIds: [t.id],
        })),
      ]);
      if (inFlight.length === 0) return;

      setIsGenerating(true);
      activeCountRef.current = inFlight.length;
      for (const t of inFlight) {
        const taskUiId = `r-${t.id}`;
        pollGenerationTask(t.id, signal)
          .then(done => {
            if (signal.aborted) return;
            const imgs = parseMarkdownImages(done.result_content || '');
            setGallery(prev => [
              ...imgs.map(img => ({
                id: uid(),
                taskId: t.id,
                url: img.url,
                alt: img.alt,
                prompt: t.prompt,
                model: t.model ?? '',
                mode: operationToImageMode(t.operation ?? 'generate'),
                size: taskSize(done),
                createdAt: taskAssetCreatedAt(done),
                sourceUrl: taskSourceUrl(done),
              })),
              ...prev,
            ]);
            setTasks(prev =>
              prev.map(gt =>
                gt.id === taskUiId ? { ...gt, status: 'completed' } : gt,
              ),
            );
          })
          .catch(err => {
            if (signal.aborted) return;
            const msg = err instanceof Error ? err.message : 'Recovery failed';
            setTasks(prev =>
              prev.map(gt =>
                gt.id === taskUiId
                  ? { ...gt, status: 'failed', error: msg }
                  : gt,
              ),
            );
          })
          .finally(() => {
            if (signal.aborted) return;
            activeCountRef.current -= 1;
            if (activeCountRef.current <= 0) {
              activeCountRef.current = 0;
              setIsGenerating(false);
            }
          });
      }
    } catch {
      // task recovery is non-fatal
    }
  }, []);

  const loadMore = useCallback(async () => {
    if (loadingMore || !hasMore) return;
    setLoadingMore(true);
    try {
      const { tasks: moreTasks, total } = await api.listGenerationTasks({
        limit: PAGE_SIZE,
        offset: galleryOffsetRef.current,
        status: 'completed',
      });
      const newItems = tasksToGallery(moreTasks);
      setGallery(prev => [...prev, ...newItems]);
      galleryOffsetRef.current += moreTasks.length;
      setHasMore(galleryOffsetRef.current < total);
    } catch { /* non-fatal */ }
    setLoadingMore(false);
  }, [loadingMore, hasMore]);

  useEffect(() => {
    if (!recoveryPromiseRef.current) {
      const controller = new AbortController();
      recoveryPromiseRef.current = recoverTasks(controller.signal);
    }
  }, [recoverTasks]);

  // Re-check processing tasks on visibility change (e.g. tab switch back, service restart)
  useEffect(() => {
    const refresh = async () => {
      const processing = tasks.filter(t => t.status === 'processing' || t.status === 'queued');
      if (processing.length === 0) return;
      const checks = processing.map(async (uiTask) => {
        const remoteId = taskRemoteIds(uiTask)[0] ?? null;
        if (!remoteId) return;
        try {
          const remote = await api.getGenerationTask(remoteId);
          if (remote.status === 'completed' && remote.result_content) {
            const imgs = parseMarkdownImages(remote.result_content);
            setGallery(prev => [
              ...imgs.map(img => ({
                id: uid(),
                taskId: remoteId,
                url: img.url,
                alt: img.alt,
                prompt: remote.prompt,
                model: remote.model ?? '',
                mode: operationToImageMode(remote.operation ?? 'generate'),
                size: taskSize(remote),
                createdAt: taskAssetCreatedAt(remote),
                sourceUrl: taskSourceUrl(remote),
              })),
              ...prev,
            ]);
            setTasks(prev => prev.map(gt => gt.id === uiTask.id ? { ...gt, status: 'completed' } : gt));
          } else if (remote.status === 'failed') {
            setTasks(prev => prev.map(gt => gt.id === uiTask.id
              ? { ...gt, status: 'failed', error: remote.error_message || 'Task failed' }
              : gt));
          }
        } catch { /* single task check is non-fatal */ }
      });
      await Promise.all(checks);
    };

    const onVisibility = () => { if (document.visibilityState === 'visible') void refresh(); };
    const onFocus = () => void refresh();
    document.addEventListener('visibilitychange', onVisibility);
    window.addEventListener('focus', onFocus);
    return () => {
      document.removeEventListener('visibilitychange', onVisibility);
      window.removeEventListener('focus', onFocus);
    };
  }, [tasks]);

  // ── Generation ────────────────────────────────────────────────────────────

  const cancelGeneration = useCallback(() => {
    abortRef.current?.abort();
  }, []);

  const activeCountRef = useRef(0);

  const generate = useCallback(
    (
      prompt: string,
      options?: GenerateOptions,
    ) => {
      if (!prompt.trim()) return;

      const controller = new AbortController();
      const signal = controller.signal;

      const taskId = uid();
      const now = new Date().toISOString();
      const mode = resolveGenerationMode(imageMode, options);
      const remoteTaskIds: number[] = [];

      const task: StudioGenerationTask = {
        id: taskId,
        prompt,
        mode,
        status: 'queued',
        createdAt: now,
        platform: selectedPlatform,
        model: selectedModelId,
        size: imageSize,
        remoteTaskIds: [],
      };

      setTasks(prev => [task, ...prev]);
      activeCountRef.current += 1;
      setIsGenerating(true);

      const updateTask = (patch: Partial<StudioGenerationTask>) => {
        setTasks(prev => prev.map(t => (t.id === taskId ? { ...t, ...patch } : t)));
      };

      const runTask = async () => {
        try {
          updateTask({ status: 'processing' });

          if (mode === 'batch') {
            const prompts = options?.prompts?.length
              ? options.prompts
              : Array.from({ length: options?.count ?? 4 }, () => prompt);

            const batchTasks = prompts.map(async (p) => {
              const created = await api.createGenerationTask({
                kind: 'image',
                operation: 'generate',
                platform: selectedPlatform,
                model: selectedModelId,
                prompt: p,
                parameters: imageSize ? { size: imageSize } : undefined,
              });
              remoteTaskIds.push(created.id);
              updateTask({ remoteTaskIds: [...remoteTaskIds] });
              const completed = await pollGenerationTask(created.id, signal);
              return parseMarkdownImages(completed.result_content || '').map(img => ({
                ...img,
                prompt: p,
                taskId: created.id,
                createdAt: taskAssetCreatedAt(completed),
              }));
            });

            const settled = await Promise.allSettled(batchTasks);

            const allItems: GalleryItem[] = [];
            for (const outcome of settled) {
              if (outcome.status === 'fulfilled') {
                for (const img of outcome.value) {
                  allItems.push({
                    id: uid(),
                    taskId: img.taskId,
                    url: img.url,
                    alt: img.alt,
                    prompt: img.prompt,
                    model: selectedModelId,
                    mode,
                    size: imageSize,
                    createdAt: img.createdAt,
                  });
                }
              }
            }

            if (allItems.length === 0) throw new Error('Batch generation: all tasks failed');

            setGallery(prev => [...allItems, ...prev]);
            updateTask({ status: 'completed', result: allItems, remoteTaskIds: [...remoteTaskIds] });

          } else {
            // text2img / img2img / inpaint — 统一走 task 系统
            const taskData: Parameters<typeof api.createGenerationTask>[0] = {
              kind: 'image',
              operation: modeToOperation(mode),
              platform: selectedPlatform,
              model: selectedModelId,
              prompt,
              parameters: imageSize ? { size: imageSize } : undefined,
            };

            if (mode === 'img2img' || mode === 'inpaint') {
              // Source priority: caller-passed sources > caller's single source
              // > accumulated gallery references. The reference list can hold
              // multiple URLs now, so img2img can fan out to them all.
              const sources = options?.sourceImages?.length
                ? options.sourceImages
                : options?.sourceImage
                ? [options.sourceImage]
                : referenceImages;
              if (sources.length === 0 && mode === 'inpaint') throw new Error('Inpaint requires a source image');
              if (sources.length > 0) {
                // 直接透传 source URL（data:、/assets-runtime/、http(s) 都行）。
                // core 的 normalizeTaskInputAssets 只对 data:image/* 大图落盘，已经是
                // URL 形式的会原样保留，避免"画廊 URL → 前端 fetch → data URI → 后端再落盘"
                // 的来回搬运。
                taskData.inputs = sources.map(url => ({ type: 'image' as const, role: 'source' as const, url }));
              }
            }

            if (mode === 'inpaint' && options?.maskRegion) {
              // Inpaint is single-source by API contract; use the first reference.
              const sourceUrl = options?.sourceImage ?? referenceImages[0] ?? '';
              taskData.mask = { type: 'image', role: 'mask', url: await createMaskDataUrl(sourceUrl, options.maskRegion) };
            }

            const created = await api.createGenerationTask(taskData);
            if (signal.aborted) throw new DOMException('Aborted', 'AbortError');
            updateTask({ remoteTaskIds: [created.id] });
            const completed = await pollGenerationTask(created.id, signal);
            const images = parseMarkdownImages(completed.result_content || '');

            const galleryItems: GalleryItem[] = images.map(img => ({
              id: uid(),
              taskId: created.id,
              url: img.url,
              alt: img.alt,
              prompt,
              model: selectedModelId,
              mode,
              size: imageSize,
              createdAt: taskAssetCreatedAt(completed),
              // GalleryItem.sourceUrl is single-valued; record the first source
              // so "regenerate" can seed at least one reference. Multi-ref recall
              // would need a schema change to GalleryItem.
              sourceUrl: (mode === 'img2img' || mode === 'inpaint')
                ? (options?.sourceImage ?? options?.sourceImages?.[0] ?? referenceImages[0] ?? undefined)
                : undefined,
            }));

            setGallery(prev => [...galleryItems, ...prev]);
            updateTask({ status: 'completed', result: galleryItems, remoteTaskIds: [created.id] });
          }
        } catch (err) {
          if (signal.aborted) {
            updateTask({ status: 'failed', error: 'Generation cancelled' });
          } else {
            const msg = err instanceof Error ? err.message : 'Generation failed';
            updateTask({ status: 'failed', error: msg });
          }
        } finally {
          activeCountRef.current -= 1;
          if (activeCountRef.current <= 0) {
            activeCountRef.current = 0;
            setIsGenerating(false);
          }
        }
      };

      void runTask();
    },
    [
      imageMode,
      imageSize,
      referenceImages,
      selectedPlatform,
      selectedModelId,
    ],
  );

  // ── Gallery helpers ───────────────────────────────────────────────────────

  const deleteTask = useCallback((uiId: string) => {
    const task = tasks.find(t => t.id === uiId);
    const remoteIds = uniqueNumbers([
      ...(task ? taskRemoteIds(task) : []),
      ...((uiId.startsWith('r-') ? [Number(uiId.slice(2))] : [])),
    ]);
    setTasks(prev => prev.filter(t => t.id !== uiId));
    if (remoteIds.length > 0) {
      setGallery(prev => prev.filter(item => !item.taskId || !remoteIds.includes(item.taskId)));
    }
    for (const remoteId of remoteIds) {
      api.deleteGenerationTask(remoteId).catch(() => {});
    }
  }, [tasks]);

  const deleteGalleryItem = useCallback((id: string) => {
    const item = gallery.find(g => g.id === id);
    if (!item) return;
    const matchingTask = item.taskId
      ? tasks.find(task => taskRemoteIds(task).includes(item.taskId!))
      : undefined;
    if (matchingTask) {
      deleteTask(matchingTask.id);
      return;
    }
    setGallery(prev => (item.taskId
      ? prev.filter(g => g.taskId !== item.taskId)
      : prev.filter(g => g.id !== id)));
    if (item.taskId) {
      api.deleteGenerationTask(item.taskId).catch(() => {});
    }
  }, [deleteTask, gallery, tasks]);

  const useAsReference = useCallback((item: GalleryItem) => {
    // Dedupe-append rather than replace so multiple gallery items accumulate.
    setReferenceImages(prev => prev.includes(item.url) ? prev : [...prev, item.url]);
    setImageMode('img2img');
  }, []);

  const regenerate = useCallback((item: GalleryItem) => {
    const mode = item.mode === 'batch' ? 'text2img' : item.mode;
    const sourceImage = item.sourceUrl ?? (mode === 'img2img' || mode === 'inpaint' ? item.url : undefined);
    setSelectedModelId(item.model);
    setImageMode(mode);
    if (item.size) setImageSize(item.size);
    // Regenerate resets references to the original source (one item only —
    // GalleryItem.sourceUrl can't carry multiple references today).
    setReferenceImages(sourceImage ? [sourceImage] : []);
    setTimeout(() => {
      generate(item.prompt, {
        mode,
        sourceImage,
      });
    }, 0);
  }, [generate, setSelectedModelId, setImageMode, setImageSize]);

  // ── Context value ─────────────────────────────────────────────────────────

  const value: StudioContextValue = {
    mediaType,
    setMediaType,
    imageMode,
    setImageMode,
    currentModel,
    selectedModelId,
    setSelectedModelId,
    selectedPlatform,
    imageSize,
    setImageSize,
    referenceImages,
    setReferenceImages,
    isGenerating,
    tasks,
    generate,
    cancelGeneration,
    gallery,
    hasMore,
    loadingMore,
    loadMore,
    generatedAssetRetentionDays,
    previewItem,
    setPreviewItem,
    deleteGalleryItem,
    deleteTask,
    useAsReference,
    regenerate,
  };

  return <StudioContext.Provider value={value}>{children}</StudioContext.Provider>;
}
