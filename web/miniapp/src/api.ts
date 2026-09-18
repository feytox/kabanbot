import { retrieveRawInitData } from '@tma.js/sdk-react';

export type ProviderKind = 'openai' | 'openrouter' | 'gemini';

export interface Me {
  id: number;
  first_name: string;
  username?: string;
  is_owner: boolean;
}

export interface ChatRef {
  id: number;
  title: string;
}

export interface Model {
  id: number;
  provider_id: number;
  name: string;
  display_name: string;
  temperature: number | null;
  max_tokens: number;
  chats: ChatRef[];
}

export interface Provider {
  id: number;
  kind: ProviderKind;
  name: string;
  base_url: string;
  key_hint: string;
  shared: boolean;
  models: Model[];
}

export interface ModelOption {
  id: number;
  display_name: string;
  name: string;
  provider_name: string;
  provider_kind: ProviderKind;
  owner_name: string;
  is_mine: boolean;
  shared: boolean;
}

export interface Features {
  summary: boolean;
  mention_all: boolean;
}

export interface Chat {
  id: number;
  title: string;
  enabled: boolean;
  features: Features;
  summary_model: ModelOption | null;
}

export interface ProviderInput {
  kind: ProviderKind;
  name: string;
  base_url: string;
  api_key: string;
  shared: boolean;
}

export interface ModelInput {
  name: string;
  display_name: string;
  temperature: number | null;
  max_tokens: number;
}

export interface ChatInput {
  enabled: boolean;
  features: Features;
  summary_model_id: number | null;
}

export interface TestResult {
  reply: string;
  latency_ms: number;
}

export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

let initData: string | undefined;

function authHeader(): string {
  initData ??= retrieveRawInitData();
  if (!initData) throw new ApiError(401, 'Откройте приложение из Telegram');
  return `tma ${initData}`;
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`/api${path}`, {
    method,
    headers: {
      Authorization: authHeader(),
      ...(body === undefined ? {} : { 'Content-Type': 'application/json' }),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (res.status === 204) return undefined as T;
  const data: unknown = await res.json().catch(() => null);
  if (!res.ok) {
    const message =
      data && typeof data === 'object' && 'error' in data ? String(data.error) : `Ошибка ${res.status}`;
    throw new ApiError(res.status, message);
  }
  return data as T;
}

export const api = {
  me: () => request<Me>('GET', '/me'),

  providers: () => request<Provider[]>('GET', '/providers'),
  createProvider: (in_: ProviderInput) => request<Provider>('POST', '/providers', in_),
  updateProvider: (id: number, in_: ProviderInput) => request<Provider>('PUT', `/providers/${id}`, in_),
  deleteProvider: (id: number) => request<void>('DELETE', `/providers/${id}`),

  createModel: (providerId: number, in_: ModelInput) =>
    request<Model>('POST', `/providers/${providerId}/models`, in_),
  updateModel: (id: number, in_: ModelInput) => request<Model>('PUT', `/models/${id}`, in_),
  deleteModel: (id: number) => request<void>('DELETE', `/models/${id}`),
  testModel: (id: number) => request<TestResult>('POST', `/models/${id}/test`),
  unbindModel: (id: number, chatId: number) => request<void>('DELETE', `/models/${id}/chats/${chatId}`),
  usableModels: () => request<ModelOption[]>('GET', '/models/usable'),

  chats: () => request<Chat[]>('GET', '/chats'),
  chat: (id: number) => request<Chat>('GET', `/chats/${id}`),
  updateChat: (id: number, in_: ChatInput) => request<Chat>('PUT', `/chats/${id}`, in_),
};

export const providerKinds: { kind: ProviderKind; label: string; baseUrlHint: string }[] = [
  { kind: 'openai', label: 'OpenAI-совместимый', baseUrlHint: 'https://api.openai.com/v1' },
  { kind: 'openrouter', label: 'OpenRouter', baseUrlHint: 'https://openrouter.ai/api/v1' },
  { kind: 'gemini', label: 'Gemini API', baseUrlHint: 'https://generativelanguage.googleapis.com' },
];

export function kindLabel(kind: ProviderKind): string {
  return providerKinds.find((k) => k.kind === kind)?.label ?? kind;
}
