import { useTranslation } from 'react-i18next';
import { DocumentInfo, FileResult, ValidationInfo } from '../types';

/** Human readable name of the qualification with its level, e.g. "Havo (NLQF 4)". */
export function qualificationLabel(document: DocumentInfo): string {
  return document.qualification;
}

/** Summary table of one extract. */
export function DocumentSummary({ document }: { document: DocumentInfo }) {
  const { t } = useTranslation();

  return (
    <table className="table">
      <tbody>
        <tr>
          <th>{t('field_document_type')}</th>
          <td>{document.document_type}</td>
        </tr>
        <tr>
          <th>{t('field_qualification')}</th>
          <td>{document.qualification}</td>
        </tr>
        {document.profiles.length > 0 && (
          <tr>
            <th>{t('field_profile')}</th>
            <td>{document.profiles.join(', ')}</td>
          </tr>
        )}
        <tr>
          <th>{t('field_institution')}</th>
          <td>{document.institution}</td>
        </tr>
        <tr>
          <th>{t('field_date_awarded')}</th>
          <td>{[document.place_of_issue, document.date_awarded].filter(Boolean).join(', ')}</td>
        </tr>
        {document.nlqf_level && (
          <tr>
            <th>{t('field_level')}</th>
            <td>{t('field_level_value', { nlqf: document.nlqf_level, eqf: document.eqf_level })}</td>
          </tr>
        )}
        <tr>
          <th>{t('field_document_number')}</th>
          <td>{document.document_number}</td>
        </tr>
        <tr>
          <th>{t('field_download_date')}</th>
          <td>{document.download_date}</td>
        </tr>
        {document.has_grade_list && (
          <tr>
            <th>{t('field_grade_list')}</th>
            <td>{t('field_grade_list_included')}</td>
          </tr>
        )}
      </tbody>
    </table>
  );
}

/** Translated explanation of a failed verification, if any. */
export function validationDetail(t: (key: string, options?: Record<string, unknown>) => string, validation?: ValidationInfo): string | undefined {
  if (!validation || validation.valid) {
    return undefined;
  }
  const key = `validation_check_${validation.key}`;
  const translated = t(key);
  return translated === key ? t('validation_check_unknown', { check: validation.key }) : translated;
}

/**
 * One uploaded file with its outcome: a green card with the data read from an
 * accepted extract, a red card with the reason for a rejected one.
 */
export default function FileCard({ file }: { file: FileResult }) {
  const { t } = useTranslation();
  const detail = validationDetail(t, file.validation);

  if (file.accepted && file.document) {
    return (
      <section className="file-card file-card-accepted" aria-label={file.filename}>
        <header className="file-card-header">
          <span className="file-card-mark" aria-hidden="true">✓</span>
          <div className="file-card-title">
            <div className="file-card-filename">{file.filename}</div>
            <div className="file-card-status">{t('file_accepted')}</div>
          </div>
        </header>
        <DocumentSummary document={file.document} />
      </section>
    );
  }

  const reasonKey = file.error ? file.error.trim().replaceAll('-', '_').replaceAll(':', '_').toLowerCase() : 'error_default';
  return (
    <section className="file-card file-card-rejected" aria-label={file.filename}>
      <header className="file-card-header">
        <span className="file-card-mark" aria-hidden="true">✕</span>
        <div className="file-card-title">
          <div className="file-card-filename">{file.filename}</div>
          <div className="file-card-status">{t('file_rejected')}</div>
        </div>
      </header>
      <p className="file-card-reason">
        {t(reasonKey)}
        {detail && <><br />{detail}</>}
      </p>
    </section>
  );
}
