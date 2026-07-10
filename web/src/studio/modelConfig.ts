// 模型注册表（动态）：模型列表来自后端 /api/models（透传 core /v1/models，
// 每条带非标 protocols 数组），不再维护硬编码 MODEL_REGISTRY。
// 本文件只放纯函数：
//   - isLikelyImageModel   图像模型过滤启发式（漏判可手动输入模型名兜底）
//   - resolveImageStrategy 执行策略判定（镜像后端 strategy.go 的 resolveStrategy）
//   - modelCapabilities    按策略推导 UI 能力（size/quality/图生图控件显隐）

export interface SizeOption {
  value: string;
  label: string;
  tier: string;
  aspect?: string;
}

export interface QualityOption {
  value: string;
  label: string;
}

/** 执行策略（与后端 strategy.go 一一对应；edits/generations 由后端按有无输入图再分流）。 */
export type ImageStrategy = 'imagen-predict' | 'gemini-content' | 'openai-images' | 'chat';

export interface ModelCapabilities {
  strategy: ImageStrategy;
  /** size 参数是否原生生效（images 端点 / predict aspectRatio 映射）。 */
  supportsSize: boolean;
  supportsQuality: boolean;
  /** 是否支持图生图/局部重绘输入图。 */
  supportsImageInput: boolean;
  sizes: SizeOption[];
  qualities: QualityOption[];
  defaultSize: string;
}

/** UI 使用的模型条目（id + 协议 + 推导能力）。 */
export interface ImageModel {
  id: string;
  name: string;
  protocols: string[];
  caps: ModelCapabilities;
}

// ── 纯函数：名称归一 / 启发式 ────────────────────────────────────────────────

/** 去掉厂商前缀（"openai/gpt-image-1" → "gpt-image-1"）并小写。 */
function bareModelName(id: string): string {
  const lower = id.trim().toLowerCase();
  const i = lower.lastIndexOf('/');
  return i >= 0 ? lower.slice(i + 1) : lower;
}

/** 图像模型命名模式（命中即认为可生图）。 */
const IMAGE_MODEL_PATTERNS: RegExp[] = [
  /image/, // gpt-image-*、gemini-*-image、*-image-* …
  /^imagen/,
  /^dall-?e/,
  /^flux/,
  /^stable-diffusion/,
  /^sd(xl|3)/,
  /seedream/,
  /seededit/,
  /kolors/,
  /cogview/,
  /midjourney|^mj[-_]/,
  /janus/,
];

/** 明显不是生图模型的关键词（嵌入/审核/语音等），优先排除。 */
const NON_IMAGE_KEYWORDS = /embed|moderation|rerank|whisper|tts|audio|transcribe|realtime/;

/** 图像模型过滤启发式：按模型名判断是否可能支持生图。 */
export function isLikelyImageModel(id: string): boolean {
  const name = bareModelName(id);
  if (!name || NON_IMAGE_KEYWORDS.test(name)) return false;
  return IMAGE_MODEL_PATTERNS.some(p => p.test(name));
}

/** 协议启发式兜底（镜像后端 guessProtocolsByModelName）：目录没给 protocols 时按前缀猜。 */
function guessProtocolsByModelName(id: string): string[] {
  const name = bareModelName(id);
  if (name.startsWith('imagen') || name.startsWith('gemini') || name.startsWith('veo')) {
    return ['gemini'];
  }
  if (name.startsWith('claude')) return ['anthropic'];
  return ['openai'];
}

// ── 纯函数：执行策略判定（镜像后端 resolveStrategy） ────────────────────────

export function resolveImageStrategy(id: string, protocols: string[]): ImageStrategy {
  const name = bareModelName(id);
  const has = (p: string) => protocols.some(x => x.trim().toLowerCase() === p);
  if (has('gemini') && name.startsWith('imagen')) return 'imagen-predict';
  if (has('gemini')) return 'gemini-content';
  if (has('openai') && (name.startsWith('gpt-image') || name.startsWith('dall-e') || name.startsWith('dalle'))) {
    return 'openai-images';
  }
  return 'chat';
}

// ── 尺寸 / 质量预设 ──────────────────────────────────────────────────────────

const GPT_IMAGE_SIZES: SizeOption[] = [
  { value: 'auto', label: 'Auto', tier: '1K' },
  { value: '1024x1024', label: '1024×1024', tier: '1K', aspect: '1:1' },
  { value: '1536x1024', label: '1536×1024', tier: '1K', aspect: '3:2' },
  { value: '1024x1536', label: '1024×1536', tier: '1K', aspect: '2:3' },
];

