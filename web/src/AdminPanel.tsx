import { useCallback, useEffect, useState, type CSSProperties } from 'react';
import { cssVar } from '@doudou-start/airgate-theme';
import { api, type AdminGroup, type AdminModel } from './api';

// AdminPanel：管理控制台弹层——分组开关 + 按组同步/上架模型。
// 分组镜像来自管理员登录时自动收集（core userinfo.groups）；
// 模型同步用管理员本人在该组的 sk- key 拉 core /v1/models（后端完成，前端只触发）。

const s: Record<string, CSSProperties> = {
  overlay: {
    position: 'fixed',
    inset: 0,
    zIndex: 2000,
    background: 'rgba(0, 0, 0, 0.55)',
    backdropFilter: 'blur(4px)',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    padding: 24,
  },
  panel: {
    width: 'min(920px, 100%)',
    maxHeight: '85vh',
    display: 'flex',
    flexDirection: 'column',
    borderRadius: 16,
    background: cssVar('bgElevated'),
    border: `1px solid ${cssVar('glassBorder')}`,
    boxShadow: '0 24px 80px rgba(0, 0, 0, 0.5)',
    color: cssVar('text'),
    fontFamily: cssVar('fontSans'),
    overflow: 'hidden',
  },
  header: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '14px 20px',
    borderBottom: `1px solid ${cssVar('borderSubtle')}`,
  },
  title: { fontSize: 15, fontWeight: 700 },
  close: {
    padding: '4px 12px',
    borderRadius: 8,
    border: `1px solid ${cssVar('borderSubtle')}`,
    background: 'transparent',
    color: cssVar('textSecondary'),
    cursor: 'pointer',
    fontSize: 12,
    fontFamily: 'inherit',
  },
  body: { display: 'flex', minHeight: 0, flex: 1 },
  groupCol: {
    width: 260,
    flexShrink: 0,
    borderRight: `1px solid ${cssVar('borderSubtle')}`,
    overflowY: 'auto',
    padding: 10,
  },
  groupItem: {
    width: '100%',
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    padding: '10px 12px',
    borderRadius: 10,
    border: '1px solid transparent',
    background: 'transparent',
    color: cssVar('text'),
    cursor: 'pointer',
    textAlign: 'left',
    fontFamily: 'inherit',
    fontSize: 12,
  },
  groupItemActive: {
    background: cssVar('primarySubtle'),
    borderColor: `color-mix(in oklab, ${cssVar('primary')} 30%, transparent)`,
  },
  groupMeta: { fontSize: 10, color: cssVar('textTertiary') },
  modelCol: { flex: 1, minWidth: 0, overflowY: 'auto', padding: 14 },
  toolRow: { display: 'flex', alignItems: 'center', gap: 10, marginBottom: 12 },
  syncBtn: {
    padding: '6px 14px',
    borderRadius: 8,
    border: 'none',
    background: cssVar('primary'),
    color: cssVar('primaryForeground'),
    cursor: 'pointer',
    fontSize: 12,
    fontWeight: 600,
    fontFamily: 'inherit',
  },
  hint: { fontSize: 11, color: cssVar('textTertiary') },
  table: { width: '100%', borderCollapse: 'collapse', fontSize: 12 },
  th: {
    textAlign: 'left',
    padding: '6px 8px',
    color: cssVar('textTertiary'),
    fontWeight: 500,
    fontSize: 11,
    borderBottom: `1px solid ${cssVar('borderSubtle')}`,
  },
  td: { padding: '6px 8px', borderBottom: `1px solid ${cssVar('borderSubtle')}`, verticalAlign: 'middle' },
  nameInput: {
    width: '100%',
    boxSizing: 'border-box',
    padding: '5px 8px',
    borderRadius: 6,
    border: `1px solid ${cssVar('borderSubtle')}`,
    background: cssVar('bgDeep'),
    color: cssVar('text'),
    fontSize: 12,
    fontFamily: 'inherit',
    outline: 'none',
  },
  missing: {
    display: 'inline-block',
    padding: '1px 6px',
    borderRadius: 999,
    fontSize: 10,
    color: cssVar('danger'),
    border: `1px solid color-mix(in oklab, ${cssVar('danger')} 40%, transparent)`,
    marginLeft: 6,
  },
  error: { padding: '8px 12px', fontSize: 12, color: cssVar('danger') },
};

// switchStyle 简易开关（复选框语义），跟随主题色。
function Toggle({ checked, onChange, disabled }: { checked: boolean; onChange: (v: boolean) => void; disabled?: boolean }) {
  return (
    <button
      type="button"
      onClick={() => !disabled && onChange(!checked)}
      style={{
        width: 34,
        height: 18,
        borderRadius: 999,
        border: 'none',
        cursor: disabled ? 'not-allowed' : 'pointer',
        background: checked ? cssVar('primary') : cssVar('borderSubtle'),
        position: 'relative',
        transition: 'background 0.15s',
        flexShrink: 0,
        opacity: disabled ? 0.5 : 1,
      }}
      aria-pressed={checked}
    >
      <span
        style={{
          position: 'absolute',
          top: 2,
          left: checked ? 18 : 2,
          width: 14,
          height: 14,
          borderRadius: '50%',
          background: '#fff',
          transition: 'left 0.15s',
        }}
      />
    </button>
  );
}

