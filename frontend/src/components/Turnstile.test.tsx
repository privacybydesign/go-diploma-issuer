import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, render, screen, waitFor } from '@testing-library/react';
import { createRef } from 'react';
import '../i18n';
import { TurnstileApi, TurnstileError, TurnstileRenderOptions, loadTurnstile } from '../turnstile';
import Turnstile, { TOKEN_TIMEOUT_MS, TurnstileHandle } from './Turnstile';

vi.mock('../turnstile', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../turnstile')>();
  return { ...actual, loadTurnstile: vi.fn() };
});

const loadMock = vi.mocked(loadTurnstile);

/** A scripted Turnstile client: the test fires the callbacks itself. */
function fakeTurnstileApi() {
  let options: TurnstileRenderOptions | undefined;
  const api: TurnstileApi & { options: () => TurnstileRenderOptions; solve: (token: string) => void } = {
    render: vi.fn((_container, opts) => {
      options = opts;
      return 'widget-1';
    }),
    reset: vi.fn(),
    remove: vi.fn(),
    getResponse: vi.fn(),
    options: () => options!,
    solve: (token: string) => options!.callback?.(token),
  };
  return api;
}

function renderWidget() {
  const ref = createRef<TurnstileHandle>();
  render(<Turnstile ref={ref} siteKey="0x4AAAAAAEskYIZQOLbu1QvE" action="diploma-upload" />);
  return ref;
}

describe('Turnstile', () => {
  let api: ReturnType<typeof fakeTurnstileApi>;

  beforeEach(() => {
    api = fakeTurnstileApi();
    loadMock.mockResolvedValue(api);
  });

  afterEach(() => {
    cleanup();
    loadMock.mockReset();
    vi.useRealTimers();
  });

  it('renders the widget with the sitekey and action the backend expects', async () => {
    renderWidget();
    await waitFor(() => expect(api.render).toHaveBeenCalledTimes(1));
    expect(api.render).toHaveBeenCalledWith(screen.getByTestId('turnstile-widget'), expect.objectContaining({
      sitekey: '0x4AAAAAAEskYIZQOLbu1QvE',
      action: 'diploma-upload',
    }));
    expect(screen.getByText(/Cloudflare Turnstile/)).toBeInTheDocument();
  });

  it('hands out a token that arrived before anyone asked, once', async () => {
    const ref = renderWidget();
    await waitFor(() => expect(api.render).toHaveBeenCalled());
    act(() => api.solve('token-1'));

    await expect(ref.current!.getToken()).resolves.toBe('token-1');

    // The token is spent: the next request resets the widget and waits.
    const next = ref.current!.getToken();
    expect(api.reset).toHaveBeenCalledWith('widget-1');
    act(() => api.solve('token-2'));
    await expect(next).resolves.toBe('token-2');
  });

  it('waits for the running challenge instead of restarting it', async () => {
    const ref = renderWidget();
    await waitFor(() => expect(api.render).toHaveBeenCalled());

    const pending = ref.current!.getToken();
    expect(api.reset).not.toHaveBeenCalled();
    act(() => api.solve('token-1'));
    await expect(pending).resolves.toBe('token-1');
  });

  it('forgets an expired token', async () => {
    const ref = renderWidget();
    await waitFor(() => expect(api.render).toHaveBeenCalled());
    act(() => api.solve('stale'));
    act(() => api.options()['expired-callback']?.());

    const pending = ref.current!.getToken();
    act(() => api.solve('fresh'));
    await expect(pending).resolves.toBe('fresh');
  });

  it('rejects when the widget reports an error and shows a hint', async () => {
    const ref = renderWidget();
    await waitFor(() => expect(api.render).toHaveBeenCalled());

    const pending = ref.current!.getToken();
    act(() => {
      api.options()['error-callback']?.('300010');
    });

    await expect(pending).rejects.toBeInstanceOf(TurnstileError);
    await expect(ref.current!.getToken()).rejects.toMatchObject({ code: '300010' });
    expect(screen.getByRole('alert')).toHaveTextContent('challenges.cloudflare.com');
  });

  it('rejects when the script cannot be loaded', async () => {
    loadMock.mockRejectedValue(new TurnstileError('script-load-failed'));
    const ref = renderWidget();

    await waitFor(() => expect(screen.getByRole('alert')).toBeInTheDocument());
    await expect(ref.current!.getToken()).rejects.toMatchObject({ code: 'script-load-failed' });
  });

  it('gives up waiting after the timeout', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const ref = renderWidget();
    await waitFor(() => expect(api.render).toHaveBeenCalled());

    const pending = expect(ref.current!.getToken()).rejects.toMatchObject({ code: 'timeout' });
    await vi.advanceTimersByTimeAsync(TOKEN_TIMEOUT_MS + 1);
    await pending;
  });

  it('removes the widget on unmount', async () => {
    renderWidget();
    await waitFor(() => expect(api.render).toHaveBeenCalled());
    cleanup();
    expect(api.remove).toHaveBeenCalledWith('widget-1');
  });
});
