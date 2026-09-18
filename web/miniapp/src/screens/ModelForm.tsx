import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { api, type Model, type ModelInput } from '../api';
import { useNav } from '../nav';
import { Button, Cell, ErrorText, Field, Loading, Section, confirm } from '../ui';

export function ModelScreen({ providerId, id }: { providerId: number; id: number | null }) {
  const providers = useQuery({ queryKey: ['providers'], queryFn: api.providers });
  if (providers.isPending) return <Loading />;
  if (providers.isError) return <ErrorText error={providers.error} />;
  const provider = providers.data.find((p) => p.id === providerId);
  const model = id === null ? null : provider?.models.find((m) => m.id === id);
  if (!provider || (id !== null && !model)) return <ErrorText error="Модель не найдена" />;
  return <ModelForm providerId={providerId} providerName={provider.name} model={model ?? null} />;
}

function ModelForm({ providerId, providerName, model }: { providerId: number; providerName: string; model: Model | null }) {
  const qc = useQueryClient();
  const { back } = useNav();
  const [name, setName] = useState(model?.name ?? '');
  const [displayName, setDisplayName] = useState(model?.display_name ?? '');
  const [temperature, setTemperature] = useState(model?.temperature?.toString() ?? '');
  const [maxTokens, setMaxTokens] = useState(model?.max_tokens ? String(model.max_tokens) : '');

  const input = (): ModelInput => ({
    name,
    display_name: displayName,
    temperature: temperature.trim() === '' ? null : Number(temperature.replace(',', '.')),
    max_tokens: maxTokens.trim() === '' ? 0 : Math.trunc(Number(maxTokens)),
  });

  const invalidate = () =>
    Promise.all([
      qc.invalidateQueries({ queryKey: ['providers'] }),
      qc.invalidateQueries({ queryKey: ['usable-models'] }),
      qc.invalidateQueries({ queryKey: ['chats'] }),
    ]);
  const save = useMutation({
    mutationFn: () => (model ? api.updateModel(model.id, input()) : api.createModel(providerId, input())),
    onSuccess: async () => {
      await invalidate();
      back();
    },
  });
  const remove = useMutation({
    mutationFn: () => api.deleteModel(model!.id),
    onSuccess: async () => {
      await invalidate();
      back();
    },
  });
  const test = useMutation({ mutationFn: () => api.testModel(model!.id) });
  const unbind = useMutation({
    mutationFn: (chatId: number) => api.unbindModel(model!.id, chatId),
    onSuccess: invalidate,
  });

  return (
    <>
      <h1 className="title">{model ? model.display_name : 'Новая модель'}</h1>
      <p className="subtitle">{providerName}</p>
      <Section>
        <Field label="ID модели" hint="Как в документации провайдера, например google/gemini-3.5-flash.">
          <input value={name} maxLength={128} autoCapitalize="off" onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label="Название" hint="Так модель будет подписана в списках. По умолчанию — ID модели.">
          <input value={displayName} maxLength={64} onChange={(e) => setDisplayName(e.target.value)} />
        </Field>
      </Section>
      <Section title="Параметры" footer="Пустое значение — настройка провайдера по умолчанию.">
        <Field label="Temperature (0–2)">
          <input value={temperature} inputMode="decimal" placeholder="по умолчанию" onChange={(e) => setTemperature(e.target.value)} />
        </Field>
        <Field
          label="Лимит токенов ответа"
          hint="У «думающих» моделей в лимит входят и размышления, поэтому маленький лимит может оставить пустой ответ."
        >
          <input value={maxTokens} inputMode="numeric" placeholder="без лимита" onChange={(e) => setMaxTokens(e.target.value)} />
        </Field>
      </Section>

      {model && (
        <Section title="Проверка">
          <Cell
            title={test.isPending ? 'Отправляю запрос…' : 'Проверить модель'}
            subtitle={
              test.data
                ? `${test.data.reply} (${(test.data.latency_ms / 1000).toFixed(1)} с)`
                : test.error
                  ? `Ошибка: ${test.error.message}`
                  : 'Отправит короткий запрос с вашим ключом'
            }
            onClick={test.isPending ? undefined : () => test.mutate()}
          />
        </Section>
      )}

      {model && model.chats.length > 0 && (
        <Section title="Используется в группах" footer="Можно отключить модель от группы, даже если её выбрал другой администратор.">
          {model.chats.map((c) => (
            <Cell
              key={c.id}
              title={c.title || `Чат ${c.id}`}
              after={
                <button
                  type="button"
                  className="link danger"
                  disabled={unbind.isPending}
                  onClick={async () => {
                    if (await confirm(`Отключить модель от «${c.title}»?`, 'Отключить')) unbind.mutate(c.id);
                  }}
                >
                  Отключить
                </button>
              }
            />
          ))}
        </Section>
      )}

      <ErrorText error={save.error ?? remove.error ?? unbind.error} />
      <div className="actions">
        <Button disabled={save.isPending} onClick={() => save.mutate()}>
          {save.isPending ? 'Сохранение…' : model ? 'Сохранить' : 'Добавить модель'}
        </Button>
        {model && (
          <Button
            variant="danger"
            disabled={remove.isPending}
            onClick={async () => {
              if (await confirm('Удалить модель? Группы, где она выбрана, перейдут на модель по умолчанию.', 'Удалить')) {
                remove.mutate();
              }
            }}
          >
            Удалить модель
          </Button>
        )}
      </div>
    </>
  );
}
