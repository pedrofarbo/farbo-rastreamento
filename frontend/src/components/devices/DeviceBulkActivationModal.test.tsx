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
  {
    id: 'd3',
    imei: '869000000000003',
    model: 'J16',
    manufacturer: 'TKSTAR',
    protocol: 'gt06',
    firmware: '1.0',
    phoneNumber: '11999997777',
    iccid: '895522',
    status: 'ONLINE',
    lastSeenAt: '2026-10-10T12:00:00Z',
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
const bulkCalls: string[][] = [];
// Lote cujo envio falha inteiro (rede, sessão), pelo número da chamada.
let failCall = -1;
vi.mock('@/api/resources', () => ({
  smsSetupApi: {
    startBulk: async (input: { deviceIds: string[] }) => {
      bulkStartPayload = input;
      bulkCalls.push(input.deviceIds);
      if (bulkCalls.length - 1 === failCall) throw new Error('Sem conexão');
      return {
        total: input.deviceIds.length,
        started: input.deviceIds.length,
        failed: 0,
        results: input.deviceIds.map((deviceId) => ({
          deviceId,
          imei: deviceId === 'd1' ? '869000000000001' : undefined,
          success: true,
          sessionId: `sess-${deviceId}`,
          status: 'SENDING',
        })),
      };
    },
  },
}));

import { DeviceBulkActivationModal } from './DeviceBulkActivationModal';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));

async function renderModal(initialSelectedIds: string[] = [], devices: Device[] = MOCK_DEVICES) {
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
            devices={devices}
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
  bulkCalls.length = 0;
  failCall = -1;
});

const clickButton = async (label: string) => {
  const btn = Array.from(document.body.querySelectorAll('button')).find((b) => b.textContent?.includes(label));
  expect(btn).toBeDefined();
  await act(async () => {
    btn?.click();
  });
  await flush();
};

describe('DeviceBulkActivationModal', () => {
  it('renderiza os rastreadores e oculta por padrão os já ativados', async () => {
    await renderModal();

    expect(document.body.textContent).toContain('Ativação de rastreadores em massa');
    expect(document.body.textContent).toContain('869000000000001');
    expect(document.body.textContent).toContain('869000000000002');
    // d3 está ativo, portanto fica oculto por padrão
    expect(document.body.textContent).not.toContain('869000000000003');
    expect(document.body.textContent).toContain('Sem número de chip');
    expect(document.body.textContent).toContain('1 pendente(s) com chip');
    expect(document.body.textContent).toContain('1 sem número de chip');
    expect(document.body.textContent).toContain('Ocultar já ativados (1)');
  });

  it('permite exibir já ativados ao desmarcar a opção de ocultar', async () => {
    await renderModal();

    const hideCheckbox = Array.from(document.body.querySelectorAll('input[type="checkbox"]')).find(
      (input) => input.parentElement?.textContent?.includes('Ocultar já ativados'),
    ) as HTMLInputElement | undefined;
    expect(hideCheckbox).toBeDefined();
    expect(hideCheckbox?.checked).toBe(true);

    act(() => {
      hideCheckbox?.click();
    });
    await flush();

    // Agora o rastreador já ativado d3 é exibido na listagem com o badge Já ativado
    expect(document.body.textContent).toContain('869000000000003');
    expect(document.body.textContent).toContain('Já ativado');
  });

  it('permite selecionar pendentes e disparar ativação em massa', async () => {
    await renderModal();

    // Clica em "Selecionar pendentes (1)"
    const selectBtn = Array.from(document.body.querySelectorAll('button')).find((b) =>
      b.textContent?.includes('Selecionar pendentes'),
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
      force: false,
    });

    // Exibe tela de resultado com sucesso
    expect(document.body.textContent).toContain('1 de 1 ativações iniciadas com sucesso!');
    expect(document.body.textContent).toContain('Iniciado');
  });

  it('acima de 100, manda em lotes e junta os resultados mesmo com um lote falhando', async () => {
    const many: Device[] = Array.from({ length: 250 }, (_, i) => ({
      ...MOCK_DEVICES[0],
      id: `m${i}`,
      imei: `8690000000${String(i).padStart(5, '0')}`,
      phoneNumber: `1199${String(i).padStart(7, '0')}`,
    }));
    failCall = 1; // o segundo lote não chega ao servidor
    await renderModal([], many);

    await clickButton('Selecionar pendentes');
    expect(document.body.textContent).toContain('250 selecionado(s) para ativação');
    await clickButton('Ativar 250 rastreador(es)');

    expect(bulkCalls.map((ids) => ids.length)).toEqual([100, 100, 50]);
    expect(new Set(bulkCalls.flat()).size).toBe(250);
    // Os do lote que falhou aparecem com o erro e o IMEI, os outros seguiram.
    expect(document.body.textContent).toContain('150 de 250');
    expect(document.body.textContent).toContain('Sem conexão');
    expect(document.body.textContent).toContain(many[100].imei);
    // Os que falharam vêm primeiro na lista.
    const firstRow = document.body.querySelector('tbody tr');
    expect(firstRow?.textContent).toContain(many[100].imei);
    expect(firstRow?.textContent).toContain('Sem conexão');
  });
});
