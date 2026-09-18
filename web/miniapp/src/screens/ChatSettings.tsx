import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { api, kindLabel, type Chat, type ChatInput, type ModelOption } from '../api';
import { Button, ErrorText, Loading, Radio, Section, Toggle } from '../ui';

export function ChatSettings({ id }: { id: number }) {
  const chat = useQuery({ queryKey: ['chat', id], queryFn: () => api.chat(id) });
  const models = useQuery({ queryKey: ['usable-models'], queryFn: api.usableModels });

  if (chat.isPending || models.isPending) return <Loading />;
  if (chat.isError) return <ErrorText error={chat.error} />;
  if (models.isError) return <ErrorText error={models.error} />;
  return <ChatForm chat={chat.data} usable={models.data} />;
}

function toInput(c: Chat): ChatInput {
  return { enabled: c.enabled, features: { ...c.features }, summary_model_id: c.summary_model?.id ?? null };
}

function modelHint(m: ModelOption): string {
  const owner = m.is_mine ? 'ваша' : m.shared ? 'общая' : `подключил ${m.owner_name}`;
  return `${kindLabel(m.provider_kind)} · ${m.name} · ${owner}`;
}

function ChatForm({ chat, usable }: { chat: Chat; usable: ModelOption[] }) {
  const qc = useQueryClient();
  const [form, setForm] = useState<ChatInput>(() => toInput(chat));
  useEffect(() => setForm(toInput(chat)), [chat]);

  const save = useMutation({
    mutationFn: (in_: ChatInput) => api.updateChat(chat.id, in_),
    onSuccess: (updated) => {
      qc.setQueryData(['chat', chat.id], updated);
      void qc.invalidateQueries({ queryKey: ['chats'] });
      void qc.invalidateQueries({ queryKey: ['providers'] });
    },
  });

  // The bound model may belong to another admin and so be missing from the usable list.
  const options = [...usable];
  if (chat.summary_model && !usable.some((m) => m.id === chat.summary_model?.id)) {
    options.unshift(chat.summary_model);
  }
  const dirty = JSON.stringify(form) !== JSON.stringify(toInput(chat));
  const setFeature = (key: keyof ChatInput['features'], v: boolean) =>
    setForm((f) => ({ ...f, features: { ...f.features, [key]: v } }));

  return (
    <>
      <h1 className="title">{chat.title || `Чат ${chat.id}`}</h1>

      <Section>
        <Toggle
          label="Бот включён"
          hint="Выключенный бот продолжает запоминать сообщения, но не отвечает."
          checked={form.enabled}
          onChange={(v) => setForm((f) => ({ ...f, enabled: v }))}
        />
      </Section>

      <Section title="Функции">
        <Toggle
          label="Пересказы /summary"
          checked={form.features.summary}
          disabled={!form.enabled}
          onChange={(v) => setFeature('summary', v)}
        />
        <Toggle
          label="Упоминание всех по @all"
          checked={form.features.mention_all}
          disabled={!form.enabled}
          onChange={(v) => setFeature('mention_all', v)}
        />
      </Section>

      <Section
        title="Модель для пересказов"
        footer="Подключить можно свою или общую модель. Ключи и адреса чужих моделей не видны."
      >
        <Radio
          name="model"
          label="Модель по умолчанию"
          hint="Настроена владельцем бота"
          checked={form.summary_model_id === null}
          onChange={() => setForm((f) => ({ ...f, summary_model_id: null }))}
        />
        {options.map((m) => (
          <Radio
            key={m.id}
            name="model"
            label={m.display_name}
            hint={modelHint(m)}
            checked={form.summary_model_id === m.id}
            onChange={() => setForm((f) => ({ ...f, summary_model_id: m.id }))}
          />
        ))}
      </Section>

      <ErrorText error={save.error} />
      <div className="actions sticky">
        <Button disabled={!dirty || save.isPending} onClick={() => save.mutate(form)}>
          {save.isPending ? 'Сохранение…' : save.isSuccess && !dirty ? 'Сохранено' : 'Сохранить'}
        </Button>
      </div>
    </>
  );
}
