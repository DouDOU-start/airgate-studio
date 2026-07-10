package studio

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// AssetStore 本地磁盘资产存储。
//
// 生成产物写入 {DataDir}/assets/generated/<uuid><ext>，
// 对外经 GET /assets-runtime/generated/<file> 提供访问。
type AssetStore struct {
	dir string // {DataDir}/assets/generated 的绝对/相对路径
}

// assetURLPrefix 生成产物的公开 URL 前缀。
const assetURLPrefix = "/assets-runtime/generated/"

// assetFileNamePattern 合法资产文件名：uuid + 已知扩展名（防路径穿越的第一道闸）。
// 图片之外预留视频/音频类型，供后续 video/music 模态产物落盘；
// 视频/音频经 http.ServeFile 提供，天然支持 Range 拖动播放。
var assetFileNamePattern = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}\.(png|jpg|jpeg|webp|gif|mp4|webm|mp3|wav)$`)

// NewAssetStore 创建资产存储并确保目录存在。
func NewAssetStore(dataDir string) (*AssetStore, error) {
	dir := filepath.Join(dataDir, "assets", "generated")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建资产目录失败: %w", err)
	}
	return &AssetStore{dir: dir}, nil
}

// SaveGenerated 落盘一个生成产物（图片/视频/音频），返回公开 URL（/assets-runtime/generated/<file>）。
func (s *AssetStore) SaveGenerated(data []byte, ext string) (string, error) {
	ext = normalizeAssetExt(ext)
	name := uuid.NewString() + ext
	if err := os.WriteFile(filepath.Join(s.dir, name), data, 0o644); err != nil {
		return "", fmt.Errorf("写入资产文件失败: %w", err)
	}
	return assetURLPrefix + name, nil
}

// Delete 按公开 URL 删除资产文件；非本存储管理的 URL 忽略（不报错）。
func (s *AssetStore) Delete(publicURL string) error {
	name, ok := s.fileNameFromURL(publicURL)
	if !ok {
		return nil
	}
	err := os.Remove(filepath.Join(s.dir, name))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除资产文件失败: %w", err)
	}
	return nil
}

// ReadGenerated 按公开 URL 读取本存储管理的资产文件（图生图输入用），返回内容与 MIME。
func (s *AssetStore) ReadGenerated(publicURL string) ([]byte, string, error) {
	name, ok := s.fileNameFromURL(publicURL)
	if !ok {
		return nil, "", fmt.Errorf("非本存储管理的资产 URL: %.64s", publicURL)
	}
	data, err := os.ReadFile(filepath.Join(s.dir, name))
	if err != nil {
		return nil, "", fmt.Errorf("读取资产文件失败: %w", err)
	}
	return data, mimeFromExt(filepath.Ext(name)), nil
}

// ServeFile 处理 GET /assets-runtime/generated/{file}；严格校验文件名防路径穿越。
func (s *AssetStore) ServeFile(w http.ResponseWriter, r *http.Request, name string) {
	if !assetFileNamePattern.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(s.dir, name)
	// 双保险：Join 后仍必须位于资产目录内。
	if rel, err := filepath.Rel(s.dir, path); err != nil || strings.HasPrefix(rel, "..") {
		http.NotFound(w, r)
		return
	}
	// 生成产物不可变，可长缓存。
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeFile(w, r, path)
}

// fileNameFromURL 从公开 URL 提取合法文件名。
func (s *AssetStore) fileNameFromURL(publicURL string) (string, bool) {
	if !strings.HasPrefix(publicURL, assetURLPrefix) {
		return "", false
	}
	name := strings.TrimPrefix(publicURL, assetURLPrefix)
	if !assetFileNamePattern.MatchString(name) {
		return "", false
	}
	return name, true
}

// normalizeAssetExt 归一化扩展名；未知类型回退 .png（图片为默认模态）。
func normalizeAssetExt(ext string) string {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext != "" && !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	switch ext {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif",
		".mp4", ".webm", ".mp3", ".wav":
		return ext
	default:
		return ".png"
	}
}

// mimeFromExt 资产扩展名 → MIME（extFromMIME 的反向映射；未知回退 png）。
func mimeFromExt(ext string) string {
	switch strings.ToLower(strings.TrimSpace(ext)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	default:
		return "image/png"
	}
}

// canonicalImageMIME 把任意（可能带参数或未知的）MIME 归一到受支持的图片 MIME；
// 未知类型回退 image/png。
func canonicalImageMIME(mime string) string {
	return mimeFromExt(extFromMIME(mime))
}

// parseDataURI 解析 data:<mime>;base64,<payload> 形态的 data URI。
func parseDataURI(uri string) (mime string, data []byte, err error) {
	if !strings.HasPrefix(uri, "data:") {
		return "", nil, fmt.Errorf("非 data URI")
	}
	rest := strings.TrimPrefix(uri, "data:")
	comma := strings.Index(rest, ",")
	if comma < 0 {
		return "", nil, fmt.Errorf("data URI 缺少逗号分隔符")
	}
	meta, payload := rest[:comma], rest[comma+1:]
	if !strings.HasSuffix(meta, ";base64") {
		return "", nil, fmt.Errorf("仅支持 base64 编码的 data URI")
	}
	mime = strings.TrimSuffix(meta, ";base64")
	data, err = base64.StdEncoding.DecodeString(payload)
	if err != nil {
		// 兼容 URL-safe base64
		data, err = base64.URLEncoding.DecodeString(payload)
		if err != nil {
			return "", nil, fmt.Errorf("解码 data URI 失败: %w", err)
		}
	}
	return mime, data, nil
}

// extFromMIME 从 MIME 类型推断资产扩展名（mimeFromExt 的反向映射；未知回退 .png）。
func extFromMIME(mime string) string {
	mime = strings.ToLower(strings.TrimSpace(mime))
	if i := strings.Index(mime, ";"); i >= 0 {
		mime = strings.TrimSpace(mime[:i])
	}
	switch mime {
	case "image/png":
		return ".png"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/wav", "audio/x-wav":
		return ".wav"
	default:
		return ".png"
	}
}
