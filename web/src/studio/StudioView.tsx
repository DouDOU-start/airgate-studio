import { useCallback, useEffect, useRef, useState, type CSSProperties, type KeyboardEvent, type DragEvent, type ChangeEvent } from 'react';
import { cssVar, setTheme, getStoredTheme, type ThemeName } from '@doudou-start/airgate-theme';
import { StudioProvider, useStudio } from './StudioContext';
import { MaskEditor, type NormalizedRect } from './MaskEditor';
import { INSPIRATIONS } from './inspirations';
import { GalleryView } from './GalleryView';
import { studioStyles as ss, studioCSS } from './studioStyles';
import { SizeSelector } from './SizeSelector';
import { ModelPicker } from './ModelPicker';

// ── Helpers ─────────────────────────────────────────────────────────────────

function readFileAsDataURL(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(reader.result as string);
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(file);
  });
}

// ── InspirationSidebar ─────────────────────────────────────────────────────────

const tpl: Record<string, CSSProperties> = {
  sidebar: {
    height: '100%',
    overflowY: 'auto',
    overflowX: 'hidden',
    padding: '12px 14px 24px',
    display: 'flex',
    flexDirection: 'column',
    gap: 8,
    background: cssVar('bgDeep'),
  },
  headerRow: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '4px 4px 2px',
    gap: 8,
  },
  headerTitle: {
    fontSize: 12,
    fontWeight: 700,
    letterSpacing: '0.08em',
    textTransform: 'uppercase',
    color: cssVar('textSecondary'),
    fontFamily: cssVar('fontMono'),
    userSelect: 'none',
    whiteSpace: 'nowrap',
  },
  collapseBtn: {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 40,
    minWidth: 40,
    height: 40,
    border: 'none',
    borderRadius: cssVar('radiusSm'),
    background: 'transparent',
    color: cssVar('textSecondary'),
    cursor: 'pointer',
    padding: 0,
    transition: cssVar('transition'),
    flexShrink: 0,
  },
  collapsedStrip: {
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    gap: 12,
    width: '100%',
    height: '100%',
    padding: '4px 0 14px',
    border: 'none',
    borderRight: `1px solid ${cssVar('borderSubtle')}`,
    background: cssVar('bgDeep'),
    color: cssVar('textSecondary'),
    cursor: 'pointer',
    fontFamily: 'inherit',
    transition: cssVar('transition'),
  },
  collapsedStripIcon: {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 40,
    minWidth: 40,
    height: 40,
    borderRadius: cssVar('radiusSm'),
    color: cssVar('textSecondary'),
    transition: cssVar('transition'),
  },
  collapsedLabel: {
    fontSize: 10,
    fontWeight: 700,
    letterSpacing: '0.12em',
    writingMode: 'vertical-rl',
    textOrientation: 'mixed',
    fontFamily: cssVar('fontMono'),
    textTransform: 'uppercase',
  } as CSSProperties,
  catLabel: {
    fontSize: 10,
    fontWeight: 700,
    color: cssVar('textTertiary'),
    letterSpacing: '0.04em',
    padding: '8px 4px 6px',
    fontFamily: cssVar('fontMono'),
    opacity: 0.6,
  },
  grid: {
    columns: '160px',
    columnGap: 10,
  } as CSSProperties,
  card: {
    borderRadius: 10,
    overflow: 'hidden',
    cursor: 'pointer',
    border: `1px solid ${cssVar('borderSubtle')}`,
    boxShadow: '0 1px 4px rgba(0, 0, 0, 0.06)',
    transition: 'all 0.15s',
    background: cssVar('bgElevated'),
    breakInside: 'avoid',
    marginBottom: 10,
  } as CSSProperties,
  thumb: {
    width: '100%',
    display: 'block',
    objectFit: 'cover',
  },
  cardBottom: {
    padding: '5px 8px',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
  },
  cardLabel: {
    fontSize: 11,
    fontWeight: 600,
    color: cssVar('textSecondary'),
    letterSpacing: '0.01em',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
  useBtn: {
    fontSize: 10,
    color: cssVar('primary'),
    fontWeight: 600,
    flexShrink: 0,
    cursor: 'pointer',
  },
};

