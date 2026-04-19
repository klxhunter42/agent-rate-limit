import { createContext, useContext, useState, useEffect, useCallback, type ReactNode } from 'react';
import * as authApi from '@/lib/auth-api';

interface AuthContextValue {
  isAuthenticated: boolean;
  isLoading: boolean;
  login: (apiKey: string) => Promise<boolean>;
  logout: () => Promise<void>;
  checkAuth: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue>({
  isAuthenticated: false,
  isLoading: true,
  login: async () => false,
  logout: async () => {},
  checkAuth: async () => {},
});

export function AuthProvider({ children }: { children: ReactNode }) {
  const [isAuthenticated, setIsAuthenticated] = useState(false);
  const [isLoading, setIsLoading] = useState(true);

  const check = useCallback(async () => {
    try {
      const res = await authApi.checkAuth();
      setIsAuthenticated(res.authenticated);
    } catch {
      setIsAuthenticated(false);
    } finally {
      setIsLoading(false);
    }
  }, []);

  const loginFn = useCallback(async (apiKey: string): Promise<boolean> => {
    try {
      const res = await authApi.login(apiKey);
      if (res.success) {
        setIsAuthenticated(true);
        return true;
      }
      return false;
    } catch {
      return false;
    }
  }, []);

  const logoutFn = useCallback(async () => {
    try {
      await authApi.logout();
    } finally {
      setIsAuthenticated(false);
    }
  }, []);

  useEffect(() => {
    check();
  }, [check]);

  return (
    <AuthContext.Provider value={{ isAuthenticated, isLoading, login: loginFn, logout: logoutFn, checkAuth: check }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  return useContext(AuthContext);
}
