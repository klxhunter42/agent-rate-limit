import { createContext, useContext, useState, useEffect, type ReactNode } from 'react';

interface PrivacyContextValue {
  privacyMode: boolean;
  togglePrivacyMode: () => void;
}

const PrivacyContext = createContext<PrivacyContextValue>({
  privacyMode: false,
  togglePrivacyMode: () => {},
});

export function PrivacyProvider({ children }: { children: ReactNode }) {
  const [privacyMode, setPrivacyMode] = useState(() => {
    const stored = localStorage.getItem('arl-privacy-mode');
    return stored === 'true';
  });

  useEffect(() => {
    localStorage.setItem('arl-privacy-mode', String(privacyMode));
  }, [privacyMode]);

  return (
    <PrivacyContext.Provider value={{ privacyMode, togglePrivacyMode: () => setPrivacyMode(!privacyMode) }}>
      {children}
    </PrivacyContext.Provider>
  );
}

export function usePrivacy() {
  return useContext(PrivacyContext);
}
