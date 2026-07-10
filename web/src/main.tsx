import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { injectThemeStyle, setTheme, getStoredTheme } from '@doudou-start/airgate-theme';
import App from './App';

// 独立 SPA：主题 CSS 变量由本应用自行注入（插件时代由 core 壳层提供）。
injectThemeStyle();
const theme = getStoredTheme();
setTheme(theme);
document.documentElement.classList.toggle('light', theme === 'light');
document.documentElement.classList.toggle('dark', theme === 'dark');

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
