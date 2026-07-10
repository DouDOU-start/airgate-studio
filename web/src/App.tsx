import { useEffect, useState, type CSSProperties } from 'react';
import { cssVar } from '@doudou-start/airgate-theme';
import { api, type UserInfo } from './api';
import StudioPage from './StudioPage';
import { UserBar } from './UserBar';

const bootStyles: Record<string, CSSProperties> = {
  screen: {
    position: 'fixed',
    inset: 0,
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 12,
    background: cssVar('bgDeep'),
    color: cssVar('textTertiary'),
    fontFamily: cssVar('fontSans'),
    fontSize: 13,
  },
  spinner: {
    width: 28,
    height: 28,
    borderRadius: '50%',
    border: `2px solid ${cssVar('borderSubtle')}`,
    borderTopColor: cssVar('primary'),
    animation: 'studio-boot-spin 0.8s linear infinite',
  },
  retry: {
    padding: '6px 16px',
    borderRadius: 8,
    border: `1px solid ${cssVar('borderSubtle')}`,
    background: 'transparent',
    color: cssVar('textSecondary'),
    cursor: 'pointer',
    fontSize: 12,
    fontFamily: 'inherit',
  },
};

const bootCSS = `@keyframes studio-boot-spin { to { transform: rotate(360deg); } }`;

// App 入口：先拉会话用户信息；未登录（401）由 api 层跳转 /auth/login 走单点登录。
export default function App() {
  const [user, setUser] = useState<UserInfo | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    api.getUserInfo()
      .then(info => { if (active) setUser(info); })
      .catch(err => {
        // 401 时 api 层已发起跳转；这里只处理其他错误（后端未启动等）
        const msg = err instanceof Error ? err.message : String(err);
        if (active && msg !== 'unauthorized') setError(msg);
      });
    return () => { active = false; };
  }, []);

  if (error) {
    return (
      <div style={bootStyles.screen}>
        <style>{bootCSS}</style>
        <div>加载用户信息失败：{error}</div>
        <button type="button" style={bootStyles.retry} onClick={() => window.location.reload()}>重试</button>
      </div>
    );
  }
  if (!user) {
    return (
      <div style={bootStyles.screen}>
        <style>{bootCSS}</style>
        <div style={bootStyles.spinner} />
        <div>正在进入创作中心...</div>
      </div>
    );
  }

  return (
    <>
      <StudioPage />
      <UserBar user={user} />
    </>
  );
}
