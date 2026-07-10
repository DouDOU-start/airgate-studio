import {
  useRef,
  useState,
  useCallback,
  type CSSProperties,
  type ChangeEvent,
  type DragEvent,
  type MouseEvent as ReactMouseEvent,
} from 'react';
import { useTranslation } from 'react-i18next';
import { cssVar } from '@doudou-start/airgate-theme';
import { useStudio } from '../StudioContext';
import { SizeSelector } from '../SizeSelector';
import { ModelPicker } from '../ModelPicker';
import { studioStyles as ss } from '../studioStyles';

interface NormalizedRect {
  x: number;
  y: number;
  width: number;
  height: number;
}

interface DragState {
  startX: number;
  startY: number;
}

const local: Record<string, CSSProperties> = {
  canvasContainer: {
    position: 'relative',
    display: 'inline-block',
    alignSelf: 'flex-start',
    maxWidth: '100%',
    borderRadius: 10,
    overflow: 'hidden',
    border: `1px solid ${cssVar('borderSubtle')}`,
    cursor: 'crosshair',
    userSelect: 'none',
    lineHeight: 0,
  },
  sourceImg: {
    display: 'block',
    width: 'auto',
    height: 'auto',
    maxWidth: '100%',
    maxHeight: 200,
    pointerEvents: 'none',
  },
  selectionRect: {
    position: 'absolute',
    border: '2px solid rgba(248, 113, 113, 0.95)',
    background: 'rgba(248, 113, 113, 0.32)',
    boxShadow: '0 0 0 9999px rgba(0, 0, 0, 0.22), inset 0 0 0 1px rgba(255, 255, 255, 0.65), 0 0 18px rgba(248, 113, 113, 0.45)',
    boxSizing: 'border-box',
    pointerEvents: 'none',
    borderRadius: 4,
  },
  canvasActions: {
    display: 'flex',
    gap: 6,
    marginTop: 6,
  },
  actionBtn: {
    padding: '6px 12px',
    border: `1px solid ${cssVar('borderSubtle')}`,
    borderRadius: 8,
    background: 'transparent',
    color: cssVar('textSecondary'),
    cursor: 'pointer',
    fontSize: 11,
    fontFamily: 'inherit',
    fontWeight: 500,
    transition: 'all 0.15s',
  },
  selectionHint: {
    fontSize: 10,
    color: cssVar('textTertiary'),
    marginTop: 4,
    fontFamily: cssVar('fontMono'),
    letterSpacing: '0.02em',
  },
};

function readFileAsDataURL(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(reader.result as string);
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(file);
  });
}

function normalizeRect(
  startX: number,
  startY: number,
  endX: number,
  endY: number,
  containerW: number,
  containerH: number,
): NormalizedRect {
  if (containerW <= 0 || containerH <= 0) {
    return { x: 0, y: 0, width: 0, height: 0 };
  }
  const clamp = (value: number, max: number) => Math.max(0, Math.min(max, value));
  const x1 = clamp(startX, containerW);
  const y1 = clamp(startY, containerH);
  const x2 = clamp(endX, containerW);
  const y2 = clamp(endY, containerH);
  return {
    x: Math.min(x1, x2) / containerW,
    y: Math.min(y1, y2) / containerH,
    width: Math.abs(x2 - x1) / containerW,
    height: Math.abs(y2 - y1) / containerH,
  };
}

