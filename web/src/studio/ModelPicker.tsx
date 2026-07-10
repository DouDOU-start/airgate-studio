import { useState, useRef, useEffect, useCallback, type CSSProperties, type KeyboardEvent } from 'react';
import { cssVar } from '@doudou-start/airgate-theme';
import type { ImageModel } from './modelConfig';

// ModelPicker：动态模型下拉 + 手动输入。
// 列表来自 /api/models 的图像模型启发式过滤；启发式漏掉的模型可在输入框手动填写。

interface ModelPickerProps {
  value: string;
  models: ImageModel[];
  onChange: (id: string) => void;
  upward?: boolean;
  compact?: boolean;
}

const s: Record<string, CSSProperties> = {
  trigger: {
    width: '100%',
    padding: '9px 14px',
    border: `1px solid ${cssVar('borderSubtle')}`,
    borderRadius: 10,
    background: cssVar('bgDeep'),
    color: cssVar('text'),
    cursor: 'pointer',
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    font: 'inherit',
    fontSize: 13,
    transition: 'border-color 0.2s, box-shadow 0.2s',
    boxSizing: 'border-box',
  },
  triggerOpen: {
    borderColor: `color-mix(in oklab, ${cssVar('primary')} 30%, transparent)`,
    boxShadow: `0 0 0 3px ${cssVar('primaryGlow')}`,
  },
  triggerCompact: {
    height: 26,
    minHeight: 26,
    padding: '0 10px',
    borderRadius: 6,
    fontSize: 11,
    fontFamily: cssVar('fontMono'),
  },
  dot: {
    width: 5,
    height: 5,
    borderRadius: '50%',
    background: '#4ade80',
    flexShrink: 0,
    boxShadow: '0 0 5px rgba(74, 222, 128, 0.4)',
  },
  dropdown: {
    position: 'fixed',
    zIndex: 999999,
    background: cssVar('bgElevated'),
    border: `1px solid ${cssVar('glassBorder')}`,
    borderRadius: 12,
    boxShadow: '0 12px 40px rgba(0, 0, 0, 0.5), 0 4px 12px rgba(0, 0, 0, 0.3)',
    maxHeight: 320,
    overflowY: 'auto',
    padding: 5,
    minWidth: 240,
  },
  option: {
    width: '100%',
    padding: '8px 14px',
    border: 'none',
    background: 'transparent',
    color: cssVar('text'),
    textAlign: 'left',
    cursor: 'pointer',
    borderRadius: 8,
    fontSize: 12,
    fontFamily: cssVar('fontMono'),
    transition: 'background 0.12s',
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    boxSizing: 'border-box',
  },
  optionActive: {
    background: cssVar('primarySubtle'),
    fontWeight: 600,
  },
  optionProto: {
    fontSize: 10,
    color: cssVar('textTertiary'),
    marginLeft: 'auto',
    flexShrink: 0,
  },
  emptyHint: {
    padding: '10px 14px',
    fontSize: 11,
    color: cssVar('textTertiary'),
  },
  divider: {
    height: 1,
    margin: '4px 10px',
    background: cssVar('borderSubtle'),
  },
  customRow: {
    display: 'flex',
    gap: 6,
    padding: '6px 8px 4px',
    alignItems: 'center',
  },
  customInput: {
    flex: 1,
    minWidth: 0,
    padding: '6px 10px',
    border: `1px solid ${cssVar('borderSubtle')}`,
    borderRadius: 8,
    background: cssVar('bgDeep'),
    color: cssVar('text'),
    fontSize: 12,
    fontFamily: cssVar('fontMono'),
    outline: 'none',
  },
  customBtn: {
    padding: '6px 12px',
    border: 'none',
    borderRadius: 8,
    background: cssVar('primary'),
    color: cssVar('primaryForeground'),
    fontSize: 11,
    fontWeight: 600,
    cursor: 'pointer',
    fontFamily: 'inherit',
    flexShrink: 0,
  },
  customHint: {
    padding: '2px 10px 6px',
    fontSize: 10,
    color: cssVar('textTertiary'),
    opacity: 0.7,
  },
};

