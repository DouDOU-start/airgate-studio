import type { CSSProperties } from 'react';
import { cssVar } from '@doudou-start/airgate-theme';

export const studioStyles: Record<string, CSSProperties> = {
  // ── Layout ────────────────────────────────────────────────────────────────

  layout: {
    position: 'fixed',
    top: 0,
    left: 0,
    width: '100vw',
    height: '100vh',
    display: 'flex',
    flexDirection: 'row',
    background: cssVar('bgElevated'),
    color: cssVar('text'),
    fontFamily: cssVar('fontSans'),
    overflow: 'hidden',
  },

  // ── Gallery (left pane) ───────────────────────────────────────────────────

  gallery: {
    flex: 1,
    minWidth: 0,
    minHeight: 0,
    width: '100%',
    overflowY: 'auto',
    overflowX: 'hidden',
    padding: '20px',
    boxSizing: 'border-box',
    background: cssVar('bgElevated'),
  } as CSSProperties,

  galleryGrid: {
    columns: '200px',
    columnGap: 14,
    width: '100%',
  } as CSSProperties,

  galleryCard: {
    position: 'relative',
    borderRadius: 14,
    overflow: 'hidden',
    background: cssVar('bgElevated'),
    border: `1px solid ${cssVar('borderSubtle')}`,
    boxShadow: '0 2px 8px rgba(0, 0, 0, 0.08), 0 1px 3px rgba(0, 0, 0, 0.06)',
    cursor: 'pointer',
    display: 'flex',
    flexDirection: 'column',
    marginBottom: 14,
    breakInside: 'avoid',
    transition: 'transform 0.25s cubic-bezier(0.4, 0, 0.2, 1), box-shadow 0.25s cubic-bezier(0.4, 0, 0.2, 1)',
  } as CSSProperties,

  galleryCardImg: {
    width: '100%',
    objectFit: 'cover',
    display: 'block',
  },

  galleryCardOverlay: {
    display: 'flex',
    flexDirection: 'column',
    gap: 7,
    padding: '8px 10px',
    background: cssVar('bgElevated'),
  } as CSSProperties,

  galleryCardMetaRow: {
    display: 'flex',
    alignItems: 'center',
    flexWrap: 'wrap',
    gap: 6,
  } as CSSProperties,

  galleryCardMetaItem: {
    display: 'inline-flex',
    alignItems: 'center',
    fontSize: 10,
    lineHeight: 1.2,
    color: cssVar('textTertiary'),
    fontFamily: cssVar('fontMono'),
    whiteSpace: 'nowrap',
    letterSpacing: '0.01em',
  } as CSSProperties,

  galleryCardPrompt: {
    fontSize: 11,
    color: cssVar('textTertiary'),
    lineHeight: 1.45,
    overflow: 'hidden',
    display: '-webkit-box',
    WebkitLineClamp: 2,
    WebkitBoxOrient: 'vertical',
    letterSpacing: '0.01em',
  },

  galleryCardActions: {
    display: 'flex',
    gap: 6,
  },

  galleryCardActionBtn: {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 28,
    height: 28,
    padding: 0,
    border: `1px solid ${cssVar('borderSubtle')}`,
    borderRadius: 7,
    background: 'transparent',
    color: cssVar('textTertiary'),
    cursor: 'pointer',
    transition: 'background 0.15s, color 0.15s',
  },

  // ── Preview overlay ───────────────────────────────────────────────────────

  previewOverlay: {
    position: 'fixed',
    inset: 0,
    zIndex: 1000,
    background: 'rgba(0, 0, 0, 0.75)',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    cursor: 'pointer',
  },

  previewOverlayImg: {
    maxWidth: '60vw',
    maxHeight: '65vh',
    borderRadius: 12,
    objectFit: 'contain',
    boxShadow: '0 12px 40px rgba(0, 0, 0, 0.5)',
    cursor: 'default',
  },

  previewCloseBtn: {
    position: 'absolute',
    top: 20,
    right: 20,
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 40,
    height: 40,
    border: '1px solid rgba(255, 255, 255, 0.12)',
    borderRadius: 12,
    background: 'rgba(255, 255, 255, 0.08)',
    backdropFilter: 'blur(8px)',
    WebkitBackdropFilter: 'blur(8px)',
    color: '#fff',
    fontSize: 20,
    cursor: 'pointer',
    fontFamily: 'inherit',
    transition: 'background 0.15s',
  },
};

