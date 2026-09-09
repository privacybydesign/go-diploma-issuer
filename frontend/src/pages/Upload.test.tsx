import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import '../i18n';
import { AppProvider } from '../AppContext';
import { ApiError, uploadDiplomas } from '../api';
import { FileResult, UploadResponse } from '../types';
import UploadPage from './Upload';

vi.mock('../api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../api')>();
  return { ...actual, uploadDiplomas: vi.fn() };
});

const uploadMock = vi.mocked(uploadDiplomas);

/** A controllable upload: the test decides when and how it ends. */
function pendingUpload() {
  let files: File[] = [];
  let finish!: (result: UploadResponse) => void;
  let fail!: (err: unknown) => void;
  uploadMock.mockImplementation((picked) => {
    files = picked;
    return new Promise((resolve, reject) => {
      finish = resolve;
      fail = reject;
    });
  });
  return {
    get files() {
      return files;
    },
    finish: (result: UploadResponse) => finish(result),
    fail: (err: unknown) => fail(err),
  };
}

// Minimal files that pass the client-side PDF check.
const havo = new File(['%PDF-1.7\n1 0 obj\nendobj\n%%EOF\n'], 'havo.pdf', { type: 'application/pdf' });
const hbo = new File(['%PDF-1.7\n2 0 obj\nendobj\n%%EOF\n'], 'hbo.pdf', { type: 'application/pdf' });
const notPdf = new File(['hello'], 'notes.txt', { type: 'text/plain' });

const validation = { valid: true, key: 'valid', signing_time: '2026-09-03T10:00:00Z', timestamp_trusted: true, checks: [] };

function acceptedFile(filename: string, qualification: string): FileResult {
  return {
    filename,
    accepted: true,
    validation,
    document: {
      document_type: 'Diploma', qualification, profiles: [], full_name: 'Anna van der Berg', date_of_birth: '1980-02-03',
      institution: 'School', place_of_issue: 'Utrecht', date_awarded: '2001-07-01', nlqf_level: '4', eqf_level: '4',
      document_number: '123', download_date: '2026-09-03', pages: 1, has_grade_list: false,
    },
  };
}

function rejectedFile(filename: string, error: string, check?: string): FileResult {
  return {
    filename,
    accepted: false,
    error,
    validation: check ? { ...validation, valid: false, key: check } : undefined,
  };
}

function renderPage() {
  render(
    <AppProvider>
      <MemoryRouter initialEntries={['/nl/upload']}>
        <UploadPage />
      </MemoryRouter>
    </AppProvider>,
  );
}

async function pick(...files: File[]) {
  fireEvent.change(screen.getByLabelText('Diploma-uittreksels (PDF)'), { target: { files } });
  await waitFor(() => expect(screen.getByRole('button', { name: /uploaden en controleren/i })).toBeEnabled());
}

