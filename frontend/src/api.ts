import { ErrorResponse, UploadResponse } from './types';

/**
 * ApiError carries the structured error body of the backend so pages can
 * translate the error key and show per-file / identity details.
 */
export class ApiError extends Error {
  status: number;
  body: ErrorResponse;

  constructor(status: number, body: ErrorResponse) {
    super(body.message || body.error);
    this.status = status;
    this.body = body;
  }

  /** i18n key derived from the backend error key, e.g. "error:not-a-diploma" -> "error_not_a_diploma". */
  get translationKey(): string {
    return errorKeyToTranslationKey(this.body.error);
  }
}

/** Thrown when an upload is cancelled through its AbortSignal. */
export class UploadCancelled extends Error {
  constructor() {
    super('upload cancelled');
    this.name = 'UploadCancelled';
  }
}

export function errorKeyToTranslationKey(errorKey: string | undefined): string {
  if (!errorKey) {
    return 'error_default';
  }
  return errorKey.trim().replaceAll('-', '_').replaceAll(':', '_').toLowerCase();
}

/**
 * True when the backend cannot verify signatures at the moment because the
 * trust anchors (the EU trusted lists) could not be loaded. The files
 * themselves are not the problem, so the upload can simply be repeated later.
 */
export function isVerificationUnavailable(err: unknown): err is ApiError {
  return err instanceof ApiError && err.status === 503 && err.body.error === 'error:validation-service-unavailable';
}

async function parseError(response: Response): Promise<ApiError> {
  let body: ErrorResponse = { error: 'error:internal' };
  try {
    const parsed = await response.json();
    if (parsed && typeof parsed.error === 'string') {
      body = parsed;
    }
  } catch {
    // Not JSON (e.g. a proxy error page); keep the generic error.
  }
  return new ApiError(response.status, body);
}

/**
 * Uploads one or more diploma extracts in a single request. The backend
 * verifies and reads every file and answers per file; the request as a whole
 * only fails when no file was accepted (or when the request is malformed).
 */
export async function uploadDiplomas(files: File[], signal?: AbortSignal): Promise<UploadResponse> {
  const form = new FormData();
  for (const file of files) {
    form.append('file', file, file.name);
  }
  let response: Response;
  try {
    response = await fetch('/api/diploma/upload', { method: 'POST', body: form, signal });
  } catch (err) {
    if (signal?.aborted) {
      throw new UploadCancelled();
    }
    throw err;
  }
  if (!response.ok) {
    throw await parseError(response);
  }
  return (await response.json()) as UploadResponse;
}

export async function issueDiplomas(sessionId: string): Promise<Response> {
  const response = await fetch('/api/diploma/issue', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ session_id: sessionId }),
  });
  if (!response.ok) {
    throw await parseError(response);
  }
  return response;
}
