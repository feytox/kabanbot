import { useQuery } from '@tanstack/react-query';
import { api, kindLabel } from '../api';
import { useNav } from '../nav';
import { Button, Cell, ErrorText, Loading, Section } from '../ui';

export function Home({ tab }: { tab: 'chats' | 'models' }) {
  const { replace } = useNav();
  return (
    <>
      <div className="tabs" role="tablist">
        <button
          type="button"
          role="tab"
          aria-selected={tab === 'chats'}
          onClick={() => replace({ name: 'home', tab: 'chats' })}
        >
          Группы
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={tab === 'models'}
          onClick={() => replace({ name: 'home', tab: 'models' })}
        >
          Мои модели
        </button>
      </div>
      {tab === 'chats' ? <Chats /> : <Providers />}
    </>
  );
}

function Chats() {
  const { push } = useNav();
  const chats = useQuery({ queryKey: ['chats'], queryFn: api.chats });

  if (chats.isPending) return <Loading />;
  if (chats.isError) return <ErrorText error={chats.error} />;
  if (chats.data.length === 0) {
    return (
      <p className="placeholder">
        Здесь появятся группы, где есть бот и где вы администратор. Добавьте бота в группу и напишите там любое
        сообщение.
      </p>
    );
  }
  return (
    <Section title="Группы" footer="Показаны группы, где вы администратор.">
      {chats.data.map((c) => (
        <Cell
          key={c.id}
          title={c.title || `Чат ${c.id}`}
          subtitle={
            !c.enabled
              ? 'Бот выключен'
              : c.summary_model
                ? `Модель: ${c.summary_model.display_name}`
                : 'Модель по умолчанию'
          }
          onClick={() => push({ name: 'chat', id: c.id })}
        />
      ))}
    </Section>
  );
}

function Providers() {
  const { push } = useNav();
  const providers = useQuery({ queryKey: ['providers'], queryFn: api.providers });

  if (providers.isPending) return <Loading />;
  if (providers.isError) return <ErrorText error={providers.error} />;
  return (
    <>
      {providers.data.length === 0 && (
        <p className="placeholder">
          Подключите свой API-ключ, чтобы бот работал в ваших группах на выбранной вами модели. Ключ хранится
          зашифрованным, и его не видит никто, включая других администраторов.
        </p>
      )}
      {providers.data.map((p) => (
        <Section key={p.id} title={`${p.name} · ${kindLabel(p.kind)}${p.shared ? ' · общий' : ''}`}>
          <Cell
            title="Настройки провайдера"
            subtitle={p.key_hint ? `Ключ ••••${p.key_hint}` : 'Ключ сохранён'}
            onClick={() => push({ name: 'provider', id: p.id })}
          />
          {p.models.map((m) => (
            <Cell
              key={m.id}
              title={m.display_name}
              subtitle={
                m.chats.length > 0 ? `${m.name} · групп: ${m.chats.length}` : m.name
              }
              onClick={() => push({ name: 'model', providerId: p.id, id: m.id })}
            />
          ))}
          <Cell title="+ Добавить модель" onClick={() => push({ name: 'model', providerId: p.id, id: null })} />
        </Section>
      ))}
      <div className="actions">
        <Button onClick={() => push({ name: 'provider', id: null })}>Подключить провайдера</Button>
      </div>
    </>
  );
}
