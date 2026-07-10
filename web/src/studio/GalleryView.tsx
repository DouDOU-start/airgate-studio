import { type CSSProperties, useCallback, useEffect, useRef, useState } from 'react';
import { cssVar } from '@doudou-start/airgate-theme';
import { useStudio } from './StudioContext';
import type { GalleryItem, StudioGenerationTask } from './types';
import { studioStyles as ss } from './studioStyles';
import { downloadImage } from '../lib/utils';

function useNearViewport(rootMargin = '600px', estimatedHeight = 0) {
  const ref = useRef<HTMLDivElement>(null);
  const [near, setNear] = useState(false);
  const heightRef = useRef<number>(0);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting) {
          setNear(true);
        } else {
          if (el.offsetHeight > 0) heightRef.current = el.offsetHeight;
          setNear(false);
        }
      },
      { rootMargin },
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, [rootMargin]);

  // Real measurement wins; estimate is only a fallback so the off-screen slot
  // has a reservable height before the card has rendered once.
  const placeholderHeight = heightRef.current || estimatedHeight;
  return { ref, near, placeholderHeight };
}

function formatCreatedAt(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const pad = (n: number): string => String(n).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

// Parse "1024x1024" → 1, "1024x768" → 0.75. Returns undefined if unparseable
// so callers can fall back to letting the image define its own aspect ratio.
function parseAspectRatio(size: string | undefined): number | undefined {
  if (!size) return undefined;
  const m = /^(\d+)x(\d+)$/.exec(size);
  if (!m) return undefined;
  const w = Number(m[1]);
  const h = Number(m[2]);
  if (!w || !h) return undefined;
  return w / h;
}

function useCopyOnClick(text: string | undefined | null) {
  const [copied, setCopied] = useState(false);
  const timerRef = useRef<number | null>(null);

  useEffect(() => () => {
    if (timerRef.current !== null) window.clearTimeout(timerRef.current);
  }, []);

  const copy = useCallback(async (e: React.MouseEvent) => {
    if (!text) return;
    e.stopPropagation();
    try {
      await navigator.clipboard.writeText(text);
    } catch {
      // Fallback for non-secure contexts where Clipboard API is unavailable.
      const ta = document.createElement('textarea');
      ta.value = text;
      ta.style.position = 'fixed';
      ta.style.opacity = '0';
      document.body.appendChild(ta);
      ta.select();
      try { document.execCommand('copy'); } catch { /* ignore */ }
      document.body.removeChild(ta);
    }
    setCopied(true);
    if (timerRef.current !== null) window.clearTimeout(timerRef.current);
    timerRef.current = window.setTimeout(() => setCopied(false), 1500);
  }, [text]);

  return { copied, copy };
}

// ── TaskCard ────────────────────────────────────────────────────────────────

const taskCardStyles: Record<string, CSSProperties> = {
  card: {
    position: 'relative',
    borderRadius: 14,
    overflow: 'hidden',
    background: cssVar('bgElevated'),
    border: `1px solid ${cssVar('borderSubtle')}`,
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 10,
    padding: 16,
    marginBottom: 14,
    breakInside: 'avoid',
    backdropFilter: 'blur(8px)',
    WebkitBackdropFilter: 'blur(8px)',
  } as CSSProperties,
  spinner: {
    width: 32,
    height: 32,
    border: `2px solid ${cssVar('borderSubtle')}`,
    borderTopColor: cssVar('textSecondary'),
    borderRadius: '50%',
    animation: 'spin 0.8s linear infinite',
    flexShrink: 0,
  },
  failedIcon: {
    width: 32,
    height: 32,
    borderRadius: '50%',
    border: `2px solid ${cssVar('dangerSubtle')}`,
    color: cssVar('danger'),
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    fontSize: 18,
    fontWeight: 700,
    flexShrink: 0,
  },
  prompt: {
    fontSize: 11,
    color: cssVar('textTertiary'),
    textAlign: 'center',
    lineHeight: 1.45,
    overflow: 'hidden',
    display: '-webkit-box',
    WebkitLineClamp: 3,
    WebkitBoxOrient: 'vertical',
  },
  statusLabel: {
    fontSize: 9,
    fontWeight: 700,
    letterSpacing: '0.1em',
    textTransform: 'uppercase',
    color: cssVar('textTertiary'),
    fontFamily: cssVar('fontMono'),
  },
  errorText: {
    fontSize: 10,
    color: cssVar('danger'),
    textAlign: 'center',
    lineHeight: 1.45,
    overflow: 'hidden',
    display: '-webkit-box',
    WebkitLineClamp: 2,
    WebkitBoxOrient: 'vertical',
    opacity: 0.85,
  },
  failedActions: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    marginTop: 4,
  },
  retryBtn: {
    padding: '4px 12px',
    border: `1px solid ${cssVar('borderSubtle')}`,
    borderRadius: 6,
    background: 'transparent',
    color: cssVar('textSecondary'),
    fontSize: 10,
    fontWeight: 500,
    cursor: 'pointer',
    fontFamily: 'inherit',
    transition: 'all 0.15s',
  },
  deleteBtn: {
    padding: '4px 12px',
    border: `1px solid ${cssVar('dangerSubtle')}`,
    borderRadius: 6,
    background: 'transparent',
    color: cssVar('danger'),
    fontSize: 10,
    fontWeight: 500,
    cursor: 'pointer',
    fontFamily: 'inherit',
    transition: 'all 0.15s',
  },
};

