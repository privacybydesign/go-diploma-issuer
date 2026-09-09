/**
 * Client-side checks on a file picked for upload. These mirror what the
 * backend enforces (extension, media type, size, PDF header and trailer) so
 * the user gets immediate feedback instead of a round trip to the server.
 * The backend remains the authority; nothing here is a security boundary.
 */

/** Mirrors DefaultMaxUploadSize in the backend (5 MiB per file). */
export const MAX_UPLOAD_BYTES = 5 * 1024 * 1024;

/** Mirrors DefaultMaxFiles in the backend: extracts per upload. */
export const MAX_FILES = 10;

export type FileCheckError =
  | 'empty'
  | 'too_large'
  | 'not_pdf_extension'
  | 'not_pdf_type'
  | 'not_pdf_content';

/**
 * Media types browsers report for a PDF. Some browsers and operating systems
 * report a generic octet-stream or nothing at all for unknown associations;
 * the content check below covers those cases.
 */
const PDF_MEDIA_TYPES = new Set(['application/pdf', 'application/x-pdf', 'application/octet-stream', '']);

const PDF_HEADER = '%PDF-';
const PDF_TRAILER = '%%EOF';
const TRAILER_WINDOW = 1024;

export async function checkPdfFile(file: File, maxBytes: number = MAX_UPLOAD_BYTES): Promise<FileCheckError | undefined> {
  if (!/\.pdf$/i.test(file.name)) {
    return 'not_pdf_extension';
  }
  if (!PDF_MEDIA_TYPES.has(file.type.toLowerCase())) {
    return 'not_pdf_type';
  }
  if (file.size === 0) {
    return 'empty';
  }
  if (file.size > maxBytes) {
    return 'too_large';
  }

  const head = await readBytes(file.slice(0, PDF_HEADER.length + 3));
  if (!isPdfHeader(head)) {
    return 'not_pdf_content';
  }
  const tail = await readBytes(file.slice(Math.max(0, file.size - TRAILER_WINDOW)));
  if (!hasPdfTrailer(tail)) {
    return 'not_pdf_content';
  }
  return undefined;
}

/** Maps a check error onto the i18n key of the message to show the user. */
export function fileCheckErrorKey(error: FileCheckError): string {
  switch (error) {
    case 'too_large':
      return 'file_error_too_large';
    case 'empty':
      return 'file_error_empty';
    default:
      return 'file_error_not_pdf';
  }
}

/** "%PDF-" followed by a single-digit major version, a dot and a digit. */
export function isPdfHeader(bytes: Uint8Array): boolean {
  const text = latin1(bytes);
  if (!text.startsWith(PDF_HEADER)) {
    return false;
  }
  const version = text.slice(PDF_HEADER.length, PDF_HEADER.length + 3);
  return /^\d\.\d$/.test(version);
}

/** "%%EOF" as the last non-whitespace content of the (windowed) tail. */
export function hasPdfTrailer(bytes: Uint8Array): boolean {
  const text = latin1(bytes).replace(/[ \t\r\n\0]+$/, '');
  return text.endsWith(PDF_TRAILER);
}

/** Human readable size, e.g. "312 kB" or "1.2 MB". */
export function formatBytes(bytes: number): string {
  if (bytes < 1024) {
    return `${bytes} B`;
  }
  if (bytes < 1024 * 1024) {
    return `${Math.round(bytes / 1024)} kB`;
  }
  return `${(bytes / (1024 * 1024)).toFixed(1).replace(/\.0$/, '')} MB`;
}

function latin1(bytes: Uint8Array): string {
  let out = '';
  for (let i = 0; i < bytes.length; i++) {
    out += String.fromCharCode(bytes[i]);
  }
  return out;
}

async function readBytes(blob: Blob): Promise<Uint8Array> {
  if (typeof blob.arrayBuffer === 'function') {
    return new Uint8Array(await blob.arrayBuffer());
  }
  // Older WebKit: fall back to FileReader.
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(new Uint8Array(reader.result as ArrayBuffer));
    reader.onerror = () => reject(reader.error);
    reader.readAsArrayBuffer(blob);
  });
}
