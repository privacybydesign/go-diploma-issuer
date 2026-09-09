import { describe, expect, it } from 'vitest';
import { MAX_UPLOAD_BYTES, checkPdfFile, fileCheckErrorKey, formatBytes, hasPdfTrailer, isPdfHeader } from './fileCheck';

const validPdf = '%PDF-1.5\n1 0 obj\nendobj\n%%EOF\n';

function makeFile(content: string | Uint8Array<ArrayBuffer>, name = 'diploma.pdf', type = 'application/pdf'): File {
  return new File([content], name, { type });
}

describe('checkPdfFile', () => {
  it('accepts a PDF picked in a browser', async () => {
    expect(await checkPdfFile(makeFile(validPdf))).toBeUndefined();
  });

  it('accepts a PDF with an unknown media type when the content is right', async () => {
    expect(await checkPdfFile(makeFile(validPdf, 'DIPLOMA.PDF', ''))).toBeUndefined();
    expect(await checkPdfFile(makeFile(validPdf, 'diploma.pdf', 'application/octet-stream'))).toBeUndefined();
  });

  it('rejects files with another extension', async () => {
    expect(await checkPdfFile(makeFile(validPdf, 'diploma.docx'))).toBe('not_pdf_extension');
    expect(await checkPdfFile(makeFile(validPdf, 'diploma.pdf.exe'))).toBe('not_pdf_extension');
    expect(await checkPdfFile(makeFile(validPdf, 'diploma'))).toBe('not_pdf_extension');
  });

  it('rejects files that the browser reports as another type', async () => {
    expect(await checkPdfFile(makeFile(validPdf, 'diploma.pdf', 'image/png'))).toBe('not_pdf_type');
    expect(await checkPdfFile(makeFile(validPdf, 'diploma.pdf', 'text/html'))).toBe('not_pdf_type');
  });

  it('rejects empty and oversized files', async () => {
    expect(await checkPdfFile(makeFile(''))).toBe('empty');
    expect(await checkPdfFile(makeFile(validPdf), 10)).toBe('too_large');
    expect(await checkPdfFile(makeFile(new Uint8Array(MAX_UPLOAD_BYTES + 1)))).toBe('too_large');
  });

  it('rejects files that only pretend to be a PDF', async () => {
    expect(await checkPdfFile(makeFile('hello world'))).toBe('not_pdf_content');
    expect(await checkPdfFile(makeFile('%PDF\n%%EOF'))).toBe('not_pdf_content');
    expect(await checkPdfFile(makeFile('%PDF-1.5\nno trailer here'))).toBe('not_pdf_content');
    expect(await checkPdfFile(makeFile('%PDF-1.5\n%%EOF\n' + 'x'.repeat(2048)))).toBe('not_pdf_content');
    expect(await checkPdfFile(makeFile('<html>%PDF-1.5 %%EOF</html>'))).toBe('not_pdf_content');
    expect(await checkPdfFile(makeFile('PK\x03\x04%PDF-1.5%%EOF'))).toBe('not_pdf_content');
  });
});

describe('isPdfHeader / hasPdfTrailer', () => {
  const bytes = (s: string) => Uint8Array.from(s, (c) => c.charCodeAt(0));

  it('recognises the header', () => {
    expect(isPdfHeader(bytes('%PDF-1.7\n'))).toBe(true);
    expect(isPdfHeader(bytes('%PDF-2.0'))).toBe(true);
    expect(isPdfHeader(bytes('%PDF-1'))).toBe(false);
    expect(isPdfHeader(bytes('%PDF-a.b'))).toBe(false);
    expect(isPdfHeader(bytes(' %PDF-1.5'))).toBe(false);
  });

  it('recognises the trailer', () => {
    expect(hasPdfTrailer(bytes('...\n%%EOF'))).toBe(true);
    expect(hasPdfTrailer(bytes('...\n%%EOF\r\n\r\n'))).toBe(true);
    expect(hasPdfTrailer(bytes('%%EOF\nmore'))).toBe(false);
    expect(hasPdfTrailer(bytes(''))).toBe(false);
  });
});

describe('fileCheckErrorKey', () => {
  it('maps detailed codes onto user messages', () => {
    expect(fileCheckErrorKey('not_pdf_extension')).toBe('file_error_not_pdf');
    expect(fileCheckErrorKey('not_pdf_type')).toBe('file_error_not_pdf');
    expect(fileCheckErrorKey('not_pdf_content')).toBe('file_error_not_pdf');
    expect(fileCheckErrorKey('too_large')).toBe('file_error_too_large');
    expect(fileCheckErrorKey('empty')).toBe('file_error_empty');
  });
});

describe('formatBytes', () => {
  it('formats sizes for humans', () => {
    expect(formatBytes(512)).toBe('512 B');
    expect(formatBytes(312 * 1024)).toBe('312 kB');
    expect(formatBytes(5 * 1024 * 1024)).toBe('5 MB');
    expect(formatBytes(1.25 * 1024 * 1024)).toBe('1.3 MB');
  });
});
