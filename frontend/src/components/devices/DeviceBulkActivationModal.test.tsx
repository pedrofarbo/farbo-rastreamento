// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ToastProvider } from '@/components/ui/Toast';
import type { Device, Vehicle } from '@/types';

const MOCK_DEVICES: Device[] = [
  {
    id: 'd1',
    imei: '869000000000001',
    model: 'J16',
    manufacturer: 'TKSTAR',
    protocol: 'gt06',
    firmware: '1.0',
    phoneNumber: '11999998888',
    iccid: '895511',
    status: 'OFFLINE',
    lastSeenAt: null,
    apn: '',
    apnUser: '',
    serverHost: '',
    serverPort: null,
    reportIntervalSeconds: 30,
    heartbeatIntervalSeconds: 60,
    commandOverrides: {},
    notes: '',
    createdAt: '2026-09-01T12:00:00Z',
    updatedAt: '2026-10-10T12:00:00Z',
  },
  {
    id: 'd2',
    imei: '869000000000002',
    model: 'J16',
    manufacturer: 'TKSTAR',
    protocol: 'gt06',
    firmware: '1.0',
    phoneNumber: '', // sem chip
    iccid: '',
    status: 'OFFLINE',
    lastSeenAt: null,
    apn: '',
    apnUser: '',
    serverHost: '',
    serverPort: null,
    reportIntervalSeconds: 30,
    heartbeatIntervalSeconds: 60,
    commandOverrides: {},
    notes: '',
    createdAt: '2026-09-01T12:00:00Z',
    updatedAt: '2026-10-10T12:00:00Z',
  },
];

const MOCK_VEHICLES: Vehicle[] = [
  {
    id: 'v1',
    name: 'Gol Prata',
    kind: 'CAR',
    plate: 'ABC1234',
    brand: 'VW',
    model: 'Gol',
    year: 2020,
    color: 'Prata',
    speedLimitKmh: 100,
    deviceId: 'd1',
    ownerId: null,
    historyRetentionDays: null,
    groupId: null,
    custom1: '',
    custom2: '',
    createdAt: '2026-09-01T12:00:00Z',
    updatedAt: '2026-09-01T12:00:00Z',
  },
];

let bulkStartPayload: unknown = null;
vi.mock('@/api/resources', () => ({
  smsSetupApi: {
    startBulk: async (input: unknown) => {
      bulkStartPayload = input;
      return {
        total: 1,
        started: 1,
        failed: 0,
        results: [
          {
            deviceId: 'd1',
            imei: '869000000000001',
            success: true,
            sessionId: 'sess-1',
            status: 'SENDING',
          },
        ],
      };
    },
  },
}));

import { DeviceBulkActivationModal } from './DeviceBulkActivationModal';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));

async function renderModal(initialSelectedIds: string[] = []) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={client}>
        <ToastProvider>
          <DeviceBulkActivationModal
            open
            onClose={() => {}}
            devices={MOCK_DEVICES}
            vehicles={MOCK_VEHICLES}
            initialSelectedIds={initialSelectedIds}
          />
        </ToastProvider>
      </QueryClientProvider>,
    ),
  );
  await flush();
  return host;
}

afterEach(() => {
  document.body.innerHTML = '';
  bulkStartPayload = null;
});

describe('DeviceBulkActivationModal', () => {
  it('renderiza os rastreadores e identifica os que não possuem chip cadastrado', async () => {
    await renderModal();

    expect(document.body.textContent).toContain('Ativação de rastreadores em massa');
    expect(document.body.textContent).toContain('869000000000001');
    expect(document.body.textContent).toContain('869000000000002');
    expect(document.body.textContent).toContain('Sem número de chip');
    expect(document.body.textContent).toContain('1 com chip cadastrado');
    expect(document.body.textContent).toContain('1 sem número de chip');
  });

  it('permite selecionar com chip e disparar ativação em massa', async () => {
    await renderModal();

    // Clica em "Selecionar com chip (1)"
    const selectBtn = Array.from(document.body.querySelectorAll('button')).find((b) =>
      b.textContent?.includes('Selecionar com chip'),
    );
    expect(selectBtn).toBeDefined();

    act(() => {
      selectBtn?.click();
    });
    await flush();

    expect(document.body.textContent).toContain('1 selecionado(s) para ativação');

    // Botão de envio "Ativar 1 rastreador(es)"
    const submitBtn = Array.from(document.body.querySelectorAll('button')).find((b) =>
      b.textContent?.includes('Ativar 1 rastreador(es)'),
    );
    expect(submitBtn).toBeDefined();

    await act(async () => {
      submitBtn?.click();
    });
    await flush();

    expect(bulkStartPayload).toEqual({
      deviceIds: ['d1'],
      unlock: false,
      query: false,
    });

    // Exibe tela de resultado com sucesso
    expect(document.body.textContent).toContain('1 de 1 ativações iniciadas com sucesso!');
    expect(document.body.textContent).toContain('Iniciado');
  });
});
