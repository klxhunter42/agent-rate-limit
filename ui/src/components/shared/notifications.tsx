import { createContext, useContext, useState, useCallback, useEffect, useRef, type ReactNode } from 'react';
import { cn } from '@/lib/utils';
import { Info, CheckCircle, AlertTriangle, XCircle, X } from 'lucide-react';

interface Toast {
  id: string;
  title: string;
  message?: string;
  type: 'info' | 'success' | 'warning' | 'error';
  duration?: number;
}

type ToastInput = Omit<Toast, 'id'>;

interface NotificationContextValue {
  notify: (toast: ToastInput) => void;
  dismiss: (id: string) => void;
}

const NotificationContext = createContext<NotificationContextValue>({
  notify: () => {},
  dismiss: () => {},
});

let toastCounter = 0;

function ToastItem({ toast, onDismiss }: { toast: Toast; onDismiss: (id: string) => void }) {
  const [progress, setProgress] = useState(100);
  const duration = toast.duration ?? 5000;
  const startTime = useRef(Date.now());
  const rafRef = useRef<number>(0);

  useEffect(() => {
    startTime.current = Date.now();

    function animate() {
      const elapsed = Date.now() - startTime.current;
      const remaining = Math.max(0, 100 - (elapsed / duration) * 100);
      setProgress(remaining);
      if (remaining > 0) {
        rafRef.current = requestAnimationFrame(animate);
      } else {
        onDismiss(toast.id);
      }
    }

    rafRef.current = requestAnimationFrame(animate);
    return () => cancelAnimationFrame(rafRef.current);
  }, [duration, toast.id, onDismiss]);

  const icons: Record<Toast['type'], typeof Info> = {
    info: Info,
    success: CheckCircle,
    warning: AlertTriangle,
    error: XCircle,
  };

  const colorMap: Record<Toast['type'], string> = {
    info: 'border-l-blue-500',
    success: 'border-l-green-500',
    warning: 'border-l-amber-500',
    error: 'border-l-red-500',
  };

  const iconColorMap: Record<Toast['type'], string> = {
    info: 'text-blue-500',
    success: 'text-green-500',
    warning: 'text-amber-500',
    error: 'text-red-500',
  };

  const Icon = icons[toast.type];

  return (
    <div
      className={cn(
        'relative flex items-start gap-3 rounded-lg border border-border border-l-4 bg-popover p-4 shadow-lg animate-in slide-in-from-right-full duration-300',
        colorMap[toast.type]
      )}
    >
      <Icon className={cn('h-5 w-5 shrink-0 mt-0.5', iconColorMap[toast.type])} />
      <div className="flex-1 min-w-0">
        <p className="text-sm font-medium text-popover-foreground">{toast.title}</p>
        {toast.message && (
          <p className="mt-1 text-xs text-muted-foreground">{toast.message}</p>
        )}
      </div>
      <button
        onClick={() => onDismiss(toast.id)}
        className="shrink-0 rounded-md p-1 text-muted-foreground hover:text-popover-foreground hover:bg-muted transition-colors"
      >
        <X className="h-3.5 w-3.5" />
      </button>
      <div className="absolute bottom-0 left-0 h-0.5 rounded-b-lg bg-current opacity-20 transition-all" style={{ width: `${progress}%` }} />
    </div>
  );
}

export function ToastContainer() {
  const { toasts, dismiss } = useNotificationInternal();

  return (
    <div className="fixed bottom-4 right-4 z-[200] flex flex-col gap-2 w-full max-w-sm pointer-events-none">
      {toasts.map((t) => (
        <div key={t.id} className="pointer-events-auto">
          <ToastItem toast={t} onDismiss={dismiss} />
        </div>
      ))}
    </div>
  );
}

interface InternalNotificationContextValue extends NotificationContextValue {
  toasts: Toast[];
}

const InternalNotificationContext = createContext<InternalNotificationContextValue>({
  notify: () => {},
  dismiss: () => {},
  toasts: [],
});

function useNotificationInternal() {
  return useContext(InternalNotificationContext);
}

export function NotificationProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);

  const notify = useCallback((input: ToastInput) => {
    const id = `toast-${++toastCounter}`;
    setToasts((prev) => {
      const next = [...prev, { ...input, id }];
      return next.slice(-5);
    });
  }, []);

  const dismiss = useCallback((id: string) => {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);

  return (
    <NotificationContext.Provider value={{ notify, dismiss }}>
      <InternalNotificationContext.Provider value={{ notify, dismiss, toasts }}>
        {children}
      </InternalNotificationContext.Provider>
    </NotificationContext.Provider>
  );
}

export function useNotification(): NotificationContextValue {
  return useContext(NotificationContext);
}
