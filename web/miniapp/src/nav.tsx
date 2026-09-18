import { backButton } from '@tma.js/sdk-react';
import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';

export type Route =
  | { name: 'home'; tab: 'chats' | 'models' }
  | { name: 'chat'; id: number }
  | { name: 'provider'; id: number | null }
  | { name: 'model'; providerId: number; id: number | null };

interface Nav {
  route: Route;
  push: (r: Route) => void;
  back: () => void;
  replace: (r: Route) => void;
}

const NavContext = createContext<Nav | null>(null);

/** A tiny stack router wired to Telegram's native back button. */
export function NavProvider({ initial, children }: { initial: Route[]; children: ReactNode }) {
  const [stack, setStack] = useState<Route[]>(initial);

  const push = useCallback((r: Route) => setStack((s) => [...s, r]), []);
  const back = useCallback(() => setStack((s) => (s.length > 1 ? s.slice(0, -1) : s)), []);
  const replace = useCallback((r: Route) => setStack((s) => [...s.slice(0, -1), r]), []);

  useEffect(() => {
    if (!backButton.isMounted()) return;
    if (stack.length > 1) backButton.show();
    else backButton.hide();
  }, [stack.length]);

  useEffect(() => {
    if (!backButton.isMounted()) return;
    return backButton.onClick(back);
  }, [back]);

  const value = useMemo(() => ({ route: stack[stack.length - 1], push, back, replace }), [stack, push, back, replace]);
  return <NavContext value={value}>{children}</NavContext>;
}

export function useNav(): Nav {
  const nav = useContext(NavContext);
  if (!nav) throw new Error('useNav outside NavProvider');
  return nav;
}