export const studioCSS = `
  .studio-gallery-card:hover {
    transform: translateY(-3px) scale(1.005);
    box-shadow: 0 12px 40px rgba(0, 0, 0, 0.5), 0 4px 16px rgba(0, 0, 0, 0.3);
  }
  .studio-gallery-card:active {
    transform: translateY(-1px) scale(1.0);
  }

  .studio-gallery-action:hover {
    background: ${cssVar('bgHover')} !important;
    color: ${cssVar('text')} !important;
  }

  .studio-send-btn:hover:not(:disabled) {
    transform: scale(1.06);
    box-shadow: 0 0 20px ${cssVar('primaryGlow')};
  }

  .studio-quick-input:focus-within {
    border-color: color-mix(in oklab, ${cssVar('primary')} 35%, transparent);
    box-shadow: 0 8px 40px rgba(0, 0, 0, 0.4), 0 2px 12px rgba(0, 0, 0, 0.2), 0 0 0 1px color-mix(in oklab, ${cssVar('primary')} 12%, transparent);
  }

  .studio-textarea:focus {
    border-color: color-mix(in oklab, ${cssVar('primary')} 30%, transparent) !important;
    box-shadow: 0 0 0 3px ${cssVar('primaryGlow')};
  }

  .studio-template-card:hover {
    border-color: ${cssVar('border')};
    transform: translateY(-2px);
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.3);
  }

  .studio-console-link:hover {
    color: ${cssVar('text')} !important;
    background: ${cssVar('bgHover')};
  }

  .studio-preview-close:hover {
    background: rgba(255, 255, 255, 0.16) !important;
  }

  .studio-upload-area:hover {
    border-color: ${cssVar('border')};
    background: ${cssVar('bgHover')};
  }

  @keyframes spin {
    to { transform: rotate(360deg); }
  }

  @keyframes studioFadeIn {
    from { opacity: 0; transform: translateY(8px) scale(0.97); }
    to { opacity: 1; transform: translateY(0) scale(1); }
  }

  @keyframes studioPulse {
    0%, 100% { opacity: 0.4; }
    50% { opacity: 1; }
  }

  @keyframes studioShimmer {
    0% { background-position: -200% 0; }
    100% { background-position: 200% 0; }
  }

  .studio-gallery-card {
    animation: studioFadeIn 0.35s cubic-bezier(0.4, 0, 0.2, 1) backwards;
  }

  .studio-sidebar::-webkit-scrollbar {
    width: 4px;
  }
  .studio-sidebar::-webkit-scrollbar-track {
    background: transparent;
  }
  .studio-sidebar::-webkit-scrollbar-thumb {
    background: rgba(255, 255, 255, 0.08);
    border-radius: 4px;
  }
  .studio-sidebar::-webkit-scrollbar-thumb:hover {
    background: rgba(255, 255, 255, 0.14);
  }

  .studio-gallery {
    scrollbar-width: none;
  }
  .studio-gallery::-webkit-scrollbar {
    display: none;
  }

  .studio-panel-inspiration {
    flex: 0 0 320px;
    transition: flex-basis 0.2s ease;
  }
  .studio-panel-inspiration[data-collapsed="true"] {
    flex: 0 0 48px;
  }
  .studio-panel-inspiration[data-collapsed="true"] .studio-inspiration-content {
    display: none;
  }
  .studio-panel-inspiration:not([data-collapsed="true"]) .studio-inspiration-strip {
    display: none;
  }
  .studio-collapsed-strip:hover {
    background: ${cssVar('bgHover')} !important;
    color: ${cssVar('text')} !important;
  }
  .studio-collapsed-strip:hover .studio-collapse-icon,
  .studio-collapse-btn:hover {
    background: ${cssVar('bgHover')} !important;
    color: ${cssVar('text')} !important;
  }
  .studio-collapse-btn svg,
  .studio-collapse-icon svg {
    stroke-width: 2.5;
  }

  @media (max-width: 1023px) {
    .studio-mobile-tabs {
      display: flex !important;
    }
    [data-mobile-tab] {
      flex-direction: column !important;
    }
    [data-mobile-tab="create"] .studio-panel-inspiration {
      display: none !important;
    }
    [data-mobile-tab="inspiration"] .studio-panel-create {
      display: none !important;
    }
    .studio-panel-inspiration,
    .studio-panel-create {
      flex: 1 !important;
      min-height: 0 !important;
      width: 100% !important;
    }
    .studio-panel-inspiration .studio-inspiration-content {
      display: block !important;
    }
    .studio-panel-inspiration .studio-inspiration-strip {
      display: none !important;
    }
    .studio-composer-toolbar-left {
      flex-wrap: wrap;
      gap: 4px !important;
    }
  }
`;