function TaskCard({ task }: { task: StudioGenerationTask }) {
  const { deleteTask, generate, setSelectedModelId, setImageSize, setImageMode } = useStudio();
  const { copied, copy } = useCopyOnClick(task.prompt);

  const statusLabel = task.status === 'queued'
    ? '队列中...'
    : task.status === 'failed'
      ? '生成失败'
      : '生成中...';

  const handleRetry = () => {
    if (!task.prompt) return;
    deleteTask(task.id);
    if (task.model) setSelectedModelId(task.model);
    if (task.size) setImageSize(task.size);
    setImageMode(task.mode);
    setTimeout(() => generate(task.prompt, { mode: task.mode }), 0);
  };

  const handleDelete = async () => {
    if (!window.confirm('确定要删除这个任务吗？')) return;
    deleteTask(task.id);
  };

  return (
    <div style={taskCardStyles.card}>
      {task.status === 'failed' ? (
        <div style={taskCardStyles.failedIcon}>!</div>
      ) : (
        <div style={taskCardStyles.spinner} />
      )}
      <div style={taskCardStyles.statusLabel}>{statusLabel}</div>
      {task.status === 'failed' && task.error && (
        <div style={taskCardStyles.errorText}>{task.error}</div>
      )}
      {task.prompt && (
        <div
          style={{
            ...taskCardStyles.prompt,
            cursor: 'pointer',
            color: copied ? cssVar('primary') : taskCardStyles.prompt.color,
            transition: 'color 0.2s',
          }}
          onClick={copy}
          title={copied ? '已复制到剪贴板' : '点击复制提示词'}
        >
          {copied ? '✓ 已复制' : task.prompt}
        </div>
      )}
      {task.status === 'failed' && (
        <div style={taskCardStyles.failedActions}>
          <button
            type="button"
            style={taskCardStyles.retryBtn}
            className="studio-gallery-action"
            onClick={handleRetry}
          >
            {'重试'}
          </button>
          <button
            type="button"
            style={taskCardStyles.deleteBtn}
            className="studio-gallery-action"
            onClick={handleDelete}
          >
            {'删除'}
          </button>
        </div>
      )}
    </div>
  );
}

// ── GalleryCard ─────────────────────────────────────────────────────────────

const GALLERY_COL_WIDTH = 200;
const GALLERY_OVERLAY_HEIGHT = 104;

