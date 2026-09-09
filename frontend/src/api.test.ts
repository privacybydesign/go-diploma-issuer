import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError, UploadCancelled, errorKeyToTranslationKey, isVerificationUnavailable, uploadDiplomas } from './api';

describe('errorKeyToTranslationKey', () => {
  it('maps backend error keys onto i18n keys', () => {
    expect(errorKeyToTranslationKey('error:not-a-diploma')).toBe('error_not_a_diploma');
    expect(errorKeyToTranslationKey('error:validation-service-unavailable')).toBe('error_validation_service_unavailable');
    expect(errorKeyToTranslationKey(' error:internal ')).toBe('error_internal');
  });

  it('falls back to the default error', () => {
    expect(errorKeyToTranslationKey(undefined)).toBe('error_default');
    expect(errorKeyToTranslationKey('')).toBe('error_default');
  });
});

describe('ApiError', () => {
  it('exposes status, body and translation key', () => {
    const err = new ApiError(422, {
      error: 'error:validation-failed',
      message: 'rejected',
      files: [{ filename: 'a.pdf', accepted: false, error: 'error:validation-failed' }],
    });
    expect(err.status).toBe(422);
    expect(err.message).toBe('rejected');
    expect(err.translationKey).toBe('error_validation_failed');
    expect(err.body.files?.[0].filename).toBe('a.pdf');
  });
});

describe('isVerificationUnavailable', () => {
  it('recognises missing trust anchors', () => {
    expect(isVerificationUnavailable(new ApiError(503, { error: 'error:validation-service-unavailable' }))).toBe(true);
  });

  it('does not treat a rejected document or other errors as an outage', () => {
    expect(isVerificationUnavailable(new ApiError(422, { error: 'error:validation-failed' }))).toBe(false);
    expect(isVerificationUnavailable(new ApiError(503, { error: 'error:internal' }))).toBe(false);
    expect(isVerificationUnavailable(new Error('network'))).toBe(false);
    expect(isVerificationUnavailable(undefined)).toBe(false);
  });
});

describe('uploadDiplomas', () => {
  const files = [
    new File(['%PDF-1.7 a'], 'havo.pdf', { type: 'application/pdf' }),
    new File(['%PDF-1.7 b'], 'hbo.pdf', { type: 'application/pdf' }),
  ];

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('sends every file as a "file" part and returns the parsed response', async () => {
    const ok = { session_id: 's1', person: { full_name: 'A B', date_of_birth: '1980-02-03' }, files: [], accepted: 2, rejected: 0 };
    const fetchMock = vi.fn(async (_url: string, init: RequestInit) => {
      const form = init.body as FormData;
      expect(form.getAll('file')).toHaveLength(2);
      return new Response(JSON.stringify(ok), { status: 200 });
    });
    vi.stubGlobal('fetch', fetchMock);

    const result = await uploadDiplomas(files);
    expect(result.session_id).toBe('s1');
    expect(result.accepted).toBe(2);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls[0][0]).toBe('/api/diploma/upload');
  });

  it('turns an error response into an ApiError with the per-file detail', async () => {
    const body = {
      error: 'error:validation-failed',
      files: [{ filename: 'havo.pdf', accepted: false, error: 'error:validation-failed' }],
    };
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify(body), { status: 422 })));

    await expect(uploadDiplomas(files)).rejects.toMatchObject({ status: 422, body });
  });

  it('keeps a generic error when the body is not JSON', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('<html>bad gateway</html>', { status: 502 })));

    await expect(uploadDiplomas(files)).rejects.toMatchObject({ status: 502, body: { error: 'error:internal' } });
  });

  it('reports a cancelled request as a cancellation', async () => {
    const controller = new AbortController();
    vi.stubGlobal(
      'fetch',
      vi.fn(async (_url: string, init: RequestInit) => {
        controller.abort();
        throw init.signal?.reason ?? new DOMException('aborted', 'AbortError');
      }),
    );

    await expect(uploadDiplomas(files, controller.signal)).rejects.toBeInstanceOf(UploadCancelled);
  });
});
