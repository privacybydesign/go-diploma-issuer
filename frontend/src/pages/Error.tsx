import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { errorKeyToTranslationKey } from '../api';

export default function ErrorPage() {
  const { t, i18n } = useTranslation();
  // An optional error key can be passed as query parameter: /error?error=error:internal
  const urlParams = new URLSearchParams(window.location.search);
  const errorKey = urlParams.get('error');
  const message = errorKey ? t(errorKeyToTranslationKey(errorKey)) : t('error_default');

  return (
    <div id="container">
      <header>
        <h1>{t('error_header')}</h1>
      </header>
      <main>
        <div className="sms-form">
          <div className="imageContainer">
            <img src="/images/fail.png" alt="error" />
            <p>{message}</p>
          </div>
        </div>
      </main>
      <footer>
        <div className="actions">
          <Link to={`/${i18n.language}`} id="back-button">
            {t('back')}
          </Link>
          <div></div>
        </div>
      </footer>
    </div>
  );
}
