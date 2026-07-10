import { useCallback, useEffect, useRef, useState, type CSSProperties, type MouseEvent as ReactMouseEvent } from 'react';
import { cssVar } from '@doudou-start/airgate-theme';

// ── MaskEditor（全屏蒙层：框选局部重绘区域）──────────────────────────────────

export interface NormalizedRect { x: number; y: number; width: number; height: number }

function normalizeRect(
  sx: number, sy: number, ex: number, ey: number, cw: number, ch: number,
): NormalizedRect {
  if (cw <= 0 || ch <= 0) return { x: 0, y: 0, width: 0, height: 0 };
  const clamp = (value: number, max: number) => Math.max(0, Math.min(max, value));
  const x1 = clamp(sx, cw);
  const y1 = clamp(sy, ch);
  const x2 = clamp(ex, cw);
  const y2 = clamp(ey, ch);
  return {
    x: Math.min(x1, x2) / cw,
    y: Math.min(y1, y2) / ch,
    width: Math.abs(x2 - x1) / cw,
    height: Math.abs(y2 - y1) / ch,
  };
}

const me: Record<string, CSSProperties> = {
  overlay: {
    position: 'fixed', inset: 0, zIndex: 1100,
    background: 'rgba(0,0,0,0.82)',
    display: 'flex', flexDirection: 'column',
    alignItems: 'center', justifyContent: 'center',
    gap: 14, padding: 40,
  },
  hint: {
    fontSize: 12, color: 'rgba(255,255,255,0.5)',
    fontFamily: 'inherit', letterSpacing: '0.01em',
    userSelect: 'none',
  },
  canvas: {
    position: 'relative', borderRadius: 10, overflow: 'hidden',
    cursor: 'crosshair', userSelect: 'none', lineHeight: 0,
    boxShadow: '0 12px 40px rgba(0,0,0,0.5)',
  },
  img: {
    display: 'block', maxWidth: '70vw', maxHeight: '60vh',
    objectFit: 'contain', pointerEvents: 'none',
  },
  selRect: {
    position: 'absolute',
    border: '2px solid rgba(248,113,113,0.95)',
    background: 'rgba(248,113,113,0.32)',
    boxShadow: '0 0 0 9999px rgba(0,0,0,0.28), inset 0 0 0 1px rgba(255,255,255,0.65), 0 0 18px rgba(248,113,113,0.45)',
    borderRadius: 4, pointerEvents: 'none', boxSizing: 'border-box',
  } as CSSProperties,
  actions: {
    display: 'flex', gap: 8,
  },
  btn: {
    padding: '8px 20px', border: '1px solid rgba(255,255,255,0.12)',
    borderRadius: 10, background: 'rgba(255,255,255,0.08)',
    color: '#fff', fontSize: 13, fontWeight: 600,
    cursor: 'pointer', fontFamily: 'inherit',
    transition: 'background 0.15s',
  },
  btnPrimary: {
    padding: '8px 20px', border: 'none',
    borderRadius: 10, background: cssVar('primary'),
    color: cssVar('primaryForeground'), fontSize: 13, fontWeight: 600,
    cursor: 'pointer', fontFamily: 'inherit',
    transition: 'opacity 0.15s',
  },
  btnDanger: {
    padding: '8px 20px', border: '1px solid rgba(248,113,113,0.3)',
    borderRadius: 10, background: 'transparent',
    color: '#f87171', fontSize: 13, fontWeight: 600,
    cursor: 'pointer', fontFamily: 'inherit',
    transition: 'background 0.15s',
    marginRight: 'auto',
  },
};