const DALLE3_SIZES: SizeOption[] = [
  { value: '1024x1024', label: '1024×1024', tier: '1K', aspect: '1:1' },
  { value: '1792x1024', label: '1792×1024', tier: '2K', aspect: '7:4' },
  { value: '1024x1792', label: '1024×1792', tier: '2K', aspect: '4:7' },
];

const DALLE2_SIZES: SizeOption[] = [
  { value: '256x256', label: '256×256', tier: '低清', aspect: '1:1' },
  { value: '512x512', label: '512×512', tier: '低清', aspect: '1:1' },
  { value: '1024x1024', label: '1024×1024', tier: '1K', aspect: '1:1' },
];

/** Imagen predict 的尺寸以宽高比表达（后端映射为 parameters.aspectRatio）。 */
const IMAGEN_ASPECT_SIZES: SizeOption[] = [
  { value: '1:1', label: '1:1 方形', tier: '宽高比', aspect: '1:1' },
  { value: '4:3', label: '4:3 横向', tier: '宽高比', aspect: '4:3' },
  { value: '3:4', label: '3:4 纵向', tier: '宽高比', aspect: '3:4' },
  { value: '16:9', label: '16:9 宽屏', tier: '宽高比', aspect: '16:9' },
  { value: '9:16', label: '9:16 竖屏', tier: '宽高比', aspect: '9:16' },
];

const GPT_IMAGE_QUALITIES: QualityOption[] = [
  { value: 'auto', label: 'Auto' },
  { value: 'low', label: '低' },
  { value: 'medium', label: '中' },
  { value: 'high', label: '高' },
];

const DALLE3_QUALITIES: QualityOption[] = [
  { value: 'standard', label: '标准' },
  { value: 'hd', label: 'HD' },
];

/** Imagen quality 档位（后端映射为 parameters.sampleImageSize）。 */
const IMAGEN_QUALITIES: QualityOption[] = [
  { value: '1k', label: '1K' },
  { value: '2k', label: '2K' },
];

// ── 纯函数：能力推导 ─────────────────────────────────────────────────────────

const NO_PARAM_CAPS = (strategy: ImageStrategy): ModelCapabilities => ({
  strategy,
  supportsSize: false,
  supportsQuality: false,
  supportsImageInput: true,
  sizes: [],
  qualities: [],
  defaultSize: '',
});

/** 按策略推导模型能力：决定 size/quality/参考图控件的显隐与选项。 */
export function modelCapabilities(id: string, protocols: string[]): ModelCapabilities {
  const strategy = resolveImageStrategy(id, protocols);
  const name = bareModelName(id);
  switch (strategy) {
    case 'imagen-predict':
      // Imagen predict 无图生图语义 → 不收输入图
      return {
        strategy,
        supportsSize: true,
        supportsQuality: true,
        supportsImageInput: false,
        sizes: IMAGEN_ASPECT_SIZES,
        qualities: IMAGEN_QUALITIES,
        defaultSize: '1:1',
      };
    case 'openai-images': {
      if (name.startsWith('dall-e-3')) {
        // dall-e-3 无 edits 端点 → 不收输入图
        return {
          strategy,
          supportsSize: true,
          supportsQuality: true,
          supportsImageInput: false,
          sizes: DALLE3_SIZES,
          qualities: DALLE3_QUALITIES,
          defaultSize: '1024x1024',
        };
      }
      if (name.startsWith('dall-e-2') || name.startsWith('dalle-2')) {
        return {
          strategy,
          supportsSize: true,
          supportsQuality: false,
          supportsImageInput: true,
          sizes: DALLE2_SIZES,
          qualities: [],
          defaultSize: '1024x1024',
        };
      }
      // gpt-image 系
      return {
        strategy,
        supportsSize: true,
        supportsQuality: true,
        supportsImageInput: true,
        sizes: GPT_IMAGE_SIZES,
        qualities: GPT_IMAGE_QUALITIES,
        defaultSize: 'auto',
      };
    }
    case 'gemini-content':
      // generateContent 路径维持现状：无原生 size/quality 参数
      return NO_PARAM_CAPS(strategy);
    default:
      // chat 多模态回退：size/quality 不生效，不给控件
      return NO_PARAM_CAPS(strategy);
  }
}

/** 把（可能来自手动输入的）模型 id 组装成 UI 模型条目；无协议声明时启发式兜底。 */
export function toImageModel(id: string, protocols?: string[]): ImageModel {
  const proto = protocols && protocols.length > 0 ? protocols : guessProtocolsByModelName(id);
  return { id, name: id, protocols: proto, caps: modelCapabilities(id, proto) };
}

