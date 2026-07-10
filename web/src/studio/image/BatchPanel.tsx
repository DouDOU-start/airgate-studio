import { useCallback, useRef, useState, type CSSProperties } from 'react';
import { useTranslation } from 'react-i18next';
import { cssVar } from '@doudou-start/airgate-theme';
import { useStudio } from '../StudioContext';
import { SizeSelector } from '../SizeSelector';
import { ModelPicker } from '../ModelPicker';
import { studioStyles as ss } from '../studioStyles';

const local: Record<string, CSSProperties> = {
  tabRow: {
    display: 'flex',
    gap: 0,
    borderRadius: 10,
    overflow: 'hidden',
    border: `1px solid ${cssVar('borderSubtle')}`,
  },
  tab: {
    flex: 1,
    padding: '8px 0',
    border: 'none',
    background: 'transparent',
    color: cssVar('textSecondary'),
    fontSize: 12,
    fontWeight: 500,
    cursor: 'pointer',
    fontFamily: 'inherit',
    transition: 'all 0.18s',
  },
  tabActive: {
    flex: 1,
    padding: '8px 0',
    border: 'none',
    background: cssVar('bgHover'),
    color: cssVar('text'),
    fontSize: 12,
    fontWeight: 700,
    cursor: 'pointer',
    fontFamily: 'inherit',
  },
  uploadArea: {
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 6,
    minHeight: 80,
    padding: 14,
    border: `1.5px dashed ${cssVar('borderSubtle')}`,
    borderRadius: 12,
    cursor: 'pointer',
    color: cssVar('textTertiary'),
    fontSize: 12,
    textAlign: 'center',
    transition: 'all 0.2s',
  },
  thumbGrid: {
    display: 'grid',
    gridTemplateColumns: 'repeat(3, 1fr)',
    gap: 6,
  },
  thumb: {
    position: 'relative',
    aspectRatio: '1',
    borderRadius: 8,
    overflow: 'hidden',
    border: `1px solid ${cssVar('borderSubtle')}`,
  },
  thumbImg: {
    width: '100%',
    height: '100%',
    objectFit: 'cover',
    display: 'block',
  },
  thumbRemove: {
    position: 'absolute',
    top: 3,
    right: 3,
    width: 18,
    height: 18,
    border: 'none',
    borderRadius: '50%',
    background: 'rgba(0, 0, 0, 0.6)',
    backdropFilter: 'blur(4px)',
    WebkitBackdropFilter: 'blur(4px)',
    color: '#fff',
    fontSize: 11,
    cursor: 'pointer',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    padding: 0,
    lineHeight: 1,
    transition: 'background 0.15s',
  },
};

type BatchMode = 'multi_prompt' | 'multi_image';