export function MaskEditor({ src, selection: initialSelection, onConfirm, onClose, onDelete, maskingEnabled = true }: {
  src: string;
  selection: NormalizedRect | null;
  onConfirm: (sel: NormalizedRect | null) => void;
  onClose: () => void;
  onDelete?: () => void;
  maskingEnabled?: boolean;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const imgRef = useRef<HTMLImageElement>(null);
  const [sel, setSel] = useState<NormalizedRect | null>(initialSelection);
  const [dragStart, setDragStart] = useState<{ x: number; y: number } | null>(null);
  const [liveRect, setLiveRect] = useState<{ x: number; y: number; w: number; h: number } | null>(null);

  useEffect(() => {
    const handleKey = (e: globalThis.KeyboardEvent) => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', handleKey);
    return () => window.removeEventListener('keydown', handleKey);
  }, [onClose]);

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

  const getRelPos = useCallback((e: ReactMouseEvent): { x: number; y: number } | null => {
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
      left: metrics.offsetX + rect.x,
      top: metrics.offsetY + rect.y,
      width: rect.w,
      height: rect.h,
    };
  }, [getImageMetrics]);

  const onDown = useCallback((e: ReactMouseEvent<HTMLDivElement>) => {
    const pos = getRelPos(e);
    if (!pos) return;
    e.preventDefault();
    setDragStart(pos);
    setLiveRect({ x: pos.x, y: pos.y, w: 0, h: 0 });
    setSel(null);
  }, [getRelPos]);

  const onMove = useCallback((e: ReactMouseEvent<HTMLDivElement>) => {
    if (!dragStart) return;
    const pos = getRelPos(e);
    if (!pos) return;
    setLiveRect({
      x: Math.min(dragStart.x, pos.x), y: Math.min(dragStart.y, pos.y),
      w: Math.abs(pos.x - dragStart.x), h: Math.abs(pos.y - dragStart.y),
    });
  }, [dragStart, getRelPos]);

  const onUp = useCallback((e: ReactMouseEvent<HTMLDivElement>) => {
    if (!dragStart) return;
    const pos = getRelPos(e);
    const metrics = getImageMetrics();
    if (!pos || !metrics) { setDragStart(null); setLiveRect(null); return; }
    const norm = normalizeRect(dragStart.x, dragStart.y, pos.x, pos.y, metrics.width, metrics.height);
    if (norm.width > 0.01 && norm.height > 0.01) setSel(norm);
    setDragStart(null);
    setLiveRect(null);
  }, [dragStart, getImageMetrics, getRelPos]);

  const overlay = (() => {
    const rect = liveRect
      ? toContainerRect(liveRect)
      : sel
        ? (() => {
            const metrics = getImageMetrics();
            if (!metrics) return null;
            return {
              left: metrics.offsetX + sel.x * metrics.width,
              top: metrics.offsetY + sel.y * metrics.height,
              width: sel.width * metrics.width,
              height: sel.height * metrics.height,
            };
          })()
        : null;
    if (!rect || (rect.width < 2 && rect.height < 2)) return null;
    return <div style={{ ...me.selRect, ...rect }} />;
  })();

  return (
    <div style={me.overlay} onClick={onClose}>
      {maskingEnabled && (
        <div style={me.hint}>在图片上拖拽框选要局部修改的区域，不框选则为整图变换</div>
      )}
      <div
        ref={containerRef}
        style={maskingEnabled ? me.canvas : { ...me.canvas, cursor: 'default' }}
        onClick={e => e.stopPropagation()}
        onMouseDown={maskingEnabled ? onDown : undefined}
        onMouseMove={maskingEnabled ? onMove : undefined}
        onMouseUp={maskingEnabled ? onUp : undefined}
        onMouseLeave={maskingEnabled ? onUp : undefined}
      >
        <img ref={imgRef} src={src} alt="source" style={me.img} />
        {maskingEnabled && overlay}
      </div>
      <div style={me.actions} onClick={e => e.stopPropagation()}>
        {onDelete && (
          <button type="button" style={me.btnDanger} onClick={onDelete}>删除图片</button>
        )}
        {maskingEnabled && sel && (
          <button type="button" style={me.btn} onClick={() => setSel(null)}>清除选区</button>
        )}
        <button type="button" style={me.btn} onClick={onClose}>{maskingEnabled ? '取消' : '关闭'}</button>
        {maskingEnabled && (
          <button type="button" style={me.btnPrimary} onClick={() => onConfirm(sel)}>确定</button>
        )}
      </div>
    </div>
  );
}