// ── TopNav (fixed global nav bar) ──────────────────────────────────────────

const floatNav: Record<string, CSSProperties> = {
  wrap: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '0 0 12px',
    flexShrink: 0,
  },
  btn: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 5,
    padding: '5px 10px',
    borderRadius: 8,
    border: `1px solid ${cssVar('borderSubtle')}`,
    background: 'transparent',
    color: cssVar('textTertiary'),
    fontSize: 11,
    fontWeight: 500,
    textDecoration: 'none',
    fontFamily: 'inherit',
    cursor: 'pointer',
    transition: 'all 0.15s',
  },
  iconBtn: {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 28,
    height: 28,
    borderRadius: 8,
    border: `1px solid ${cssVar('borderSubtle')}`,
    background: 'transparent',
    color: cssVar('textTertiary'),
    cursor: 'pointer',
    padding: 0,
    transition: 'all 0.15s',
  },
};

function InspirationSidebar({ onSelect, onCollapse }: { onSelect: (prompt: string) => void; onCollapse?: () => void }) {
  const categories = [...new Set(INSPIRATIONS.map(i => i.category))];
  const title = '灵感画廊';

  return (
    <div style={tpl.sidebar} className="studio-gallery">
      <div style={tpl.headerRow}>
        {/* 独立部署：原 core 壳层面包屑改为本地标题 */}
        <span style={tpl.headerTitle}>{title}</span>
        {onCollapse && (
          <button
            type="button"
            style={tpl.collapseBtn}
            className="studio-console-link studio-collapse-btn"
            onClick={onCollapse}
            title="收起灵感画廊"
          >
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round">
              <path d="M15 18l-6-6 6-6" />
            </svg>
          </button>
        )}
      </div>
      {categories.map(cat => (
        <div key={cat}>
          <div style={tpl.catLabel}>{cat}</div>
          <div style={tpl.grid}>
            {INSPIRATIONS.filter(i => i.category === cat).map(item => (
              <div
                key={item.title}
                style={tpl.card}
                className="studio-template-card"
                onClick={() => onSelect(item.prompt)}
                title={item.prompt.slice(0, 100) + '...'}
              >
                <img src={item.image} alt={item.title} style={tpl.thumb} loading="lazy" />
                <div style={tpl.cardBottom}>
                  <span style={tpl.cardLabel}>{item.title}</span>
                  <span style={tpl.useBtn}>使用</span>
                </div>
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

function CollapsedInspirationStrip({ onExpand }: { onExpand: () => void }) {
  return (
    <button
      type="button"
      onClick={onExpand}
      style={tpl.collapsedStrip}
      className="studio-collapsed-strip"
      title="展开灵感画廊"
    >
      <span style={tpl.collapsedStripIcon} className="studio-collapse-icon">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round">
          <path d="M9 18l6-6-6-6" />
        </svg>
      </span>
      <span style={tpl.collapsedLabel}>灵感画廊</span>
    </button>
  );
}

// ── ComposerBar ─────────────────────────────────────────────────────────────

const COUNT_OPTIONS = [1, 2, 3, 4];
const COMPOSER_TEXTAREA_HEIGHT = 112;

function ComposerBar({ promptRef }: { promptRef?: React.MutableRefObject<{ set: (v: string) => void } | null> }) {
  const {
    setImageMode,
    models,
    currentModel,
    selectedModelId, setSelectedModelId,
    imageSize, setImageSize,
    imageQuality, setImageQuality,
    generate,
    referenceImages, setReferenceImages,
  } = useStudio();

  const [prompt, setPrompt] = useState('');
  const [count, setCount] = useState(1);
  const [theme, setThemeState] = useState<ThemeName>(() => getStoredTheme());
  const toggleTheme = () => {
    const next: ThemeName = theme === 'dark' ? 'light' : 'dark';
    setTheme(next);
    document.documentElement.classList.toggle('light', next === 'light');
    document.documentElement.classList.toggle('dark', next === 'dark');
    setThemeState(next);
  };
  const [sourceImages, setSourceImages] = useState<string[]>([]);
  const [isDragging, setIsDragging] = useState(false);

  // mask state (only for single image → inpaint)
  const [selection, setSelection] = useState<NormalizedRect | null>(null);
  // Index into allSources for the thumbnail currently open in the preview/mask editor.
  // null when closed. Multi-image opens in preview-only mode (no mask drawing).
  const [editorIndex, setEditorIndex] = useState<number | null>(null);

  const fileInputRef = useRef<HTMLInputElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    if (promptRef) {
      promptRef.current = {
        set: (v: string) => { setPrompt(v); textareaRef.current?.focus(); },
      };
    }
  }, [promptRef]);

  // Union: composer uploads come first, then gallery "use as reference" picks.
  // Both can coexist now (previously gallery picks only showed when composer
  // was empty, which made it impossible to combine).
  const allSources = [...sourceImages, ...referenceImages];
  // 图生图能力按策略推导（imagen predict / dall-e-3 无图生图语义 → 不收输入图）
  const imageInputAllowed = currentModel.caps.supportsImageInput;
  const hasSource = imageInputAllowed && allSources.length > 0;
  const isSingleSource = imageInputAllowed && allSources.length === 1;
  const canSend = prompt.trim().length > 0 && selectedModelId.trim().length > 0;

  const handleSend = () => {
    const trimmed = prompt.trim();
    if (!trimmed) return;

    if (isSingleSource && selection) {
      setImageMode('inpaint');
      void generate(trimmed, { mode: 'inpaint', sourceImage: allSources[0], maskRegion: selection });
    } else if (hasSource) {
      setImageMode('img2img');
      for (let i = 0; i < count; i++) {
        void generate(trimmed, { mode: 'img2img', sourceImages: allSources, count: 1 });
      }
    } else {
      setImageMode('text2img');
      for (let i = 0; i < count; i++) {
        void generate(trimmed, { mode: 'text2img', count: 1 });
      }
    }
    setPrompt('');
  };

  const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  };

  const handleFile = useCallback(async (file: File) => {
    if (!file.type.startsWith('image/')) return;
    try {
      const dataUrl = await readFileAsDataURL(file);
      setSourceImages(prev => [...prev, dataUrl]);
      setSelection(null);
    } catch { /* ignore */ }
  }, []);

  const handleFileInput = (e: ChangeEvent<HTMLInputElement>) => {
    const files = e.target.files;
    if (files) {
      for (const file of files) void handleFile(file);
    }
    e.target.value = '';
  };

  const handleDragOver = (e: DragEvent) => { e.preventDefault(); setIsDragging(true); };
  const handleDragLeave = () => setIsDragging(false);
  const handleDrop = (e: DragEvent) => {
    e.preventDefault();
    setIsDragging(false);
    const files = e.dataTransfer.files;
    if (files) {
      for (const file of files) void handleFile(file);
    }
  };

  const handlePaste = useCallback((e: ClipboardEvent) => {
    const items = e.clipboardData?.items;
    if (!items) return;
    for (const item of items) {
      if (item.type.startsWith('image/')) {
        e.preventDefault();
        const file = item.getAsFile();
        if (file) void handleFile(file);
        return;
      }
    }
  }, [handleFile]);

  useEffect(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.addEventListener('paste', handlePaste as EventListener);
    return () => el.removeEventListener('paste', handlePaste as EventListener);
  }, [handlePaste]);

  const removeSource = (index: number) => {
    // Index addresses allSources = [...sourceImages, ...referenceImages].
    // Route the removal to the right backing array.
    if (index < sourceImages.length) {
      setSourceImages(prev => prev.filter((_, i) => i !== index));
    } else {
      const refIdx = index - sourceImages.length;
      setReferenceImages(referenceImages.filter((_, i) => i !== refIdx));
    }
    setSelection(null);
  };

  const clearAllSources = () => {
    setSourceImages([]);
    setReferenceImages([]);
    setSelection(null);
  };

  const placeholder = hasSource
    ? (isSingleSource && selection ? '描述要修改的区域...' : '描述你想要的变化...')
    : '描述你想生成的图片...';

  const modeHint = hasSource
    ? (isSingleSource && selection ? '局部绘图' : '图生图')
    : null;

  return (
    <div
      style={isDragging ? { ...c.card, ...c.cardDragging } : c.card}
      className="studio-quick-input"
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
    >
      {/* Source image thumbnails */}
      {hasSource && (
        <div style={c.sourceStrip}>
          {allSources.map((src, i) => (
            <div key={i} style={c.thumbWrap} onClick={() => setEditorIndex(i)}>
              <img
                src={src}
                alt="source"
                style={c.thumbImg}
              />
              {isSingleSource && selection && (
                <div
                  style={{
                    ...c.thumbMaskOverlay,
                    left: `${selection.x * 100}%`,
                    top: `${selection.y * 100}%`,
                    width: `max(${selection.width * 100}%, 10px)`,
                    height: `max(${selection.height * 100}%, 10px)`,
                  }}
                  title="已选区"
                />
              )}
            </div>
          ))}
          {allSources.length > 1 && (
            <button type="button" style={c.sourceActionBtn} className="studio-gallery-action" onClick={clearAllSources}>
              {'清除全部'}
            </button>
          )}
          {isSingleSource && selection && (
            <button type="button" style={c.sourceActionBtn} className="studio-gallery-action" onClick={() => setSelection(null)}>
              {'清除选区'}
            </button>
          )}
          {modeHint && <span style={c.modeHint}>{modeHint}</span>}
        </div>
      )}
      {editorIndex !== null && allSources[editorIndex] && (
        <MaskEditor
          src={allSources[editorIndex]}
          selection={isSingleSource ? selection : null}
          maskingEnabled={isSingleSource}
          onConfirm={(sel) => {
            if (isSingleSource) setSelection(sel);
            setEditorIndex(null);
          }}
          onClose={() => setEditorIndex(null)}
          onDelete={() => {
            removeSource(editorIndex);
            setEditorIndex(null);
          }}
        />
      )}
      <input ref={fileInputRef} type="file" accept="image/*" multiple style={{ display: 'none' }} onChange={handleFileInput} />

      {/* Prompt textarea */}
      <textarea
        ref={textareaRef}
        style={c.textarea}
        value={prompt}
        onChange={e => setPrompt(e.target.value)}
        onKeyDown={handleKeyDown}
        placeholder={placeholder}
        rows={5}
      />

      {/* Toolbar row */}
      <div style={c.toolbar}>
        <div style={c.toolbarLeft} className="studio-composer-toolbar-left">
          <div style={c.modelPicker}>
            <ModelPicker
              value={selectedModelId}
              models={models}
              onChange={setSelectedModelId}
              upward
              compact
            />
          </div>
          {/* 参数控件按模型能力显隐：size/quality 仅在原生生效的路径展示 */}
          {currentModel.caps.supportsSize && (
            <div style={c.sizePicker}>
              <SizeSelector value={imageSize} sizes={currentModel.caps.sizes} onChange={setImageSize} upward compact />
            </div>
          )}
          {currentModel.caps.supportsQuality && currentModel.caps.qualities.length > 0 && (
            <div style={c.countGroup} title={'质量'}>
              {currentModel.caps.qualities.map(q => (
                <button
                  key={q.value}
                  type="button"
                  style={imageQuality === q.value ? c.qualityBtnActive : c.qualityBtn}
                  onClick={() => setImageQuality(imageQuality === q.value ? '' : q.value)}
                >
                  {q.label}
                </button>
              ))}
            </div>
          )}
          {imageInputAllowed && (
            <button
              type="button"
              style={c.imgUploadBtn}
              className="studio-gallery-action"
              onClick={() => fileInputRef.current?.click()}
              title={'添加参考图'}
            >
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
                <rect x="3" y="3" width="18" height="18" rx="2" /><circle cx="8.5" cy="8.5" r="1.5" /><path d="M21 15l-5-5L5 21" />
              </svg>
            </button>
          )}
          <div style={c.countGroup}>
            {COUNT_OPTIONS.map(n => (
              <button
                key={n}
                type="button"
                style={count === n ? c.countBtnActive : c.countBtn}
                onClick={() => setCount(n)}
              >
                {n}
              </button>
            ))}
          </div>
          <button type="button" style={floatNav.iconBtn} className="studio-console-link" onClick={toggleTheme}>
            {theme === 'dark' ? (
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
                <circle cx="12" cy="12" r="5" /><line x1="12" y1="1" x2="12" y2="3" /><line x1="12" y1="21" x2="12" y2="23" /><line x1="4.22" y1="4.22" x2="5.64" y2="5.64" /><line x1="18.36" y1="18.36" x2="19.78" y2="19.78" /><line x1="1" y1="12" x2="3" y2="12" /><line x1="21" y1="12" x2="23" y2="12" /><line x1="4.22" y1="19.78" x2="5.64" y2="18.36" /><line x1="18.36" y1="5.64" x2="19.78" y2="4.22" />
              </svg>
            ) : (
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
                <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z" />
              </svg>
            )}
          </button>
        </div>
        <button
          type="button"
          style={{
            ...c.sendBtn,
            ...(canSend ? {} : c.sendBtnDisabled),
          }}
          className={canSend ? 'studio-send-btn' : ''}
          onClick={handleSend}
          disabled={!canSend}
        >
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
            <path d="M12 19V5" />
            <path d="M5 12l7-7 7 7" />
          </svg>
        </button>
      </div>
    </div>
  );
}

// ── ComposerBar styles ──────────────────────────────────────────────────────

const c: Record<string, CSSProperties> = {
  card: {
    width: '100%',
    maxWidth: 720,
    display: 'flex',
    flexDirection: 'column',
    gap: 0,
    padding: '6px 6px 10px',
    borderRadius: 20,
    background: cssVar('bgElevated'),
    border: `1px solid ${cssVar('glassBorder')}`,
    boxShadow: '0 8px 48px rgba(0, 0, 0, 0.4), 0 2px 12px rgba(0, 0, 0, 0.2)',
    transition: 'box-shadow 0.3s, border-color 0.15s',
  },
  cardDragging: {
    borderColor: cssVar('primary'),
    boxShadow: `0 0 0 2px ${cssVar('primaryGlow')}, 0 8px 48px rgba(0, 0, 0, 0.4)`,
  },
  sourceStrip: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    padding: '6px 12px 2px',
  },
  thumbWrap: {
    position: 'relative',
    borderRadius: 8,
    overflow: 'hidden',
    border: `1px solid ${cssVar('borderSubtle')}`,
    cursor: 'pointer',
    lineHeight: 0,
    flexShrink: 0,
  },
  thumbImg: {
    display: 'block',
    height: 48,
    width: 'auto',
    maxWidth: 100,
    objectFit: 'cover',
    pointerEvents: 'none',
  },
  thumbMaskOverlay: {
    position: 'absolute',
    borderRadius: 3,
    border: '2px solid rgba(248, 113, 113, 0.95)',
    background: 'rgba(248, 113, 113, 0.42)',
    boxShadow: '0 0 0 9999px rgba(0, 0, 0, 0.18), inset 0 0 0 1px rgba(255, 255, 255, 0.65), 0 0 12px rgba(248, 113, 113, 0.65)',
    boxSizing: 'border-box',
    pointerEvents: 'none',
  },
  sourceActionBtn: {
    padding: '3px 8px',
    border: `1px solid ${cssVar('borderSubtle')}`,
    borderRadius: 5,
    background: 'transparent',
    color: cssVar('textTertiary'),
    cursor: 'pointer',
    fontSize: 10,
    fontFamily: 'inherit',
    fontWeight: 500,
    transition: 'all 0.15s',
  },
  modeHint: {
    marginLeft: 'auto',
    fontSize: 10,
    color: cssVar('textTertiary'),
    fontFamily: cssVar('fontMono'),
    letterSpacing: '0.02em',
    opacity: 0.6,
  },
  textarea: {
    width: '100%',
    height: COMPOSER_TEXTAREA_HEIGHT,
    minHeight: COMPOSER_TEXTAREA_HEIGHT,
    maxHeight: COMPOSER_TEXTAREA_HEIGHT,
    padding: '8px 14px',
    border: 'none',
    background: 'transparent',
    color: cssVar('text'),
    fontSize: 14,
    fontFamily: 'inherit',
    resize: 'none',
    outline: 'none',
    lineHeight: 1.6,
    overflowY: 'auto',
    boxSizing: 'border-box',
  },
  toolbar: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 8,
    padding: '2px 8px 0',
  },
  toolbarLeft: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    flex: 1,
    minWidth: 0,
    overflow: 'hidden',
  },
  modelPicker: {
    flexShrink: 1,
    minWidth: 120,
    maxWidth: 240,
    width: 240,
  },
  sizePicker: {
    flexShrink: 0,
    width: 150,
  },
  countGroup: {
    display: 'flex',
    gap: 2,
    flexShrink: 0,
  },
  qualityBtn: {
    height: 26,
    padding: '0 8px',
    border: `1px solid ${cssVar('borderSubtle')}`,
    borderRadius: 6,
    background: 'transparent',
    color: cssVar('textSecondary'),
    cursor: 'pointer',
    fontSize: 11,
    fontFamily: 'inherit',
    transition: 'all 0.15s',
  },
  qualityBtnActive: {
    height: 26,
    padding: '0 8px',
    border: `1px solid color-mix(in oklab, ${cssVar('primary')} 40%, transparent)`,
    borderRadius: 6,
    background: cssVar('primarySubtle'),
    color: cssVar('text'),
    cursor: 'pointer',
    fontSize: 11,
    fontFamily: 'inherit',
    fontWeight: 700,
    transition: 'all 0.15s',
  },
  countBtn: {
    width: 26,
    height: 26,
    border: `1px solid ${cssVar('borderSubtle')}`,
    borderRadius: 6,
    background: 'transparent',
    color: cssVar('textSecondary'),
    cursor: 'pointer',
    fontSize: 11,
    fontFamily: 'inherit',
    fontVariantNumeric: 'tabular-nums',
    transition: 'all 0.15s',
    padding: 0,
  },
  countBtnActive: {
    width: 26,
    height: 26,
    border: `1px solid color-mix(in oklab, ${cssVar('primary')} 40%, transparent)`,
    borderRadius: 6,
    background: cssVar('primarySubtle'),
    color: cssVar('text'),
    cursor: 'pointer',
    fontSize: 11,
    fontFamily: 'inherit',
    fontWeight: 700,
    fontVariantNumeric: 'tabular-nums',
    transition: 'all 0.15s',
    padding: 0,
  },
  batchHint: {
    fontSize: 11,
    color: cssVar('textTertiary'),
    fontFamily: cssVar('fontMono'),
    whiteSpace: 'nowrap',
  },
  consoleLink: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 4,
    padding: '4px 8px',
    borderRadius: 6,
    color: cssVar('textTertiary'),
    fontSize: 11,
    fontWeight: 500,
    textDecoration: 'none',
    fontFamily: 'inherit',
    transition: 'color 0.15s',
    flexShrink: 0,
    whiteSpace: 'nowrap',
  },
  imgUploadBtn: {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 30,
    height: 26,
    border: `1px solid ${cssVar('borderSubtle')}`,
    borderRadius: 6,
    background: 'transparent',
    color: cssVar('textTertiary'),
    cursor: 'pointer',
    padding: 0,
    flexShrink: 0,
    transition: 'all 0.15s',
  },
  sendBtn: {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 34,
    height: 34,
    border: 'none',
    borderRadius: 10,
    background: cssVar('primary'),
    color: cssVar('primaryForeground'),
    cursor: 'pointer',
    flexShrink: 0,
    padding: 0,
    transition: 'all 0.2s',
    boxShadow: `0 0 12px ${cssVar('primaryGlow')}`,
  },
  sendBtnDisabled: {
    background: cssVar('bgHover'),
    color: cssVar('textTertiary'),
    cursor: 'not-allowed',
    boxShadow: 'none',
    opacity: 0.4,
  },
};

