import { popup } from '@tma.js/sdk-react';
import type { ReactNode } from 'react';

export function Section({ title, footer, children }: { title?: string; footer?: ReactNode; children: ReactNode }) {
  return (
    <section className="section">
      {title && <h2 className="section-title">{title}</h2>}
      <div className="section-body">{children}</div>
      {footer && <p className="section-footer">{footer}</p>}
    </section>
  );
}

export function Cell({
  title,
  subtitle,
  after,
  onClick,
}: {
  title: ReactNode;
  subtitle?: ReactNode;
  after?: ReactNode;
  onClick?: () => void;
}) {
  const content = (
    <>
      <span className="cell-main">
        <span className="cell-title">{title}</span>
        {subtitle && <span className="cell-subtitle">{subtitle}</span>}
      </span>
      {after && <span className="cell-after">{after}</span>}
    </>
  );
  return onClick ? (
    <button type="button" className="cell cell-button" onClick={onClick}>
      {content}
      <span className="chevron" aria-hidden>
        ›
      </span>
    </button>
  ) : (
    <div className="cell">{content}</div>
  );
}

export function Toggle({
  label,
  hint,
  checked,
  disabled,
  onChange,
}: {
  label: string;
  hint?: string;
  checked: boolean;
  disabled?: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <label className={`cell toggle${disabled ? ' disabled' : ''}`}>
      <span className="cell-main">
        <span className="cell-title">{label}</span>
        {hint && <span className="cell-subtitle">{hint}</span>}
      </span>
      <input
        type="checkbox"
        role="switch"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onChange(e.target.checked)}
      />
    </label>
  );
}

export function Radio({
  name,
  label,
  hint,
  checked,
  onChange,
}: {
  name: string;
  label: ReactNode;
  hint?: ReactNode;
  checked: boolean;
  onChange: () => void;
}) {
  return (
    <label className="cell radio">
      <input type="radio" name={name} checked={checked} onChange={onChange} />
      <span className="cell-main">
        <span className="cell-title">{label}</span>
        {hint && <span className="cell-subtitle">{hint}</span>}
      </span>
    </label>
  );
}

export function Field({ label, hint, children }: { label: string; hint?: ReactNode; children: ReactNode }) {
  return (
    <label className="field">
      <span className="field-label">{label}</span>
      {children}
      {hint && <span className="field-hint">{hint}</span>}
    </label>
  );
}

export function Button({
  children,
  variant = 'primary',
  disabled,
  onClick,
}: {
  children: ReactNode;
  variant?: 'primary' | 'secondary' | 'danger';
  disabled?: boolean;
  onClick: () => void;
}) {
  return (
    <button type="button" className={`button button-${variant}`} disabled={disabled} onClick={onClick}>
      {children}
    </button>
  );
}

export function ErrorText({ error }: { error: unknown }) {
  if (!error) return null;
  return <p className="error">{error instanceof Error ? error.message : String(error)}</p>;
}

export function Loading() {
  return <p className="placeholder">Загрузка…</p>;
}

/** Asks for confirmation with Telegram's native popup, falling back to the browser dialog. */
export async function confirm(message: string, action: string): Promise<boolean> {
  if (popup.show.isAvailable()) {
    const id = await popup.show({
      message,
      buttons: [
        { id: 'ok', type: 'destructive', text: action },
        { type: 'cancel' },
      ],
    });
    return id === 'ok';
  }
  return window.confirm(message);
}
