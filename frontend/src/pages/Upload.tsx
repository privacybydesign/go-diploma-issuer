import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useNavigate } from 'react-router-dom';
import { useAppContext } from '../AppContext';
import { ApiError, UploadCancelled, isVerificationUnavailable, uploadDiplomas } from '../api';
import { MAX_FILES, checkPdfFile, fileCheckErrorKey } from '../fileCheck';
import FileDropzone from '../components/FileDropzone';
import FileCard from './DocumentSummary';
import { FileResult } from '../types';

export default function UploadPage() {
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();
  const { upload, setUpload } = useAppContext();
  const [files, setFiles] = useState<File[]>([]);
  const [fileErrors, setFileErrors] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [errorMessage, setErrorMessage] = useState<string | undefined>();
  const [rejected, setRejected] = useState<FileResult[]>([]);
  const abortRef = useRef<AbortController | undefined>(undefined);

  // Leaving the page cancels an upload still in progress.
  useEffect(() => () => abortRef.current?.abort(), []);

  const clearMessages = () => {
    setErrorMessage(undefined);
    setFileErrors([]);
    setRejected([]);
  };

  const add = async (picked: File[]) => {
    clearMessages();
    const accepted: File[] = [];
    const problems: string[] = [];
    let count = files.length;
    for (const file of picked) {
      if (files.some((f) => f.name === file.name && f.size === file.size) || accepted.some((f) => f.name === file.name && f.size === file.size)) {
        problems.push(t('file_error_duplicate', { name: file.name }));
        continue;
      }
      if (count >= MAX_FILES) {
        problems.push(t('file_error_too_many', { name: file.name, count: MAX_FILES }));
        continue;
      }
      const problem = await checkPdfFile(file);
      if (problem) {
        problems.push(`${file.name}: ${t(fileCheckErrorKey(problem))}`);
        continue;
      }
      accepted.push(file);
      count++;
    }
    if (accepted.length > 0) {
      setFiles((current) => [...current, ...accepted]);
    }
    setFileErrors(problems);
  };

  const remove = (index: number) => {
    clearMessages();
    setFiles((current) => current.filter((_, i) => i !== index));
  };

  const submit = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (files.length === 0 || busy) {
      return;
    }
    const controller = new AbortController();
    abortRef.current = controller;
    setBusy(true);
    clearMessages();
    try {
      const result = await uploadDiplomas(files, controller.signal);
      setUpload(result);
    } catch (err) {
      if (err instanceof UploadCancelled) {
        return;
      }
      if (isVerificationUnavailable(err)) {
        setErrorMessage(t('upload_verification_unavailable'));
      } else if (err instanceof ApiError) {
        setErrorMessage(err.body.files && err.body.files.length > 0 ? t('upload_all_rejected', { count: err.body.files.length }) : t(err.translationKey));
        setRejected(err.body.files ?? []);
      } else {
        navigate(`/${i18n.language}/error`);
      }
    } finally {
      if (abortRef.current === controller) {
        abortRef.current = undefined;
      }
      setBusy(false);
    }
  };

  const reset = () => {
    setUpload(undefined);
    setFiles([]);
    clearMessages();
  };

  if (upload) {
    return (
      <div id="container">
        <header>
          <h1>{t('upload_header')}</h1>
        </header>
        <main>
          <div className="sms-form">
            <div id="status-bar" className={`alert ${upload.rejected > 0 ? 'alert-warning' : 'alert-success'}`} role="alert">
              <div className="status-container">
                <div id="status">
                  {t('upload_success', { count: upload.accepted })}
                  {upload.rejected > 0 && <><br />{t('upload_some_rejected', { count: upload.rejected })}</>}
                </div>
              </div>
            </div>
            <p>{t('upload_result_header', { name: upload.person.full_name, dob: upload.person.date_of_birth })}</p>
            <div className="file-cards">
              {upload.files.map((file, index) => <FileCard key={`${file.filename}-${index}`} file={file} />)}
            </div>
          </div>
        </main>
        <footer>
          <div className="actions">
            <a href="#" id="back-button" onClick={(e) => { e.preventDefault(); reset(); }}>
              {t('upload_other_files')}
            </a>
            <button id="submit-button" type="button" onClick={() => navigate(`/${i18n.language}/verify`)}>
              {t('upload_continue')}
            </button>
          </div>
        </footer>
      </div>
    );
  }

  return (
    <form id="container" onSubmit={submit}>
      <header>
        <h1>{t('upload_header')}</h1>
      </header>
      <main>
        <div className="sms-form">
          {errorMessage && (
            <div id="status-bar" className="alert alert-danger" role="alert">
              <div className="status-container">
                <div id="status">{errorMessage}</div>
              </div>
            </div>
          )}
          {busy && (
            <div id="status-bar" className="alert alert-info" role="status">
              <div className="status-container">
                <div id="status">{t('upload_busy', { count: files.length })}</div>
              </div>
            </div>
          )}
          <p>{t('upload_explanation')}</p>
          <p>{t('upload_validation_explanation')}</p>
          <p className="details">
            {t('upload_where_hint')}{' '}
            <Link to={`/${i18n.language}`}>{t('upload_where_link')}</Link>
          </p>
          <label htmlFor="diploma-files">{t('upload_file_label')}</label>
          <FileDropzone files={files} errors={fileErrors} disabled={busy} onAdd={add} onRemove={remove} />
          {rejected.length > 0 && (
            <div className="file-cards">
              {rejected.map((file, index) => <FileCard key={`${file.filename}-${index}`} file={file} />)}
            </div>
          )}
        </div>
      </main>
      <footer>
        <div className="actions">
          <Link to={`/${i18n.language}`} id="back-button">
            {t('back')}
          </Link>
          <button id="submit-button" type="submit" disabled={files.length === 0 || busy}>
            {t('upload_button', { count: Math.max(files.length, 1) })}
          </button>
        </div>
      </footer>
    </form>
  );
}
