import { createContext, useContext, useState, useEffect, type ReactNode } from 'react';
import { t as translate, getCurrentLang, setLang as saveLang, type Lang } from '@/lib/i18n';

interface LanguageContextValue {
  lang: Lang;
  setLang: (lang: Lang) => void;
  t: (key: string) => string;
}

const LanguageContext = createContext<LanguageContextValue>({
  lang: 'en',
  setLang: () => {},
  t: (key) => key,
});

export function LanguageProvider({ children }: { children: ReactNode }) {
  const [lang, setLangState] = useState<Lang>(getCurrentLang);

  const setLang = (l: Lang) => {
    saveLang(l);
    setLangState(l);
    window.dispatchEvent(new CustomEvent('arl:lang-changed', { detail: l }));
  };

  useEffect(() => {
    const handler = (e: Event) => {
      const l = (e as CustomEvent).detail as Lang;
      if (l) setLangState(l);
    };
    window.addEventListener('arl:lang-changed', handler);
    return () => window.removeEventListener('arl:lang-changed', handler);
  }, []);

  const value: LanguageContextValue = {
    lang,
    setLang,
    t: (key: string) => translate(key, lang),
  };

  return (
    <LanguageContext.Provider value={value}>
      {children}
    </LanguageContext.Provider>
  );
}

export function useLanguage(): LanguageContextValue {
  return useContext(LanguageContext);
}