export function AdminPanel({ onClose }: { onClose: () => void }) {
  const [adminGroups, setAdminGroups] = useState<AdminGroup[]>([]);
  const [activeGroupId, setActiveGroupId] = useState(0);
  const [models, setModels] = useState<AdminModel[]>([]);
  const [syncing, setSyncing] = useState(false);
  const [error, setError] = useState('');

  const loadGroups = useCallback(async () => {
    try {
      const list = await api.adminListGroups();
      setAdminGroups(list);
      setActiveGroupId(prev => (list.some(g => g.core_group_id === prev) ? prev : (list[0]?.core_group_id ?? 0)));
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载分组失败');
    }
  }, []);

  const loadModels = useCallback(async (groupID: number) => {
    if (groupID <= 0) {
      setModels([]);
      return;
    }
    try {
      setModels(await api.adminListModels(groupID));
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载货架失败');
    }
  }, []);

  useEffect(() => { void loadGroups(); }, [loadGroups]);
  useEffect(() => { void loadModels(activeGroupId); }, [activeGroupId, loadModels]);

  const toggleGroup = async (g: AdminGroup, enabled: boolean) => {
    setError('');
    try {
      await api.adminSetGroupEnabled(g.core_group_id, enabled);
      setAdminGroups(prev => prev.map(item =>
        item.core_group_id === g.core_group_id ? { ...item, enabled } : item));
    } catch (err) {
      setError(err instanceof Error ? err.message : '更新分组失败');
    }
  };

  const syncModels = async () => {
    if (activeGroupId <= 0 || syncing) return;
    setError('');
    setSyncing(true);
    try {
      await api.adminSyncModels(activeGroupId);
      await loadModels(activeGroupId);
    } catch (err) {
      setError(err instanceof Error ? err.message : '同步失败');
    } finally {
      setSyncing(false);
    }
  };

  const patchModel = async (m: AdminModel, patch: { display_name?: string; enabled?: boolean; sort_order?: number }) => {
    setError('');
    try {
      const updated = await api.adminUpdateModel(m.id, patch);
      setModels(prev => prev.map(item => (item.id === m.id ? updated : item)));
    } catch (err) {
      setError(err instanceof Error ? err.message : '更新模型失败');
    }
  };

  const activeGroup = adminGroups.find(g => g.core_group_id === activeGroupId);

  return (
    <div style={s.overlay} onClick={onClose}>
      <div style={s.panel} onClick={e => e.stopPropagation()}>
        <div style={s.header}>
          <span style={s.title}>管理控制台 · 分组与模型货架</span>
          <button type="button" style={s.close} onClick={onClose}>关闭</button>
        </div>
        {error && <div style={s.error}>{error}</div>}
        <div style={s.body}>
          <div style={s.groupCol}>
            {adminGroups.length === 0 && (
              <div style={{ ...s.hint, padding: 10 }}>
                暂无分组镜像。分组在管理员登录时自动收集（来自 core 的可用分组），
                如刚被加入新分组请重新登录。
              </div>
            )}
            {adminGroups.map(g => (
              <button
                key={g.core_group_id}
                type="button"
                onClick={() => setActiveGroupId(g.core_group_id)}
                style={{ ...s.groupItem, ...(g.core_group_id === activeGroupId ? s.groupItemActive : {}) }}
              >
                <span style={{ flex: 1, minWidth: 0 }}>
                  <span style={{ display: 'block', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {g.name}
                  </span>
                  <span style={s.groupMeta}>×{g.rate_multiplier}{g.note ? ` · ${g.note}` : ''}</span>
                </span>
                <Toggle checked={g.enabled} onChange={v => void toggleGroup(g, v)} />
              </button>
            ))}
          </div>
          <div style={s.modelCol}>
            <div style={s.toolRow}>
              <button type="button" style={s.syncBtn} onClick={() => void syncModels()} disabled={syncing || activeGroupId <= 0}>
                {syncing ? '同步中...' : '从 core 同步模型'}
              </button>
              <span style={s.hint}>
                {activeGroup ? `分组「${activeGroup.name}」` : ''}新同步的模型默认下架；core 已下线的会标记漂移
              </span>
            </div>
            <table style={s.table}>
              <thead>
                <tr>
                  <th style={{ ...s.th, width: 60 }}>上架</th>
                  <th style={s.th}>模型</th>
                  <th style={{ ...s.th, width: 220 }}>展示名</th>
                  <th style={{ ...s.th, width: 90 }}>协议</th>
                </tr>
              </thead>
              <tbody>
                {models.map(m => (
                  <tr key={m.id}>
                    <td style={s.td}>
                      <Toggle checked={m.enabled} onChange={v => void patchModel(m, { enabled: v })} />
                    </td>
                    <td style={{ ...s.td, fontFamily: cssVar('fontMono'), fontSize: 11 }}>
                      {m.model_name}
                      {m.missing_at_core && <span style={s.missing}>core 已下线</span>}
                    </td>
                    <td style={s.td}>
                      <input
                        style={s.nameInput}
                        defaultValue={m.display_name}
                        placeholder={m.model_name}
                        onBlur={e => {
                          const next = e.target.value.trim();
                          if (next !== m.display_name) void patchModel(m, { display_name: next });
                        }}
                      />
                    </td>
                    <td style={{ ...s.td, fontSize: 10, color: cssVar('textTertiary') }}>{m.protocols.join('/')}</td>
                  </tr>
                ))}
                {models.length === 0 && (
                  <tr>
                    <td style={s.td} colSpan={4}>
                      <span style={s.hint}>暂无模型，点击「从 core 同步模型」拉取该分组可用模型</span>
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </div>
  );
}