export function BatchPanel() {
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

  const [mode, setMode] = useState<BatchMode>('multi_prompt');
  const [multiPrompts, setMultiPrompts] = useState('');
  const [images, setImages] = useState<Array<{ id: string; url: string; file: File }>>([]);
  const [imagePrompt, setImagePrompt] = useState('');
  const fileInputRef = useRef<HTMLInputElement>(null);

  const promptLines = multiPrompts.split('\n').map(l => l.trim()).filter(l => l.length > 0);

  const canGenerate = (
    mode === 'multi_prompt' ? promptLines.length > 0 : (images.length > 0 && imagePrompt.trim().length > 0)
  );

  const addImages = useCallback((files: FileList | null) => {
    if (!files) return;
    Array.from(files).forEach(file => {
      if (!file.type.startsWith('image/')) return;
      const reader = new FileReader();
      reader.onload = () => {
        setImages(prev => [...prev, { id: `${file.name}-${Date.now()}`, url: reader.result as string, file }]);
      };
      reader.readAsDataURL(file);
    });
  }, []);

  const removeImage = useCallback((id: string) => {
    setImages(prev => prev.filter(img => img.id !== id));
  }, []);

  const handleGenerate = async () => {
    if (!canGenerate) return;
    if (mode === 'multi_prompt') {
      for (const line of promptLines) {
        void generate(line, { mode: 'batch', count: 1 });
      }
    } else {
      for (const img of images) {
        void generate(imagePrompt.trim(), { mode: 'img2img', sourceImage: img.url });
      }
    }
  };

  const btnLabel = isGenerating
    ? t('playground.studio_generating', { defaultValue: '生成中...' })
    : mode === 'multi_prompt'
      ? `${t('playground.studio_batch_generate', { defaultValue: '批量生成' })} ${promptLines.length} ${t('playground.studio_batch_unit', { defaultValue: '张' })}`
      : `${t('playground.studio_batch_process', { defaultValue: '批量处理' })} ${images.length} ${t('playground.studio_batch_images', { defaultValue: '张图片' })}`;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
      <div style={local.tabRow}>
        <button type="button" style={mode === 'multi_prompt' ? local.tabActive : local.tab} onClick={() => setMode('multi_prompt')}>
          {t('playground.studio_batch_multi_prompt', { defaultValue: '多提示词' })}
        </button>
        <button type="button" style={mode === 'multi_image' ? local.tabActive : local.tab} onClick={() => setMode('multi_image')}>
          {t('playground.studio_batch_multi_image', { defaultValue: '多图片' })}
        </button>
      </div>

      {mode === 'multi_prompt' ? (
        <div style={ss.formRow}>
          <label style={ss.formLabel}>{t('playground.studio_batch_prompts', { defaultValue: '批量提示词' })}</label>
          <textarea
            style={{ ...ss.formTextarea, minHeight: 96 }}
            className="studio-textarea"
            value={multiPrompts}
            onChange={e => setMultiPrompts(e.target.value)}
            placeholder={t('playground.studio_batch_placeholder', { defaultValue: '每行一个提示词...' })}
            rows={5}
          />
          <div style={ss.formHint}>
            {promptLines.length > 0 ? `共 ${promptLines.length} 个提示词` : t('playground.studio_batch_empty', { defaultValue: '尚未输入提示词' })}
          </div>
        </div>
      ) : (
        <>
          <div style={ss.formRow}>
            <label style={ss.formLabel}>{t('playground.studio_batch_upload', { defaultValue: '上传图片' })}</label>
            <div
              style={local.uploadArea}
              className="studio-upload-area"
              onClick={() => fileInputRef.current?.click()}
              onDragOver={e => e.preventDefault()}
              onDrop={e => { e.preventDefault(); addImages(e.dataTransfer.files); }}
            >
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" opacity={0.4}>
                <path d="M12 5v14M5 12h14" />
              </svg>
              {t('playground.studio_batch_add_images', { defaultValue: '点击或拖拽添加图片' })}
            </div>
            <input ref={fileInputRef} type="file" accept="image/*" multiple style={{ display: 'none' }} onChange={e => addImages(e.target.files)} />
          </div>

          {images.length > 0 && (
            <div style={local.thumbGrid}>
              {images.map(img => (
                <div key={img.id} style={local.thumb}>
                  <img src={img.url} alt="" style={local.thumbImg} />
                  <button type="button" style={local.thumbRemove} onClick={() => removeImage(img.id)}>×</button>
                </div>
              ))}
            </div>
          )}

          <div style={ss.formRow}>
            <label style={ss.formLabel}>{t('playground.studio_batch_shared_prompt', { defaultValue: '统一提示词' })}</label>
            <textarea
              style={{ ...ss.formTextarea, minHeight: 72 }}
              className="studio-textarea"
              value={imagePrompt}
              onChange={e => setImagePrompt(e.target.value)}
              placeholder={t('playground.studio_batch_shared_placeholder', { defaultValue: '对所有图片应用相同的描述...' })}
              rows={3}
            />
          </div>
        </>
      )}

      <div style={ss.formRow}>
        <label style={ss.formLabel}>{t('playground.studio_model', { defaultValue: '模型' })}</label>
        <ModelPicker value={selectedModelId} models={models} onChange={setSelectedModelId} />
      </div>
      {currentModel.caps.supportsSize && (
        <div style={ss.formRow}>
          <label style={ss.formLabel}>{t('playground.studio_size', { defaultValue: '尺寸' })}</label>
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
        onClick={() => { void handleGenerate(); }}
      >
        {btnLabel}
      </button>
    </div>
  );
}
