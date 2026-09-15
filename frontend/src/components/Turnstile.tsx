import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { TurnstileApi, TurnstileError, loadTurnstile } from '../turnstile';

/** How long getToken waits for the widget before giving up. */
export const TOKEN_TIMEOUT_MS = 60_000;

export interface TurnstileHandle {
  /**
   * Resolves with a fresh, unused token. Tokens are single-use: every token
   * is handed out at most once, and when the previous one has been spent the
   * widget is reset so Cloudflare runs a new challenge. Rejects with a
   * TurnstileError when the widget fails or does not answer in time.
   */
  getToken(): Promise<string>;
}

interface Props {
  siteKey: string;
  /** Action the token is minted for; the backend requires this exact value. */
  action: string;
}

interface Waiter {
  resolve: (token: string) => void;
  reject: (err: TurnstileError) => void;
  timer: ReturnType<typeof setTimeout>;
}

/**
 * Renders the Cloudflare Turnstile widget (explicit mode) and exposes the
 * token it produces through a ref, so the upload page can attach a fresh token
 * to every upload attempt (tokens are single-use, so a second attempt after a
 * rejection needs a new one).
 */
const Turnstile = forwardRef<TurnstileHandle, Props>(function Turnstile({ siteKey, action }, ref) {
  const { t, i18n } = useTranslation();
  const containerRef = useRef<HTMLDivElement>(null);
  const apiRef = useRef<TurnstileApi | undefined>(undefined);
  const widgetIdRef = useRef<string | undefined>(undefined);
  /** A token the widget produced that nobody has claimed yet. */
  const tokenRef = useRef<string | undefined>(undefined);
  /** True while a challenge is running and a callback is still to come. */
  const challengeRunningRef = useRef(false);
  const waitersRef = useRef<Waiter[]>([]);
  const failureRef = useRef<TurnstileError | undefined>(undefined);
  const [failure, setFailure] = useState<TurnstileError | undefined>();

  const takeWaiter = (): Waiter | undefined => {
    const waiter = waitersRef.current.shift();
    if (waiter) {
      clearTimeout(waiter.timer);
    }
    return waiter;
  };

  const rejectAll = (err: TurnstileError) => {
    for (let waiter = takeWaiter(); waiter; waiter = takeWaiter()) {
      waiter.reject(err);
    }
  };

  const startChallenge = () => {
    if (apiRef.current && widgetIdRef.current !== undefined) {
      challengeRunningRef.current = true;
      apiRef.current.reset(widgetIdRef.current);
    }
  };

  const fail = (err: TurnstileError) => {
    challengeRunningRef.current = false;
    failureRef.current = err;
    setFailure(err);
    rejectAll(err);
  };

  useEffect(() => {
    const container = containerRef.current;
    if (!container) {
      return;
    }
    let cancelled = false;
    failureRef.current = undefined;
    setFailure(undefined);
    tokenRef.current = undefined;
    // Rendering starts the first challenge; its callback is still to come.
    challengeRunningRef.current = true;

    loadTurnstile()
      .then((api) => {
        if (cancelled) {
          return;
        }
        apiRef.current = api;
        widgetIdRef.current = api.render(container, {
          sitekey: siteKey,
          action,
          language: i18n.language,
          size: 'flexible',
          callback: (token) => {
            challengeRunningRef.current = false;
            const waiter = takeWaiter();
            if (waiter) {
              waiter.resolve(token);
              // More callers waiting? They need tokens of their own.
              if (waitersRef.current.length > 0) {
                startChallenge();
              }
            } else {
              tokenRef.current = token;
            }
          },
          'expired-callback': () => {
            // An unused token expired; the widget refreshes it by itself.
            tokenRef.current = undefined;
            challengeRunningRef.current = true;
          },
          'timeout-callback': () => {
            // The interactive challenge was not completed in time.
            tokenRef.current = undefined;
            challengeRunningRef.current = false;
            if (waitersRef.current.length > 0) {
              startChallenge();
            }
          },
          'error-callback': (code) => {
            fail(new TurnstileError(code, `turnstile error ${code}`));
            return true; // handled: keeps the widget from logging to the console
          },
        });
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          fail(err instanceof TurnstileError ? err : new TurnstileError('script-load-failed'));
        }
      });

    return () => {
      cancelled = true;
      rejectAll(new TurnstileError('unmounted'));
      if (apiRef.current && widgetIdRef.current !== undefined) {
        try {
          apiRef.current.remove(widgetIdRef.current);
        } catch {
          // The widget may already be gone.
        }
      }
      widgetIdRef.current = undefined;
      tokenRef.current = undefined;
    };
  }, [siteKey, action, i18n.language]);

  useImperativeHandle(ref, () => ({
    getToken: () => {
      if (tokenRef.current) {
        const token = tokenRef.current;
        tokenRef.current = undefined;
        return Promise.resolve(token);
      }
      if (failureRef.current) {
        return Promise.reject(failureRef.current);
      }
      return new Promise<string>((resolve, reject) => {
        const timer = setTimeout(() => {
          waitersRef.current = waitersRef.current.filter((w) => w.timer !== timer);
          reject(new TurnstileError('timeout', 'the Turnstile widget did not produce a token in time'));
        }, TOKEN_TIMEOUT_MS);
        waitersRef.current.push({ resolve, reject, timer });
        // The previous token was spent and no challenge is running: ask
        // Cloudflare for a new one. While the script is still loading or the
        // first challenge is running, the callback arrives on its own.
        if (!challengeRunningRef.current) {
          startChallenge();
        }
      });
    },
  }), []);

  return (
    <div className="turnstile">
      <p className="turnstile-explanation">{t('upload_bot_check_explanation')}</p>
      <div ref={containerRef} className="turnstile-widget" data-testid="turnstile-widget" />
      {failure && (
        <p className="turnstile-error" role="alert">{t('error_bot_check_unavailable')}</p>
      )}
    </div>
  );
});

export default Turnstile;