// ── Landing ─────────────────────────────────────────────────────────────────

const landing: Record<string, CSSProperties> = {
  wrapper: {
    flex: 1,
    display: 'flex',
    flexDirection: 'column',
    background: cssVar('bgDeep'),
    overflow: 'hidden',
  },
  center: {
    flex: 1,
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 14,
    padding: '40px 32px 0',
    userSelect: 'none',
  },
  iconWrap: {
    width: 88,
    height: 88,
    borderRadius: 24,
    background: 'radial-gradient(circle at 40% 35%, rgba(255,255,255,0.05) 0%, rgba(255,255,255,0.01) 70%, transparent 100%)',
    border: '1px solid rgba(255,255,255,0.06)',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    boxShadow: '0 8px 32px rgba(0,0,0,0.12), inset 0 1px 0 rgba(255,255,255,0.04)',
    marginBottom: 4,
  },
  title: {
    fontSize: 22,
    fontWeight: 700,
    color: cssVar('text'),
    letterSpacing: '-0.02em',
  },
  subtitle: {
    fontSize: 13,
    color: cssVar('textTertiary'),
    opacity: 0.5,
  },
  bottom: {
    flexShrink: 0,
    display: 'flex',
    justifyContent: 'center',
    padding: '24px 24px 32px',
  },
};