function GalleryCard({ item, index }: { item: GalleryItem; index: number }) {
  const { setPreviewItem, deleteGalleryItem, useAsReference, regenerate } = useStudio();
  const { copied, copy } = useCopyOnClick(item.prompt);
  const aspectRatio = parseAspectRatio(item.size);
  const createdAtLabel = formatCreatedAt(item.createdAt);
  const estimatedHeight = aspectRatio
    ? Math.round(GALLERY_COL_WIDTH / aspectRatio) + GALLERY_OVERLAY_HEIGHT
    : 0;
  const { ref, near, placeholderHeight } = useNearViewport('800px', estimatedHeight);

  const handleDownload = (e: React.MouseEvent) => {
    e.stopPropagation();
    void downloadImage(item.url, item.prompt);
  };

  const handleRegenerate = async (e: React.MouseEvent) => {
    e.stopPropagation();
    if (!window.confirm('确定要重新生成吗？将消耗一次生成额度。')) return;
    regenerate(item);
  };

  const handleUseAsReference = (e: React.MouseEvent) => {
    e.stopPropagation();
    useAsReference(item);
  };

  const handleDelete = async (e: React.MouseEvent) => {
    e.stopPropagation();
    if (!window.confirm('确定要删除这张图片吗？')) return;
    deleteGalleryItem(item.id);
  };

  if (!near && placeholderHeight > 0) {
    return (
      <div
        ref={ref}
        style={{
          ...ss.galleryCard,
          height: placeholderHeight,
          background: cssVar('bgElevated'),
        }}
        className="studio-gallery-card"
      />
    );
  }

  return (
    <div
      ref={ref}
      style={{ ...ss.galleryCard, animationDelay: `${Math.min(index * 50, 300)}ms` }}
      className="studio-gallery-card"
    >
      <img
        src={item.url}
        alt={item.prompt}
        style={aspectRatio !== undefined ? { ...ss.galleryCardImg, aspectRatio: String(aspectRatio) } : ss.galleryCardImg}
        loading="lazy"
        decoding="async"
        fetchPriority="low"
        onClick={() => setPreviewItem(item)}
      />
      <div style={ss.galleryCardOverlay}>
        <div style={ss.galleryCardMetaRow}>
          {item.size && (
            <span style={ss.galleryCardMetaItem}>{item.size}</span>
          )}
          <span style={ss.galleryCardMetaItem}>创建于 {createdAtLabel}</span>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
          {item.prompt && (
            <div
              style={{
                ...ss.galleryCardPrompt,
                flex: 1,
                minWidth: 0,
                cursor: 'pointer',
                color: copied ? cssVar('primary') : ss.galleryCardPrompt.color,
                transition: 'color 0.2s',
              }}
              onClick={copy}
              title={copied ? '已复制到剪贴板' : '点击复制提示词'}
            >
              {copied ? '✓ 已复制' : item.prompt}
            </div>
          )}
        </div>
        <div style={ss.galleryCardActions}>
          <button
            type="button"
            style={ss.galleryCardActionBtn}
            className="studio-gallery-action"
            onClick={handleDownload}
            title={'下载'}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
              <polyline points="7 10 12 15 17 10" />
              <line x1="12" y1="15" x2="12" y2="3" />
            </svg>
          </button>
          <button
            type="button"
            style={ss.galleryCardActionBtn}
            className="studio-gallery-action"
            onClick={handleRegenerate}
            title={'重试'}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M1 4v6h6" />
              <path d="M3.51 15a9 9 0 1 0 2.13-9.36L1 10" />
            </svg>
          </button>
          <button
            type="button"
            style={ss.galleryCardActionBtn}
            className="studio-gallery-action"
            onClick={handleUseAsReference}
            title={'参考图'}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M16 3h5v5" />
              <path d="M21 3l-7 7" />
              <path d="M8 21H3v-5" />
              <path d="M3 21l7-7" />
            </svg>
          </button>
          <button
            type="button"
            style={ss.galleryCardActionBtn}
            className="studio-gallery-action"
            onClick={handleDelete}
            title={'删除'}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M3 6h18" />
              <path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
              <path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" />
            </svg>
          </button>
        </div>
      </div>
    </div>
  );
}

// ── PreviewOverlay ──────────────────────────────────────────────────────────

function PreviewOverlay() {
  const { previewItem, setPreviewItem } = useStudio();

  useEffect(() => {
    if (!previewItem) return;
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setPreviewItem(null);
    };
    window.addEventListener('keydown', handleKey);
    return () => window.removeEventListener('keydown', handleKey);
  }, [previewItem, setPreviewItem]);

  if (!previewItem) return null;

  return (
    <div style={ss.previewOverlay} onClick={() => setPreviewItem(null)}>
      <button
        type="button"
        style={ss.previewCloseBtn}
        className="studio-preview-close"
        onClick={() => setPreviewItem(null)}
      >
        ×
      </button>
      <img
        src={previewItem.url}
        alt={previewItem.prompt}
        style={ss.previewOverlayImg}
        onClick={e => e.stopPropagation()}
      />
    </div>
  );
}

// ── EmptyState ──────────────────────────────────────────────────────────────