const hoverCSS = `
  .studio-model-option:hover { background: ${cssVar('bgHover')}; }
  .studio-model-trigger:hover { border-color: ${cssVar('border')}; }
`;

export function ModelPicker({ value, models, onChange, upward, compact }: ModelPickerProps) {
  const [open, setOpen] = useState(false);
  const [custom, setCustom] = useState('');
  const triggerRef = useRef<HTMLButtonElement>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<{ top: number; left: number; width: number }>({ top: 0, left: 0, width: 240 });

  const calcPos = useCallback(() => {
    const el = triggerRef.current;
    if (!el) return;
    const rect = el.getBoundingClientRect();
    const width = Math.max(rect.width, 240);
    setPos(upward
      ? { top: rect.top - 6, left: rect.left, width }
      : { top: rect.bottom + 6, left: rect.left, width });
  }, [upward]);

  const handleToggle = () => {
    if (!open) calcPos();
    setOpen(v => !v);
  };

  useEffect(() => {
    if (!open) return;
    const handleOutside = (e: MouseEvent) => {
      const t = e.target as Node;
      if (triggerRef.current?.contains(t)) return;
      if (dropdownRef.current?.contains(t)) return;
      setOpen(false);
    };
    document.addEventListener('mousedown', handleOutside);
    return () => document.removeEventListener('mousedown', handleOutside);
  }, [open]);

  const select = (id: string) => {
    onChange(id);
    setOpen(false);
    setCustom('');
  };

  const confirmCustom = () => {
    const id = custom.trim();
    if (id) select(id);
  };

  const handleCustomKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      confirmCustom();
    }
  };

  const dropdownStyle: CSSProperties = upward
    ? { ...s.dropdown, bottom: `calc(100vh - ${pos.top}px)`, left: pos.left, width: pos.width, top: 'auto' }
    : { ...s.dropdown, top: pos.top, left: pos.left, width: pos.width };

  return (
    <>
      <style>{hoverCSS}</style>
      <button
        ref={triggerRef}
        type="button"
        onClick={handleToggle}
        style={{ ...s.trigger, ...(compact ? s.triggerCompact : {}), ...(open ? s.triggerOpen : {}) }}
        className="studio-model-trigger"
        title={value || '选择模型'}
      >
        <span style={s.dot} />
        <span style={{ flex: 1, textAlign: 'left', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {value || '选择模型'}
        </span>
        <svg
          width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor"
          strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round"
          style={{ opacity: 0.4, transition: 'transform 0.2s', transform: open ? 'rotate(180deg)' : 'rotate(0deg)', flexShrink: 0 }}
        >
          <path d="M6 9l6 6 6-6" />
        </svg>
      </button>

      {open && (
        <div ref={dropdownRef} style={dropdownStyle} className="studio-sidebar">
          {models.length === 0 && (
            <div style={s.emptyHint}>未发现图像模型，可在下方手动输入模型名</div>
          )}
          {models.map(m => (
            <button
              key={m.id}
              type="button"
              onClick={() => select(m.id)}
              style={{ ...s.option, ...(m.id === value ? s.optionActive : {}) }}
              className={m.id === value ? '' : 'studio-model-option'}
            >
              <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{m.id}</span>
              <span style={s.optionProto}>{m.protocols.join('/')}</span>
            </button>
          ))}
          <div style={s.divider} />
          <div style={s.customRow}>
            <input
              style={s.customInput}
              value={custom}
              onChange={e => setCustom(e.target.value)}
              onKeyDown={handleCustomKeyDown}
              placeholder="手动输入模型名..."
            />
            <button type="button" style={s.customBtn} onClick={confirmCustom} disabled={!custom.trim()}>
              使用
            </button>
          </div>
          <div style={s.customHint}>列表按名称启发式过滤，漏掉的生图模型可手动填写</div>
        </div>
      )}
    </>
  );
}
