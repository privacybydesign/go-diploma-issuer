/**
 * Loader and typings for the Cloudflare Turnstile client API.
 *
 * The script is loaded once, on demand, in explicit-render mode so the
 * Turnstile component controls where and when the widget appears and can
 * reset it between uploads (tokens are single-use).
 * See https://developers.cloudflare.com/turnstile/get-started/client-side-rendering/
 */

export const TURNSTILE_SCRIPT_URL = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit';

export interface TurnstileRenderOptions {
  sitekey: string;
  /** Echoed by siteverify; the backend requires the action it expects. */
  action?: string;
  language?: string;
  theme?: 'light' | 'dark' | 'auto';
  size?: 'normal' | 'flexible' | 'compact';
  callback?: (token: string) => void;
  'expired-callback'?: () => void;
  'error-callback'?: (errorCode: string) => boolean | void;
  'timeout-callback'?: () => void;
}

export interface TurnstileApi {
  render(container: HTMLElement | string, options: TurnstileRenderOptions): string | undefined;
  reset(widgetId?: string): void;
  remove(widgetId?: string): void;
  getResponse(widgetId?: string): string | undefined;
}

declare global {
  interface Window {
    turnstile?: TurnstileApi;
  }
}

/** Thrown when the widget cannot deliver a token. */
export class TurnstileError extends Error {
  code: string;

  constructor(code: string, message?: string) {
    super(message ?? `turnstile: ${code}`);
    this.name = 'TurnstileError';
    this.code = code;
  }
}

let loading: Promise<TurnstileApi> | undefined;

/**
 * Loads the Turnstile script (once) and resolves with its API. Rejects with a
 * TurnstileError when the script cannot be loaded, e.g. because a content
 * blocker stops challenges.cloudflare.com.
 */
export function loadTurnstile(): Promise<TurnstileApi> {
  if (window.turnstile) {
    return Promise.resolve(window.turnstile);
  }
  if (!loading) {
    loading = new Promise<TurnstileApi>((resolve, reject) => {
      const fail = () => {
        loading = undefined;
        reject(new TurnstileError('script-load-failed', 'the Turnstile script could not be loaded'));
      };
      const script = document.createElement('script');
      script.src = TURNSTILE_SCRIPT_URL;
      script.async = true;
      script.defer = true;
      script.onload = () => {
        if (window.turnstile) {
          resolve(window.turnstile);
        } else {
          fail();
        }
      };
      script.onerror = fail;
      document.head.appendChild(script);
    });
  }
  return loading;
}