describe('UploadPage', () => {
  afterEach(() => {
    // vitest runs without globals, so Testing Library does not clean up by itself.
    cleanup();
    uploadMock.mockReset();
  });

  it('explains where to get the extracts and how they are checked', () => {
    renderPage();
    expect(screen.getByText(/We controleren de digitale handtekening van elke PDF/)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /waar je je diploma-uittreksels downloadt/ })).toHaveAttribute('href', '/nl');
    expect(screen.getByRole('button', { name: 'Uploaden en controleren' })).toBeDisabled();
  });

  it('lists the picked files, refuses non-PDFs and uploads everything at once', async () => {
    const upload = pendingUpload();
    renderPage();

    fireEvent.change(screen.getByLabelText('Diploma-uittreksels (PDF)'), { target: { files: [havo, notPdf] } });
    expect(await screen.findByRole('alert')).toHaveTextContent('notes.txt: Alleen PDF-bestanden worden geaccepteerd.');
    expect(screen.getByText('havo.pdf')).toBeInTheDocument();

    await pick(hbo);
    expect(screen.getByText('hbo.pdf')).toBeInTheDocument();
    const submit = screen.getByRole('button', { name: '2 bestanden uploaden en controleren' });

    fireEvent.click(submit);
    await waitFor(() => expect(uploadMock).toHaveBeenCalledTimes(1));
    expect(upload.files.map((f) => f.name)).toEqual(['havo.pdf', 'hbo.pdf']);
    expect(screen.getByRole('status')).toHaveTextContent('De 2 uittreksels worden gecontroleerd...');
  });

  it('removes a file from the selection', async () => {
    renderPage();
    await pick(havo, hbo);
    fireEvent.click(screen.getByRole('button', { name: 'havo.pdf verwijderen' }));
    expect(screen.queryByText('havo.pdf')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Uploaden en controleren' })).toBeEnabled();
  });

  it('shows one card per file, accepted and rejected, after a partial success', async () => {
    const upload = pendingUpload();
    renderPage();
    await pick(havo, hbo);
    fireEvent.click(screen.getByRole('button', { name: /uploaden en controleren/i }));
    await waitFor(() => expect(uploadMock).toHaveBeenCalledTimes(1));

    upload.finish({
      session_id: 's1',
      person: { full_name: 'Anna van der Berg', date_of_birth: '1980-02-03' },
      files: [acceptedFile('havo.pdf', 'Hoger algemeen voortgezet onderwijs'), rejectedFile('hbo.pdf', 'error:validation-failed', 'covers_whole_file')],
      accepted: 1,
      rejected: 1,
    });

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('De handtekening van het uittreksel is geldig');
    expect(alert).toHaveTextContent('Eén bestand is niet geaccepteerd');
    expect(screen.getByText(/afgegeven aan Anna van der Berg, geboren op 1980-02-03/)).toBeInTheDocument();

    const accepted = screen.getByRole('region', { name: 'havo.pdf' });
    expect(accepted).toHaveTextContent('Gecontroleerd en uitgelezen');
    expect(accepted).toHaveTextContent('Hoger algemeen voortgezet onderwijs');
    expect(accepted).toHaveTextContent('NLQF 4 / EQF 4');

    const rejected = screen.getByRole('region', { name: 'hbo.pdf' });
    expect(rejected).toHaveTextContent('Niet geaccepteerd');
    expect(rejected).toHaveTextContent('De digitale handtekening van het document kon niet worden geverifieerd');
    expect(rejected).toHaveTextContent('De PDF is gewijzigd nadat DUO hem heeft ondertekend');

    expect(screen.getByRole('button', { name: 'Verder naar identiteitscontrole' })).toBeEnabled();
  });

  it('explains every rejection when no file was accepted', async () => {
    const upload = pendingUpload();
    renderPage();
    await pick(havo, hbo);
    fireEvent.click(screen.getByRole('button', { name: /uploaden en controleren/i }));
    await waitFor(() => expect(uploadMock).toHaveBeenCalledTimes(1));

    upload.fail(new ApiError(422, {
      error: 'error:validation-failed',
      files: [rejectedFile('havo.pdf', 'error:validation-failed', 'signature_present'), rejectedFile('hbo.pdf', 'error:not-a-diploma')],
    }));

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Geen van de 2 bestanden is geaccepteerd.');
    expect(screen.getByRole('region', { name: 'havo.pdf' })).toHaveTextContent('De PDF bevat geen digitale handtekening');
    expect(screen.getByRole('region', { name: 'hbo.pdf' })).toHaveTextContent('kon niet als diploma-uittreksel worden gelezen');
    // The selection is kept so the user can swap files.
    expect(screen.getByRole('button', { name: '2 bestanden uploaden en controleren' })).toBeEnabled();
  });

  it('tells the user when the trust anchors are unavailable', async () => {
    const upload = pendingUpload();
    renderPage();
    await pick(havo);
    fireEvent.click(screen.getByRole('button', { name: /uploaden en controleren/i }));
    await waitFor(() => expect(uploadMock).toHaveBeenCalledTimes(1));

    upload.fail(new ApiError(503, { error: 'error:validation-service-unavailable' }));

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('De handtekeningcontrole is tijdelijk niet beschikbaar');
    expect(alert).toHaveTextContent('Dit ligt niet aan je bestanden.');
  });
});