export function InpaintPanel() {
  const { t } = useTranslation();
  const {
    models,
    currentModel,
    selectedModelId,
    setSelectedModelId,
    imageSize,
    setImageSize,
    isGenerating,
    generate,
  } = useStudio();

  const [prompt, setPrompt] = useState('');
  const [sourceImage, setSourceImage] = useState<string | null>(null);
  const [isDragging, setIsDragging] = useState(false);
  const [selection, setSelection] = useState<NormalizedRect | null>(null);
  const [dragState, setDragState] = useState<DragState | null>(null);
  const [liveRect, setLiveRect] = useState<{ x: number; y: number; w: number; h: number } | null>(null);

  const fileInputRef = useRef<HTMLInputElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const imgRef = useRef<HTMLImageElement>(null);

  const canGenerate = prompt.trim().length > 0 && sourceImage !== null;

  const handleFile = async (file: File) => {
    if (!file.type.startsWith('image/')) return;
    try {
      const dataUrl = await readFileAsDataURL(file);
      setSourceImage(dataUrl);
      setSelection(null);
    } catch { /* ignore */ }
  };

  const handleFileInputChange = (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (file) void handleFile(file);
    e.target.value = '';
  };

  const handleDropzoneDragOver = (e: DragEvent<HTMLDivElement>) => {
    e.preventDefault();
    setIsDragging(true);
  };
  const handleDropzoneDragLeave = () => setIsDragging(false);
  const handleDropzoneDrop = (e: DragEvent<HTMLDivElement>) => {
    e.preventDefault();
    setIsDragging(false);
    const file = e.dataTransfer.files?.[0];
    if (file) void handleFile(file);
  };

  const getImageMetrics = useCallback(() => {
    const container = containerRef.current;
    const img = imgRef.current;
    if (!container || !img) return null;
    const containerRect = container.getBoundingClientRect();
    const imageRect = img.getBoundingClientRect();
    const originLeft = containerRect.left + container.clientLeft;
    const originTop = containerRect.top + container.clientTop;
    return {
      offsetX: imageRect.left - originLeft,
      offsetY: imageRect.top - originTop,
      width: imageRect.width,
      height: imageRect.height,
      imageRect,
    };
  }, []);

  const getRelativePos = useCallback((e: ReactMouseEvent): { x: number; y: number } | null => {
    const metrics = getImageMetrics();
    if (!metrics || metrics.width <= 0 || metrics.height <= 0) return null;
    const clamp = (value: number, max: number) => Math.max(0, Math.min(max, value));
    return {
      x: clamp(e.clientX - metrics.imageRect.left, metrics.width),
      y: clamp(e.clientY - metrics.imageRect.top, metrics.height),
    };
  }, [getImageMetrics]);

  const toContainerRect = useCallback((rect: { x: number; y: number; w: number; h: number }) => {
    const metrics = getImageMetrics();
    if (!metrics) return null;
    return {
      x: metrics.offsetX + rect.x,
      y: metrics.offsetY + rect.y,
      width: rect.w,
      height: rect.h,
    };
  }, [getImageMetrics]);

  const handleMouseDown = useCallback((e: ReactMouseEvent<HTMLDivElement>) => {
    if (!sourceImage) return;
    const pos = getRelativePos(e);
    if (!pos) return;
    e.preventDefault();
    setDragState({ startX: pos.x, startY: pos.y });
    setLiveRect({ x: pos.x, y: pos.y, w: 0, h: 0 });
    setSelection(null);
  }, [sourceImage, getRelativePos]);

  const handleMouseMove = useCallback((e: ReactMouseEvent<HTMLDivElement>) => {
    if (!dragState) return;
    const pos = getRelativePos(e);
    if (!pos) return;
    setLiveRect({
      x: Math.min(dragState.startX, pos.x),
      y: Math.min(dragState.startY, pos.y),
      w: Math.abs(pos.x - dragState.startX),
      h: Math.abs(pos.y - dragState.startY),
    });
  }, [dragState, getRelativePos]);

  const handleMouseUp = useCallback((e: ReactMouseEvent<HTMLDivElement>) => {
    if (!dragState) return;
    const pos = getRelativePos(e);
    const metrics = getImageMetrics();
    if (!pos || !metrics) {
      setDragState(null);
      setLiveRect(null);
      return;
    }
    const norm = normalizeRect(dragState.startX, dragState.startY, pos.x, pos.y, metrics.width, metrics.height);
    if (norm.width > 0.01 && norm.height > 0.01) {
      setSelection(norm);
    }
    setDragState(null);
    setLiveRect(null);
  }, [dragState, getImageMetrics, getRelativePos]);

  const renderSelectionOverlay = () => {
    const rect = liveRect
      ? toContainerRect(liveRect)
      : selection
        ? (() => {
            const metrics = getImageMetrics();
            if (!metrics) return null;
            return {
              x: metrics.offsetX + selection.x * metrics.width,
              y: metrics.offsetY + selection.y * metrics.height,
              width: selection.width * metrics.width,
              height: selection.height * metrics.height,
            };
          })()
        : null;

    if (!rect || (rect.width < 2 && rect.height < 2)) return null;

    return (
      <div
        style={{
          ...local.selectionRect,
          left: rect.x,
          top: rect.y,
          width: rect.width,
          height: rect.height,
        }}
      />
    );
  };

  const handleGenerate = () => {
    if (!canGenerate || !sourceImage) return;
    void generate(prompt, {
      mode: 'inpaint',
      sourceImage,
      maskRegion: selection ?? undefined,
    });
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
      <div style={ss.formRow}>
        <label style={ss.formLabel}>
          {t('playground.studio_source_image', { defaultValue: '参考图片' })}
        </label>

        {sourceImage ? (
          <>
            <div
              ref={containerRef}
              style={local.canvasContainer}
              onMouseDown={handleMouseDown}
              onMouseMove={handleMouseMove}
              onMouseUp={handleMouseUp}
              onMouseLeave={handleMouseUp}
            >
              <img ref={imgRef} src={sourceImage} alt="source" style={local.sourceImg} />
              {renderSelectionOverlay()}
            </div>

            <div style={local.canvasActions}>
              <button
                type="button"
                style={local.actionBtn}
                onClick={() => setSelection(null)}
              >
                {t('playground.studio_clear_selection', { defaultValue: '清除选区' })}
              </button>
              <button
                type="button"
                style={local.actionBtn}
                onClick={() => { setSourceImage(null); setSelection(null); }}
              >
                {t('playground.studio_remove_image', { defaultValue: '移除图片' })}
              </button>
            </div>

            <div style={local.selectionHint}>
              {selection
                ? t('playground.studio_selection_set', { defaultValue: '已选定修改区域，拖拽可重新选择' })
                : t('playground.studio_selection_hint', { defaultValue: '在图片上拖拽选择要修改的区域' })}
            </div>
          </>
        ) : (
          <div
            style={isDragging ? ss.formUploadAreaDragging : ss.formUploadArea}
            className="studio-upload-area"
            onClick={() => fileInputRef.current?.click()}
            onDragOver={handleDropzoneDragOver}
            onDragLeave={handleDropzoneDragLeave}
            onDrop={handleDropzoneDrop}
            role="button"
            tabIndex={0}
            onKeyDown={e => { if (e.key === 'Enter' || e.key === ' ') fileInputRef.current?.click(); }}
          >
            <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" opacity={0.35}>
              <rect x="3" y="3" width="18" height="18" rx="2" />
              <circle cx="8.5" cy="8.5" r="1.5" />
              <path d="M21 15l-5-5L5 21" />
            </svg>
            <span>
              {t('playground.studio_upload_hint', { defaultValue: '点击上传或拖拽图片到此处' })}
            </span>
          </div>
        )}

        <input
          ref={fileInputRef}
          type="file"
          accept="image/*"
          style={{ display: 'none' }}
          onChange={handleFileInputChange}
        />
      </div>

      <div style={ss.formRow}>
        <label style={ss.formLabel}>
          {t('playground.studio_prompt', { defaultValue: '提示词' })}
        </label>
        <textarea
          style={ss.formTextarea}
          className="studio-textarea"
          value={prompt}
          onChange={e => setPrompt(e.target.value)}
          placeholder={t('playground.studio_inpaint_placeholder', { defaultValue: '描述要修改的区域...' })}
          rows={3}
        />
      </div>

      <div style={ss.formRow}>
        <label style={ss.formLabel}>
          {t('playground.studio_model', { defaultValue: '模型' })}
        </label>
        <ModelPicker value={selectedModelId} models={models} onChange={setSelectedModelId} />
      </div>

      {currentModel.caps.supportsSize && (
        <div style={ss.formRow}>
          <label style={ss.formLabel}>
            {t('playground.studio_size', { defaultValue: '尺寸' })}
          </label>
          <SizeSelector
            value={imageSize}
            sizes={currentModel.caps.sizes}
            onChange={setImageSize}
          />
        </div>
      )}

      <button
        type="button"
        style={canGenerate ? ss.formGenerateBtn : ss.formGenerateBtnDisabled}
        className={canGenerate ? 'studio-gen-btn' : ''}
        disabled={!canGenerate}
        onClick={handleGenerate}
      >
        {isGenerating
          ? t('playground.studio_generating', { defaultValue: '生成中...' })
          : t('playground.studio_generate', { defaultValue: '生成' })}
      </button>
    </div>
  );
}
