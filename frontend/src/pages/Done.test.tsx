import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import '../i18n';
import { AppProvider, useAppContext } from '../AppContext';
import { UploadResponse } from '../types';
import DonePage from './Done';

const upload: UploadResponse = {
  session_id: 's1',
  person: { full_name: 'Anna van der Berg', date_of_birth: '1980-02-03' },
  files: [],
  accepted: 2,
  rejected: 0,
};

/** Exposes the flow state so the test can see the done page clear it. */
function StateProbe() {
  const { upload } = useAppContext();
  return <output data-testid="session">{upload?.session_id ?? 'none'}</output>;
}

function renderDone(count?: number) {
  render(
    <AppProvider initialUpload={upload}>
      <MemoryRouter initialEntries={[{ pathname: '/nl/done', state: count === undefined ? null : { count } }]}>
        <Routes>
          <Route path="/:lang/done" element={<DonePage />} />
        </Routes>
      </MemoryRouter>
      <StateProbe />
    </AppProvider>,
  );
}

describe('DonePage', () => {
  afterEach(() => {
    // vitest runs without globals, so Testing Library does not clean up by itself.
    cleanup();
  });

  it('confirms the diplomas are in the app and offers to add more', () => {
    renderDone(2);
    expect(screen.getByRole('heading', { name: "Diploma's toegevoegd" })).toBeInTheDocument();
    expect(screen.getByText("Je 2 diploma's staan nu als aparte kaartjes in je Yivi-app.")).toBeInTheDocument();
    expect(screen.getByText('Bedankt voor het gebruik van Yivi, je kunt deze pagina nu sluiten.')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: "Meer diploma's toevoegen" })).toHaveAttribute('href', '/nl');
  });

  it('speaks of one diploma when the count is missing', () => {
    renderDone();
    expect(screen.getByRole('heading', { name: 'Diploma toegevoegd' })).toBeInTheDocument();
    expect(screen.getByText('Je diploma staat nu als kaartje in je Yivi-app.')).toBeInTheDocument();
  });

  it('forgets the upload session so the flow starts clean', async () => {
    renderDone(1);
    await waitFor(() => expect(screen.getByTestId('session')).toHaveTextContent('none'));
  });
});
