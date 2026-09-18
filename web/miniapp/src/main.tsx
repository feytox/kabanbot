import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { backButton, init, isTMA, miniApp, retrieveLaunchParams, themeParams } from '@tma.js/sdk-react';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { NavProvider, useNav, type Route } from './nav';
import { ChatSettings } from './screens/ChatSettings';
import { Home } from './screens/Home';
import { ModelScreen } from './screens/ModelForm';
import { ProviderScreen } from './screens/ProviderForm';
import './styles.css';

function Screen() {
  const { route } = useNav();
  switch (route.name) {
    case 'home':
      return <Home tab={route.tab} />;
    case 'chat':
      return <ChatSettings key={route.id} id={route.id} />;
    case 'provider':
      return <ProviderScreen key={route.id ?? 'new'} id={route.id} />;
    case 'model':
      return <ModelScreen key={route.id ?? 'new'} providerId={route.providerId} id={route.id} />;
  }
}

/** The bot links to a group's settings with a "chat_<id>" start parameter. */
function initialRoutes(): Route[] {
  const home: Route = { name: 'home', tab: 'chats' };
  const param = retrieveLaunchParams().tgWebAppStartParam;
  const id = param?.startsWith('chat_') ? Number(param.slice('chat_'.length)) : NaN;
  return Number.isInteger(id) ? [home, { name: 'chat', id }] : [home];
}

function bootstrap(): Route[] | null {
  if (!isTMA()) return null;
  try {
    init();
    backButton.mount();
    themeParams.mount();
    themeParams.bindCssVars();
    miniApp.mount();
    miniApp.bindCssVars();
    miniApp.ready();
    return initialRoutes();
  } catch (e) {
    console.error('Mini App init failed', e);
    return null;
  }
}

const routes = bootstrap();
const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: false } },
});

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    {routes ? (
      <QueryClientProvider client={queryClient}>
        <NavProvider initial={routes}>
          <main className="app">
            <Screen />
          </main>
        </NavProvider>
      </QueryClientProvider>
    ) : (
      <p className="placeholder">Откройте настройки через бота в Telegram.</p>
    )}
  </StrictMode>,
);