// ── Gallery mode ────────────────────────────────────────────────────────────

const galleryLayout: Record<string, CSSProperties> = {
  wrapper: {
    flex: 1,
    display: 'flex',
    flexDirection: 'column',
    background: cssVar('bgElevated'),
    overflow: 'hidden',
  },
  composerWrap: {
    flexShrink: 0,
    padding: '12px 20px 16px',
    display: 'flex',
    justifyContent: 'center',
    background: cssVar('bgElevated'),
  },
};

// ── StudioLayout ────────────────────────────────────────────────────────────

const mobileTabStyle: Record<string, CSSProperties> = {
  bar: {
    display: 'none',
    gap: 0,
    borderBottom: `1px solid ${cssVar('borderSubtle')}`,
    background: cssVar('bgDeep'),
    flexShrink: 0,
  },
  tab: {
    flex: 1,
    padding: '10px 0',
    border: 'none',
    background: 'transparent',
    color: cssVar('textTertiary'),
    fontSize: 12,
    fontWeight: 600,
    cursor: 'pointer',
    fontFamily: 'inherit',
    textAlign: 'center',
    transition: 'all 0.15s',
  },
  tabActive: {
    flex: 1,
    padding: '10px 0',
    border: 'none',
    borderBottom: `2px solid ${cssVar('primary')}`,
    background: 'transparent',
    color: cssVar('text'),
    fontSize: 12,
    fontWeight: 700,
    cursor: 'pointer',
    fontFamily: 'inherit',
    textAlign: 'center',
  },
};