const emptyStyles: Record<string, CSSProperties> = {
  wrapper: {
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 20,
    height: '100%',
    minHeight: 400,
    userSelect: 'none',
    paddingBottom: 80,
  },
  iconWrap: {
    width: 120,
    height: 120,
    borderRadius: 32,
    background: `radial-gradient(circle at 40% 35%, rgba(255,255,255,0.04) 0%, rgba(255,255,255,0.01) 70%, transparent 100%)`,
    border: '1px solid rgba(255, 255, 255, 0.06)',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    position: 'relative',
    boxShadow: '0 8px 32px rgba(0,0,0,0.15), inset 0 1px 0 rgba(255,255,255,0.04)',
  },
  title: {
    fontSize: 16,
    fontWeight: 600,
    color: cssVar('textSecondary'),
    letterSpacing: '-0.01em',
  },
  hint: {
    fontSize: 13,
    marginTop: 2,
    color: cssVar('textTertiary'),
    opacity: 0.5,
    fontFamily: cssVar('fontMono'),
    letterSpacing: '0.02em',
  },
  shortcutRow: {
    display: 'flex',
    gap: 16,
    marginTop: 8,
  },
  shortcutItem: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    fontSize: 11,
    color: cssVar('textTertiary'),
    opacity: 0.4,
    fontFamily: cssVar('fontMono'),
  },
  kbd: {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    minWidth: 20,
    height: 20,
    padding: '0 5px',
    borderRadius: 5,
    background: 'rgba(255,255,255,0.06)',
    border: '1px solid rgba(255,255,255,0.08)',
    fontSize: 10,
    fontWeight: 600,
    fontFamily: cssVar('fontMono'),
    color: cssVar('textTertiary'),
  },
};

function EmptyState() {
  return (
    <div style={emptyStyles.wrapper}>
      <div style={emptyStyles.iconWrap}>
        <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1" strokeLinecap="round" strokeLinejoin="round" style={{ opacity: 0.2 }}>
          <rect x="3" y="3" width="18" height="18" rx="2" />
          <circle cx="8.5" cy="8.5" r="1.5" />
          <path d="M21 15l-5-5L5 21" />
        </svg>
      </div>
      <div style={emptyStyles.title}>{'还没有生成的图片'}</div>
      <div style={emptyStyles.hint}>
        {'在下方输入框输入提示词，开始创作'}
      </div>
      <div style={emptyStyles.shortcutRow}>
        <div style={emptyStyles.shortcutItem}>
          <span style={emptyStyles.kbd}>Enter</span>
          <span>{'发送'}</span>
        </div>
        <div style={emptyStyles.shortcutItem}>
          <span style={emptyStyles.kbd}>Shift</span>
          <span>+</span>
          <span style={emptyStyles.kbd}>Enter</span>
          <span>{'换行'}</span>
        </div>
      </div>
    </div>
  );
}

// ── GalleryView ─────────────────────────────────────────────────────────────

export function GalleryView() {
  const { gallery, tasks, previewItem, hasMore, loadingMore, loadMore } = useStudio();
  const scrollRef = useRef<HTMLDivElement>(null);

  const handleScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el || loadingMore || !hasMore) return;
    if (el.scrollTop + el.clientHeight >= el.scrollHeight - 300) {
      loadMore();
    }
  }, [loadMore, loadingMore, hasMore]);

  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    el.addEventListener('scroll', handleScroll, { passive: true });
    return () => el.removeEventListener('scroll', handleScroll);
  }, [handleScroll]);

  const visibleTasks = tasks.filter(t => t.status !== 'completed');
  const isEmpty = gallery.length === 0 && visibleTasks.length === 0;

  return (
    <div ref={scrollRef} style={ss.gallery} className="studio-gallery">
      {previewItem && <PreviewOverlay />}

      {isEmpty ? (
        <EmptyState />
      ) : (
        <div style={ss.galleryGrid}>
          {visibleTasks.map(task => (
            <TaskCard key={task.id} task={task} />
          ))}
          {gallery.map((item, i) => (
            <GalleryCard key={item.id} item={item} index={i} />
          ))}
        </div>
      )}
      {loadingMore && (
        <div style={{ textAlign: 'center', padding: '16px 0', color: cssVar('textTertiary'), fontSize: 12 }}>加载中...</div>
      )}
    </div>
  );
}
