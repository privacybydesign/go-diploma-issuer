import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { useAppContext } from '../AppContext';

const DUO_URL_NL = 'https://duo.nl/particulier/uittreksel-diplomagegevens-downloaden.jsp';
const DUO_URL_EN = 'https://duo.nl/particulier/extract-of-your-diploma.jsp';
const MIJN_DIPLOMAS_URL = 'https://mijndiplomas.nl';

export default function IndexPage() {
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();
  const { setUpload } = useAppContext();
  const duoUrl = i18n.language.startsWith('en') ? DUO_URL_EN : DUO_URL_NL;

  const start = (e: React.FormEvent) => {
    e.preventDefault();
    setUpload(undefined);
    navigate(`/${i18n.language}/upload`);
  };

  return (
    <form id="container" onSubmit={start}>
      <header>
        <h1>{t('index_header')}</h1>
      </header>
      <main>
        <div className="sms-form">
          <p>{t('index_explanation')}</p>

          <section className="help-box" aria-labelledby="where-header">
            <h2 id="where-header" className="help-header">{t('where_header')}</h2>
            <p>{t('where_intro')}</p>
            <ol>
              <li>
                {t('where_step_1_before')}{' '}
                <a href={MIJN_DIPLOMAS_URL} target="_blank" rel="noopener noreferrer">mijndiplomas.nl</a>{' '}
                {t('where_step_1_after')}
              </li>
              <li>{t('where_step_2')}</li>
              <li>{t('where_step_3')}</li>
              <li>{t('where_step_4')}</li>
            </ol>
            <p className="details">{t('where_coverage')}</p>
            <p className="details">
              {t('where_more_before')}{' '}
              <a href={duoUrl} target="_blank" rel="noopener noreferrer">{t('where_more_link')}</a>.
            </p>
          </section>

          <b>{t('index_steps')}</b>
          <ol>
            <li>{t('index_step_1')}</li>
            <li>{t('index_step_2')}</li>
            <li>{t('index_step_3')}</li>
          </ol>
          <p className="details">{t('index_privacy')}</p>
        </div>
      </main>
      <footer>
        <div className="actions">
          <div></div>
          <button id="submit-button" type="submit">{t('index_start')}</button>
        </div>
      </footer>
    </form>
  );
}
