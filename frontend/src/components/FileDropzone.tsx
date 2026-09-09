import { DragEvent, useId, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { MAX_FILES, MAX_UPLOAD_BYTES, formatBytes } from '../fileCheck';

interface Props {
  /** The currently selected (and accepted) files. */
  files: File[];
  /** Translated error messages to show under the zone (e.g. for files that were refused). */
  errors?: string[];
  disabled?: boolean;
  /** Called with the newly picked files (appended to the selection). */
  onAdd: (files: File[]) => void;
  /** Called to drop one file from the selection. */
  onRemove: (index: number) => void;
}

/**
 * Drop zone plus "choose files" button for one or more diploma extracts. The
 * native file input stays in the DOM (visually hidden) so keyboard users and
 * screen readers get the standard control, while pointer users get a large
 * drop target. Picked files are listed above the zone with a remove button.
 */
export default function FileDropzone({ files, errors, disabled, onAdd, onRemove }: Props) {
  const { t } = useTranslation();
  const inputRef = useRef<HTMLInputElement>(null);
  const [dragging, setDragging] = useState(false);
  const errorId = useId();
  const hintId = useId();
  const full = files.length >= MAX_FILES;

  const openPicker = () => {
    if (!disabled && !full) {
      inputRef.current?.click();
    }
  };

  const pick = (picked: FileList | null | undefined) => {
    if (picked && picked.length > 0) {
      onAdd(Array.from(picked));
    }
    // Reset the input so picking the same file again re-triggers onChange.
    if (inputRef.current) {
      inputRef.current.value = '';
    }
  };

  const onDragOver = (e: DragEvent<HTMLDivElement>) => {
    e.preventDefault();
    if (!disabled && !full) {
      e.dataTransfer.dropEffect = 'copy';
      setDragging(true);
    }
  };

  const onDrop = (e: DragEvent<HTMLDivElement>) => {
    e.preventDefault();
    setDragging(false);
    if (disabled || full) {
      return;
    }
    pick(e.dataTransfer.files);
  };

  const hasErrors = errors && errors.length > 0;
  const className = [
    'dropzone',
    dragging ? 'dropzone-dragging' : '',
    hasErrors ? 'dropzone-error' : '',
    disabled || full ? 'dropzone-disabled' : '',
  ].join(' ').trim();

  return (
    <div className="dropzone-wrapper">
      {files.length > 0 && (
        <ul className="file-list" aria-label={t('upload_selected_files', { count: files.length })}>
          {files.map((file, index) => (
            <li key={`${file.name}-${file.size}-${index}`} className="dropzone-file file-list-item">
              <PdfIcon />
              <div className="dropzone-file-info">
                <div className="dropzone-file-name" title={file.name}>{file.name}</div>
                <div className="dropzone-file-meta">
                  {formatBytes(file.size)} &middot; PDF &middot; <span className="dropzone-file-ready">{t('upload_file_ready')}</span>
                </div>
              </div>
              <button
                type="button"
                className="dropzone-remove"
                onClick={() => onRemove(index)}
                disabled={disabled}
                aria-label={t('upload_remove_file', { name: file.name })}
                title={t('upload_remove_file', { name: file.name })}
              >
                <CloseIcon />
              </button>
            </li>
          ))}
        </ul>
      )}

      <div
        className={className}
        onDragOver={onDragOver}
        onDragEnter={onDragOver}
        onDragLeave={() => setDragging(false)}
        onDrop={onDrop}
        onClick={openPicker}
        data-testid="dropzone"
      >
        <input
          ref={inputRef}
          id="diploma-files"
          className="dropzone-input"
          type="file"
          accept="application/pdf,.pdf"
          multiple
          disabled={disabled || full}
          aria-describedby={hasErrors ? `${hintId} ${errorId}` : hintId}
          aria-invalid={hasErrors ? true : undefined}
          onChange={(e) => pick(e.target.files)}
          onClick={(e) => e.stopPropagation()}
        />
        <div className="dropzone-empty">
          <UploadIcon />
          <div className="dropzone-title">
            {full ? t('upload_drop_full', { count: MAX_FILES }) : files.length > 0 ? t('upload_drop_more') : t('upload_drop_title')}
          </div>
          {!full && (
            <>
              <div className="dropzone-or">{t('upload_drop_or')}</div>
              <button
                type="button"
                className="dropzone-button"
                onClick={(e) => { e.stopPropagation(); openPicker(); }}
                disabled={disabled}
              >
                {files.length > 0 ? t('upload_choose_more') : t('upload_choose_files')}
              </button>
            </>
          )}
        </div>
      </div>

      <div id={hintId} className="dropzone-hint">
        {t('upload_file_requirements', { max: formatBytes(MAX_UPLOAD_BYTES), count: MAX_FILES })}
      </div>
      {hasErrors && (
        <div id={errorId} className="dropzone-message" role="alert">
          <WarningIcon />
          <ul className="dropzone-message-list">
            {errors.map((error, index) => <li key={index}>{error}</li>)}
          </ul>
        </div>
      )}
    </div>
  );
}

function UploadIcon() {
  return (
    <svg className="dropzone-icon" viewBox="0 0 48 48" width="48" height="48" aria-hidden="true" focusable="false">
      <path d="M14 6h14l10 10v24a2 2 0 0 1-2 2H14a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2z" fill="#FFFFFF" stroke="currentColor" strokeWidth="2" strokeLinejoin="round" />
      <path d="M28 6v10h10" fill="none" stroke="currentColor" strokeWidth="2" strokeLinejoin="round" />
      <path d="M24 34V22m0 0-5 5m5-5 5 5" fill="none" stroke="#E12747" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

export function PdfIcon() {
  return (
    <svg className="dropzone-icon dropzone-icon-small" viewBox="0 0 48 48" width="40" height="40" aria-hidden="true" focusable="false">
      <path d="M14 6h14l10 10v24a2 2 0 0 1-2 2H14a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2z" fill="#FFFFFF" stroke="currentColor" strokeWidth="2" strokeLinejoin="round" />
      <path d="M28 6v10h10" fill="none" stroke="currentColor" strokeWidth="2" strokeLinejoin="round" />
      <rect x="8" y="24" width="24" height="12" rx="2" fill="#E12747" />
      <text x="20" y="33.5" textAnchor="middle" fontFamily="Alexandria, Verdana, Arial, sans-serif" fontWeight="bold" fontSize="8.5" fill="#FFFFFF">PDF</text>
    </svg>
  );
}

function CloseIcon() {
  return (
    <svg viewBox="0 0 20 20" width="16" height="16" aria-hidden="true" focusable="false">
      <path d="M5 5l10 10M15 5L5 15" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
    </svg>
  );
}

function WarningIcon() {
  return (
    <svg viewBox="0 0 20 20" width="18" height="18" aria-hidden="true" focusable="false">
      <path d="M10 2.5 18.5 17H1.5z" fill="currentColor" />
      <path d="M10 8v4.5" stroke="#FFFFFF" strokeWidth="1.8" strokeLinecap="round" />
      <circle cx="10" cy="14.8" r="1" fill="#FFFFFF" />
    </svg>
  );
}
