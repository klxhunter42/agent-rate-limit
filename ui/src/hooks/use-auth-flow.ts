import { useState, useCallback, useRef, useEffect } from 'react';
import * as authApi from '@/lib/auth-api';

interface AuthFlowState {
  isAuthenticating: boolean;
  provider: string | null;
  flowType: 'device_code' | 'auth_code' | null;
  userCode: string | null;
  verificationUrl: string | null;
  authUrl: string | null;
  oauthState: string | null;
  deviceCode: string | null;
  error: string | null;
  completed: boolean;
  completedAccount: authApi.AccountInfo | null;
  needsEmail: boolean;
}

const DEFAULT_STATE: AuthFlowState = {
  isAuthenticating: false,
  provider: null,
  flowType: null,
  userCode: null,
  verificationUrl: null,
  authUrl: null,
  oauthState: null,
  deviceCode: null,
  error: null,
  completed: false,
  completedAccount: null,
  needsEmail: false,
};

const DEVICE_CODE_PROVIDERS = ['copilot', 'qwen', 'codebuddy', 'kimi', 'cursor'];
const AUTH_CODE_PROVIDERS = ['gemini', 'gemini-oauth', 'claude-oauth'];
const POLL_INTERVAL_MS = 3000;

export function useAuthFlow() {
  const [state, setState] = useState<AuthFlowState>(DEFAULT_STATE);
  const pollRef = useRef<number | null>(null);
  const cancelRef = useRef(false);

  const stopPolling = useCallback(() => {
    if (pollRef.current) {
      clearInterval(pollRef.current);
      pollRef.current = null;
    }
  }, []);

  const pollUntilDone = useCallback(
    (provider: string, params: { state?: string; device_code?: string }) => {
      cancelRef.current = false;
      pollRef.current = window.setInterval(async () => {
        if (cancelRef.current) {
          stopPolling();
          return;
        }
        try {
          const result = await authApi.pollAuthStatus(provider, params);
          if (result.status === 'complete' && result.account) {
            stopPolling();
            const needsEmail = !result.account.email;
            setState((s) => ({
              ...s,
              isAuthenticating: false,
              completed: !needsEmail,
              needsEmail,
              completedAccount: result.account!,
              error: null,
            }));
          }
        } catch (e) {
          stopPolling();
          setState((s) => ({
            ...s,
            isAuthenticating: false,
            error: e instanceof Error ? e.message : 'poll failed',
          }));
        }
      }, POLL_INTERVAL_MS);
    },
    [stopPolling],
  );

  const startAuth = useCallback(
    async (provider: string) => {
      stopPolling();
      cancelRef.current = false;
      setState({
        ...DEFAULT_STATE,
        isAuthenticating: true,
        provider,
      });

      // Set flowType immediately so dialogs render even on error.
      const isDeviceCode = DEVICE_CODE_PROVIDERS.includes(provider);
      const expectedFlow = isDeviceCode ? 'device_code' : 'auth_code';

      try {
        if (isDeviceCode) {
          const info = await authApi.startDeviceAuth(provider);
          setState((s) => ({
            ...s,
            flowType: 'device_code',
            userCode: info.user_code,
            verificationUrl: info.verification_url,
            deviceCode: info.device_code,
          }));
          pollUntilDone(provider, { device_code: info.device_code });
        } else if (AUTH_CODE_PROVIDERS.includes(provider)) {
          const info = await authApi.startAuthCode(provider);
          setState((s) => ({
            ...s,
            flowType: 'auth_code',
            authUrl: info.auth_url,
            oauthState: info.state,
          }));
          window.open(info.auth_url, '_blank', 'noopener');
          pollUntilDone(provider, { state: info.state });
        } else {
          setState((s) => ({
            ...s,
            isAuthenticating: false,
            error: `unsupported provider: ${provider}`,
          }));
          return;
        }
      } catch (e) {
        setState((s) => ({
          ...s,
          flowType: expectedFlow as 'device_code' | 'auth_code',
          isAuthenticating: false,
          error: e instanceof Error ? e.message : 'start auth failed',
        }));
      }
    },
    [stopPolling, pollUntilDone],
  );

  const cancelAuth = useCallback(async () => {
    cancelRef.current = true;
    stopPolling();
    const { provider } = state;
    if (provider) {
      try {
        await authApi.cancelAuth(provider);
      } catch {
        // best-effort cancel
      }
    }
    setState(DEFAULT_STATE);
  }, [stopPolling, state.provider]);

  const submitCallback = useCallback(
    async (provider: string, redirectUrl: string) => {
      try {
        const url = new URL(redirectUrl);
        const stateParam = url.searchParams.get('state');
        const codeParam = url.searchParams.get('code');
        if (!stateParam || !codeParam) {
          setState((s) => ({ ...s, error: 'invalid callback URL: missing state or code' }));
          return;
        }
        // Pass the full redirect URL to the backend which handles the exchange
        const res = await fetch(`/v1/auth/${provider}/callback`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ redirect_url: redirectUrl, state: stateParam, code: codeParam }),
        });
        if (!res.ok) throw new Error(`callback: ${res.status}`);
        const result = (await res.json()) as { status: string; account?: authApi.AccountInfo };
        if (result.account) {
          stopPolling();
          const account = result.account;
          const needsEmail = !account.email;
          setState((s) => ({
            ...s,
            isAuthenticating: false,
            completed: !needsEmail,
            needsEmail,
            completedAccount: account,
            error: null,
          }));
        }
      } catch (e) {
        setState((s) => ({
          ...s,
          error: e instanceof Error ? e.message : 'callback failed',
        }));
      }
    },
    [stopPolling],
  );

  const submitEmail = useCallback(async (email: string) => {
    const { completedAccount, provider } = state;
    if (!completedAccount || !provider) return;
    try {
      await authApi.updateAccountEmail(provider, completedAccount.id, email);
      setState((s) => ({
        ...s,
        completedAccount: { ...s.completedAccount!, email },
        needsEmail: false,
        completed: true,
      }));
    } catch (e) {
      setState((s) => ({
        ...s,
        error: e instanceof Error ? e.message : 'failed to save email',
      }));
    }
  }, [state.completedAccount, state.provider]);

  const reset = useCallback(() => {
    cancelRef.current = true;
    stopPolling();
    setState(DEFAULT_STATE);
  }, [stopPolling]);

  useEffect(() => {
    return () => {
      cancelRef.current = true;
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, []);

  return {
    ...state,
    startAuth,
    cancelAuth,
    submitCallback,
    submitEmail,
    reset,
  };
}
