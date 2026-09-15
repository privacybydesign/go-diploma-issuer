import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useLocation } from 'react-router-dom';
import { useAppContext } from '../AppContext';

/**
 * Shown once the Yivi app has accepted the credentials. The number of
 * diplomas comes along as navigation state from the verify page.
 */
export default function DonePage() {
  const { t, i18n } = useTranslation();
  const location = useLocation();
  const { setUpload } = useAppContext();
  const count = (location.state as { count?: number } | null)?.count ?? 1;

  // The credentials are in the app: the flow is over, forget the extracts and
  // the (already used) session.
  useEffect(() => {
    setUpload(undefined);
  }, [setUpload]);

  return (
    <div id="container">
      <header>
        <h1>{t('done_header', { count })}</h1>
      </header>
      <main>
        <div className="sms-form">
          <div className="imageContainer">
            <img src="/images/done.png" alt="done" />
            <p>{t('done_explanation', { count })}</p>
            <p>{t('thank_you')}</p>
          </div>
        </div>
      </main>
      <footer>
        <div className="actions">
          <Link to={`/${i18n.language}`} id="back-button">
            {t('again')}
          </Link>
          <div></div>
        </div>
      </footer>
    </div>
  );
}
