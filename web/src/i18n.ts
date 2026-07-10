import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';

// 独立部署下没有 core 提供的共享 i18n 实例。
// 组件里所有 t(key, { defaultValue }) 都自带中文兜底文案，
// 这里只需初始化一个空资源实例让 defaultValue 生效。
void i18n.use(initReactI18next).init({
  lng: 'zh',
  fallbackLng: 'zh',
  resources: {},
  interpolation: { escapeValue: false },
  returnEmptyString: false,
});

export default i18n;
