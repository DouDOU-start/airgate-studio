import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useStudio } from '../StudioContext';
import { SizeSelector } from '../SizeSelector';
import { ModelPicker } from '../ModelPicker';
import { studioStyles as ss } from '../studioStyles';

const COUNT_OPTIONS = [1, 2, 3, 4];

export function TextToImagePanel() {
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
  const [count, setCount] = useState(1);

  const canGenerate = prompt.trim().length > 0;

  const handleGenerate = () => {
    const trimmed = prompt.trim();
    if (!trimmed || isGenerating) return;
    const fireAll = async () => {
      for (let i = 0; i < count; i++) {
        generate(trimmed, { mode: 'text2img', count: 1 });
      }
    };
    void fireAll();
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
      <div style={ss.formRow}>
        <label style={ss.formLabel}>
          {t('playground.studio_prompt', { defaultValue: '提示词' })}
        </label>
        <textarea
          style={ss.formTextarea}
          className="studio-textarea"
          value={prompt}
          onChange={e => setPrompt(e.target.value)}
          placeholder={t('playground.studio_prompt_placeholder', { defaultValue: '描述你想生成的图片...' })}
          rows={4}
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

      <div style={ss.formRow}>
        <label style={ss.formLabel}>
          {t('playground.studio_count', { defaultValue: '数量' })}
        </label>
        <div style={ss.formCountGroup}>
          {COUNT_OPTIONS.map(n => (
            <button
              key={n}
              type="button"
              style={count === n ? ss.formCountBtnActive : ss.formCountBtn}
              className={count === n ? 'studio-count-active' : 'studio-count-btn'}
              onClick={() => setCount(n)}
            >
              {n}
            </button>
          ))}
        </div>
      </div>

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
