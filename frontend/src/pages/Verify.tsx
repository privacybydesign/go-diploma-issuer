import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useNavigate } from 'react-router-dom';
import { useAppContext } from '../AppContext';
import { ApiError, issueDiplomas } from '../api';
import { IdentityMatchInfo, IssuanceResponse } from '../types';

type Phase = 'idle' | 'disclosing' | 'matching' | 'issuing';

export default function VerifyPage() {
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();
  const { upload, setUpload } = useAppContext();
  const [phase, setPhase] = useState<Phase>('idle');
  const [errorMessage, setErrorMessage] = useState<string | undefined>();
  const [mismatch, setMismatch] = useState<IdentityMatchInfo | undefined>();

  // Without uploaded extracts there is nothing to verify against.
  useEffect(() => {
    if (!upload) {
      navigate(`/${i18n.language}/upload`, { replace: true });
    }
  }, [upload, navigate, i18n.language]);

  if (!upload) {
    return null;
  }

  const sessionId = upload.session_id;
  const accepted = upload.files.filter((file) => file.accepted && file.document);

  const handleApiError = (err: unknown) => {
    if (err instanceof ApiError) {
      if (err.body.error === 'error:identity-mismatch' && err.body.identity) {
        setMismatch(err.body.identity);
        return;
      }
      if (err.body.error === 'error:unknown-session') {
        setUpload(undefined);
        navigate(`/${i18n.language}/upload`);
        return;
      }
      setErrorMessage(t(err.translationKey));
      return;
    }
    navigate(`/${i18n.language}/error`);
  };

  // Step 2: the disclosure finished; ask the backend to compare and issue.
  const matchAndIssue = async () => {
    setPhase('matching');
    try {
      const response = await issueDiplomas(sessionId);
      const result: IssuanceResponse = await response.json();
      await startIssuance(result);
    } catch (err) {
      setPhase('idle');
      handleApiError(err);
    }
  };

  // Step 3: hand the signed issuance request (one credential per diploma) to
  // the Yivi app.
  const startIssuance = async (result: IssuanceResponse) => {
    setPhase('issuing');
    const yivi = await import('@privacybydesign/yivi-frontend');
    const issuance = yivi.newPopup({
      language: i18n.language as 'en' | 'nl',
      session: {
        url: result.irma_server_url,
        start: {
          method: 'POST',
          headers: { 'Content-Type': 'text/plain' },
          body: result.jwt,
        },
        result: false,
      },
    });
    try {
      await issuance.start();
      setUpload(undefined);
      navigate(`/${i18n.language}/done`, { state: { count: result.credentials } });
    } catch (e) {
      setPhase('idle');
      setErrorMessage(e === 'Aborted' ? t('verify_cancelled') : t('issue_error'));
    }
  };

  // Step 1: disclosure of the identity. The backend starts the IRMA session
  // (it needs the requestor token to read the result), the app gets the
  // session pointer through yivi-frontend's default mapping.
  const startDisclosure = async () => {
    setErrorMessage(undefined);
    setMismatch(undefined);
    setPhase('disclosing');
    const yivi = await import('@privacybydesign/yivi-frontend');
    const disclosure = yivi.newPopup({
      language: i18n.language as 'en' | 'nl',
      session: {
        url: '/api/diploma',
        start: {
          url: '/api/diploma/start-disclosure',
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ session_id: sessionId }),
        },
        result: false,
      },
    });
    try {
      await disclosure.start();
    } catch (e) {
      setPhase('idle');
      if (e === 'Aborted') {
        setErrorMessage(t('verify_cancelled'));
      } else {
        console.error('Disclosure failed', e);
        setErrorMessage(t('verify_disclosure_error'));
      }
      return;
    }
    await matchAndIssue();
  };

  const busy = phase !== 'idle';

  return (
    <div id="container">
      <header>
        <h1>{t('verify_header')}</h1>
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
          {mismatch && (
            <div id="status-bar" className="alert alert-danger" role="alert">
              <div className="status-container">
                <div id="status">
                  <b>{t('mismatch_header')}</b>
                  <ul className="mismatch-list">
                    {!mismatch.date_of_birth_match && <li>{t('mismatch_date_of_birth')}</li>}
                    {!mismatch.surname_match && <li>{t('mismatch_surname')}</li>}
                    {!mismatch.given_names_match && <li>{t('mismatch_given_names')}</li>}
                  </ul>
                </div>
              </div>
            </div>
          )}
          {phase === 'matching' && (
            <div id="status-bar" className="alert alert-info" role="status">
              <div className="status-container">
                <div id="status">{t('verify_busy')}</div>
              </div>
            </div>
          )}
          {phase === 'issuing' && (
            <div id="status-bar" className="alert alert-success" role="status">
              <div className="status-container">
                <div id="status">{t('verify_match', { count: accepted.length })}</div>
              </div>
            </div>
          )}
          <p>{t('verify_explanation', { count: accepted.length })}</p>
          <table className="table">
            <tbody>
              <tr>
                <th>{t('field_name')}</th>
                <td>{upload.person.full_name}</td>
              </tr>
              <tr>
                <th>{t('field_date_of_birth')}</th>
                <td>{upload.person.date_of_birth}</td>
              </tr>
              <tr>
                <th>{t('field_diplomas', { count: accepted.length })}</th>
                <td>
                  <ul className="profile-list">
                    {accepted.map((file, index) => (
                      <li key={`${file.filename}-${index}`}>{file.document!.qualification}</li>
                    ))}
                  </ul>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </main>
      <footer>
        <div className="actions">
          <Link to={`/${i18n.language}/upload`} id="back-button">
            {t('back')}
          </Link>
          <button id="submit-button" type="button" disabled={busy} onClick={startDisclosure}>
            {mismatch ? t('verify_retry') : t('verify_start')}
          </button>
        </div>
      </footer>
    </div>
  );
}