const GALLERY_COLLAPSE_KEY = 'airgate-studio-gallery-collapsed';

function StudioLayout() {
  const { gallery, tasks } = useStudio();
  const promptRef = useRef<{ set: (v: string) => void } | null>(null);
  const [mobileTab, setMobileTab] = useState<'inspiration' | 'create'>('create');
  const [collapsed, setCollapsed] = useState<boolean>(() => {
    try { return localStorage.getItem(GALLERY_COLLAPSE_KEY) === '1'; } catch { return false; }
  });

  const toggleCollapsed = () => {
    setCollapsed(prev => {
      const next = !prev;
      try { localStorage.setItem(GALLERY_COLLAPSE_KEY, next ? '1' : '0'); } catch { /* ignore */ }
      return next;
    });
  };

  const visibleTasks = tasks.filter(tk => tk.status !== 'completed');
  const isEmpty = gallery.length === 0 && visibleTasks.length === 0;

  const handleTemplate = (prompt: string) => {
    promptRef.current?.set(prompt);
    setMobileTab('create');
  };

  const mobileTabs = (
    <div style={mobileTabStyle.bar} className="studio-mobile-tabs">
      <button type="button" style={mobileTab === 'inspiration' ? mobileTabStyle.tabActive : mobileTabStyle.tab} onClick={() => setMobileTab('inspiration')}>灵感</button>
      <button type="button" style={mobileTab === 'create' ? mobileTabStyle.tabActive : mobileTabStyle.tab} onClick={() => setMobileTab('create')}>创作</button>
    </div>
  );

  const inspirationPanel = (
    <div
      className="studio-panel-inspiration"
      data-collapsed={collapsed ? 'true' : 'false'}
      style={{ minWidth: 0, overflow: 'hidden' }}
    >
      <div className="studio-inspiration-content" style={{ width: '100%', height: '100%' }}>
        <InspirationSidebar onSelect={handleTemplate} onCollapse={toggleCollapsed} />
      </div>
      <div className="studio-inspiration-strip" style={{ width: '100%', height: '100%' }}>
        <CollapsedInspirationStrip onExpand={toggleCollapsed} />
      </div>
    </div>
  );

  if (isEmpty) {
    return (
      <div style={ss.layout} data-mobile-tab={mobileTab}>
        <style>{studioCSS}</style>
        {mobileTabs}
        {inspirationPanel}
        <div className="studio-panel-create" style={{ flex: 1, display: 'flex', flexDirection: 'column', minWidth: 0, background: cssVar('bgElevated'), overflow: 'hidden' } as CSSProperties}>
          <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', overflow: 'hidden' }}>
            <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 14, padding: '0 32px', userSelect: 'none' } as CSSProperties}>
              <div style={landing.iconWrap}>
                <svg width="36" height="36" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1" strokeLinecap="round" strokeLinejoin="round" style={{ opacity: 0.2 }}>
                  <rect x="3" y="3" width="18" height="18" rx="2" />
                  <circle cx="8.5" cy="8.5" r="1.5" />
                  <path d="M21 15l-5-5L5 21" />
                </svg>
              </div>
              <div style={landing.title}>创作中心</div>
              <div style={landing.subtitle}>输入提示词，AI 为你生成图片</div>
              <div style={{ width: '100%', maxWidth: 720, marginTop: 16 }}>
                <ComposerBar promptRef={promptRef} />
              </div>
            </div>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div style={ss.layout} data-mobile-tab={mobileTab}>
      <style>{studioCSS}</style>
      {mobileTabs}
      {inspirationPanel}
      <div className="studio-panel-create" style={{ ...galleryLayout.wrapper, flex: 1, minWidth: 0 }}>
        <GalleryView />
        <div style={galleryLayout.composerWrap}>
          <ComposerBar promptRef={promptRef} />
        </div>
      </div>
    </div>
  );
}

// ── StudioView (entry point) ────────────────────────────────────────────────

export function StudioView() {
  return (
    <StudioProvider>
      <StudioLayout />
    </StudioProvider>
  );
}
