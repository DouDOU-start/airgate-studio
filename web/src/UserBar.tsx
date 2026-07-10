import { useState, type CSSProperties } from 'react';
import { cssVar } from '@doudou-start/airgate-theme';
import { api, type UserInfo } from './api';

const styles: Record<string, CSSProperties> = {
  bar: {
    position: 'fixed',
    top: 10,
    right: 14,
    zIndex: 1000,
    display: 'flex',
    alignItems: 'center',
    gap: 10,
    padding: '5px 6px 5px 12px',
    borderRadius: 999,
    background: cssVar('glass'),
    backdropFilter: 'blur(16px) saturate(1.2)',
    WebkitBackdropFilter: 'blur(16px) saturate(1.2)',
    border: `1px solid ${cssVar('glassBorder')}`,
    boxShadow: '0 4px 20px rgba(0, 0, 0, 0.25)',
    color: cssVar('textSecondary'),
    fontFamily: cssVar('fontSans'),
    fontSize: 12,
    userSelect: 'none',
  },
  username: {
    fontWeight: 600,
    color: cssVar('text'),
    maxWidth: 140,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
  balance: {
    fontFamily: cssVar('fontMono'),
    fontSize: 11,
    color: cssVar('textTertiary'),
    whiteSpace: 'nowrap',
  },
  logout: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 4,
    padding: '4px 10px',
    borderRadius: 999,
    border: `1px solid ${cssVar('borderSubtle')}`,
    background: 'transparent',
    color: cssVar('textTertiary'),
    cursor: 'pointer',
    fontSize: 11,
    fontFamily: 'inherit',
    transition: 'all 0.15s',
  },
};

function formatBalance(balance: number | null): string | null {
  if (balance === null || balance === undefined || Number.isNaN(balance)) return null;
  return `$${balance.toFixed(2)}`;
}

// UserBar 顶部极简用户区：用户名 / 余额 / 退出。
export function UserBar({ user }: { user: UserInfo }) {
  const [loggingOut, setLoggingOut] = useState(false);
  const balanceLabel = formatBalance(user.balance);

  const handleLogout = async () => {
    if (loggingOut) return;
    setLoggingOut(true);
    try {
      await api.logout();
    } finally {
      // 退出后回到登录入口（core 侧会话仍在时会静默重新登录，属预期 SSO 行为）
      window.location.href = '/auth/login';
    }
  };

  return (
    <div style={styles.bar}>
      <span style={styles.username} title={user.email || user.username}>
        {user.username || user.email || `用户 #${user.airgate_user_id}`}
      </span>
      {balanceLabel && <span style={styles.balance} title="可用余额">{balanceLabel}</span>}
      {!user.api_key_ready && (
        <span style={{ ...styles.balance, color: cssVar('danger') }} title="未能领取 API Key，生成任务将失败">
          Key 不可用
        </span>
      )}
      <button type="button" style={styles.logout} className="studio-gallery-action" onClick={handleLogout}>
        {loggingOut ? '退出中...' : '退出'}
      </button>
    </div>
  );
}
