import { useEffect } from 'react';

const PAGES = ['/', '/system-health', '/model-limits', '/key-pool', '/analytics', '/metrics', '/controls', '/privacy', '/settings'];

function isEditable(el: EventTarget | null): boolean {
  if (!el || !(el instanceof HTMLElement)) return false;
  const tag = el.tagName;
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true;
  if (el.isContentEditable) return true;
  return false;
}

export function useKeyboardShortcuts() {
  useEffect(() => {
    function handler(e: KeyboardEvent) {
      const mod = e.metaKey || e.ctrlKey;

      // Cmd/Ctrl + K: toggle command palette
      if (mod && e.key === 'k') {
        e.preventDefault();
        window.dispatchEvent(new CustomEvent('arl:toggle-palette'));
        return;
      }

      // Cmd/Ctrl + R: refresh data
      if (mod && e.key === 'r') {
        e.preventDefault();
        window.location.reload();
        return;
      }

      // Cmd/Ctrl + B: toggle sidebar
      if (mod && e.key === 'b') {
        e.preventDefault();
        window.dispatchEvent(new CustomEvent('arl:toggle-sidebar'));
        return;
      }

      // Cmd/Ctrl + P: toggle privacy mode
      if (mod && e.key === 'p') {
        e.preventDefault();
        window.dispatchEvent(new CustomEvent('arl:toggle-privacy'));
        return;
      }

      // Cmd/Ctrl + ,: open settings
      if (mod && e.key === ',') {
        e.preventDefault();
        window.dispatchEvent(new CustomEvent('arl:open-settings'));
        return;
      }

      // Escape: close any open modal/palette
      if (e.key === 'Escape' && !mod) {
        window.dispatchEvent(new CustomEvent('arl:escape'));
        return;
      }

      // Number keys 1-9: navigate to page (only when not in input/textarea)
      if (!mod && !e.altKey && !e.shiftKey && !isEditable(e.target)) {
        const num = parseInt(e.key);
        if (num >= 1 && num <= 9 && num <= PAGES.length) {
          const target = PAGES[num - 1] ?? '/';
          if (window.location.pathname !== target) {
            window.location.href = target;
          }
        }
      }
    }

    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, []);
}
