import { useRef, useState, type CSSProperties, type DragEvent, type ChangeEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { cssVar } from '@doudou-start/airgate-theme';
import { useStudio } from '../StudioContext';
import { SizeSelector } from '../SizeSelector';
import { ModelPicker } from '../ModelPicker';
import { studioStyles as ss } from '../studioStyles';

const local: Record<string, CSSProperties> = {
  previewWrapper: {
    position: 'relative',
    display: 'inline-flex',
    borderRadius: 10,
    overflow: 'hidden',
    border: `1px solid ${cssVar('borderSubtle')}`,
    alignSelf: 'flex-start',
  },
  previewImg: {
    width: 120,
    height: 90,
    objectFit: 'cover',
    display: 'block',
  },
  removeBtn: {
    position: 'absolute',
    top: 4,
    right: 4,
    width: 22,
    height: 22,
    border: 'none',
    borderRadius: '50%',
    background: 'rgba(0, 0, 0, 0.6)',
    backdropFilter: 'blur(4px)',
    WebkitBackdropFilter: 'blur(4px)',
    color: '#fff',
    cursor: 'pointer',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    fontSize: 13,
    lineHeight: 1,
    padding: 0,
    transition: 'background 0.15s',
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

export function ImageToImagePanel() {
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
  const fileInputRef = useRef<HTMLInputElement>(null);

  const canGenerate = prompt.trim().length > 0 && sourceImage !== null;

  const handleFile = async (file: File) => {
    if (!file.type.startsWith('image/')) return;
    try {
      const dataUrl = await readFileAsDataURL(file);
      setSourceImage(dataUrl);
    } catch { /* ignore */ }
  };

  const handleFileInputChange = (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (file) void handleFile(file);
    e.target.value = '';
  };

  const handleDragOver = (e: DragEvent<HTMLDivElement>) => {
    e.preventDefault();
    setIsDragging(true);
  };

  const handleDragLeave = () => setIsDragging(false);

  const handleDrop = (e: DragEvent<HTMLDivElement>) => {
    e.preventDefault();
    setIsDragging(false);
    const file = e.dataTransfer.files?.[0];
    if (file) void handleFile(file);
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
      <div style={ss.formRow}>
        <label style={ss.formLabel}>
          {t('playground.studio_source_image', { defaultValue: '参考图片' })}
        </label>

        {sourceImage ? (
          <div style={local.previewWrapper}>
            <img src={sourceImage} alt="source" style={local.previewImg} />
            <button
              type="button"
              style={local.removeBtn}
              onClick={() => setSourceImage(null)}
              title={t('playground.studio_remove_image', { defaultValue: '移除图片' })}
            >
              ×
            </button>
          </div>
        ) : (
          <div
            style={isDragging ? ss.formUploadAreaDragging : ss.formUploadArea}
            className="studio-upload-area"
            onClick={() => fileInputRef.current?.click()}
            onDragOver={handleDragOver}
            onDragLeave={handleDragLeave}
            onDrop={handleDrop}
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
          placeholder={t('playground.studio_img2img_placeholder', { defaultValue: '描述你想要的变化...' })}
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
        onClick={() => { void generate(prompt, { mode: 'img2img', sourceImage: sourceImage ?? undefined }); }}
      >
        {isGenerating
          ? t('playground.studio_generating', { defaultValue: '生成中...' })
          : t('playground.studio_generate', { defaultValue: '生成' })}
      </button>
    </div>
  );
}
