import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { api, providerKinds, type Provider, type ProviderInput } from '../api';
import { useNav } from '../nav';
import { Button, ErrorText, Field, Loading, Section, Toggle, confirm } from '../ui';

export function ProviderScreen({ id }: { id: number | null }) {
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const providers = useQuery({ queryKey: ['providers'], queryFn: api.providers, enabled: id !== null });

  if (me.isPending || (id !== null && providers.isPending)) return <Loading />;
  if (me.isError) return <ErrorText error={me.error} />;
  const provider = id === null ? null : providers.data?.find((p) => p.id === id);
  if (id !== null && !provider) return <ErrorText error="Провайдер не найден" />;
  return <ProviderForm provider={provider ?? null} isOwner={me.data.is_owner} />;
}

function ProviderForm({ provider, isOwner }: { provider: Provider | null; isOwner: boolean }) {
  const qc = useQueryClient();
  const { back, replace } = useNav();
  const [form, setForm] = useState<ProviderInput>({
    kind: provider?.kind ?? 'openrouter',
    name: provider?.name ?? '',
    base_url: provider?.base_url ?? '',
    api_key: '',
    shared: provider?.shared ?? false,
  });
  const set = <K extends keyof ProviderInput>(k: K, v: ProviderInput[K]) => setForm((f) => ({ ...f, [k]: v }));

  const save = useMutation({
    mutationFn: () => (provider ? api.updateProvider(provider.id, form) : api.createProvider(form)),
    onSuccess: async (saved) => {
      await qc.invalidateQueries({ queryKey: ['providers'] });
      void qc.invalidateQueries({ queryKey: ['usable-models'] });
      if (provider) back();
      // A new provider is useless without a model, so go straight to adding one.
      else replace({ name: 'model', providerId: saved.id, id: null });
    },
  });
  const remove = useMutation({
    mutationFn: () => api.deleteProvider(provider!.id),
    onSuccess: async () => {
      await qc.invalidateQueries();
      back();
    },
  });

  const kind = providerKinds.find((k) => k.kind === form.kind)!;
  const urlChanged = provider !== null && form.base_url.trim() !== provider.base_url;

  return (
    <>
      <h1 className="title">{provider ? provider.name : 'Новый провайдер'}</h1>
      <Section>
        <Field label="Тип API">
          <select value={form.kind} disabled={provider !== null} onChange={(e) => set('kind', e.target.value as ProviderInput['kind'])}>
            {providerKinds.map((k) => (
              <option key={k.kind} value={k.kind}>
                {k.label}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Название">
          <input value={form.name} maxLength={64} placeholder="Например, «Мой OpenRouter»" onChange={(e) => set('name', e.target.value)} />
        </Field>
        <Field
          label="Адрес API"
          hint={form.kind === 'openai' ? 'Для OpenAI можно оставить пустым. Для других совместимых API укажите адрес.' : 'Оставьте пустым, чтобы использовать адрес по умолчанию.'}
        >
          <input
            value={form.base_url}
            inputMode="url"
            placeholder={kind.baseUrlHint}
            onChange={(e) => set('base_url', e.target.value)}
          />
        </Field>
        <Field
          label="API-ключ"
          hint={
            provider
              ? urlChanged
                ? 'При смене адреса ключ нужно ввести заново.'
                : `Сохранён ключ ••••${provider.key_hint || '????'}. Оставьте поле пустым, чтобы не менять.`
              : 'Ключ шифруется и никому не показывается, даже вам.'
          }
        >
          <input
            type="password"
            autoComplete="off"
            value={form.api_key}
            onChange={(e) => set('api_key', e.target.value)}
          />
        </Field>
      </Section>
      {isOwner && (
        <Section footer="Общими моделями могут пользоваться администраторы любых групп.">
          <Toggle label="Общий провайдер" checked={form.shared} onChange={(v) => set('shared', v)} />
        </Section>
      )}

      <ErrorText error={save.error ?? remove.error} />
      <div className="actions">
        <Button disabled={save.isPending} onClick={() => save.mutate()}>
          {save.isPending ? 'Сохранение…' : provider ? 'Сохранить' : 'Подключить'}
        </Button>
        {provider && (
          <Button
            variant="danger"
            disabled={remove.isPending}
            onClick={async () => {
              if (await confirm('Удалить провайдера вместе со всеми его моделями? Группы перейдут на модель по умолчанию.', 'Удалить')) {
                remove.mutate();
              }
            }}
          >
            Удалить провайдера
          </Button>
        )}
      </div>
    </>
  );
}
