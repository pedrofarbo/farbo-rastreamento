// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { CustomerSummary, Device, Vehicle } from '@/types';

const MOCK_DEVICE: Device = {
  id: 'dev-1',
  imei: '869247061230011',
  model: 'TK910',
  manufacturer: 'TKSTAR',
  protocol: 'gt06',
  firmware: '1.0',
  phoneNumber: '34999990011',
  iccid: '89553202100093795330',
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
};

const MOCK_PENDING_DEVICE: Device = {
  id: 'dev-2',
  imei: '869247061230022',
  model: 'TK910',
  manufacturer: 'TKSTAR',
  protocol: 'gt06',
  firmware: '1.0',
  phoneNumber: '34999990022',
  iccid: '89553202100093795331',
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
  createdAt: '2026-09-02T12:00:00Z',
  updatedAt: '2026-09-02T12:00:00Z',
};

const MOCK_VEHICLE: Vehicle = {
  id: 'veh-1',
  name: 'Caminhão 01',
  kind: 'CAR',
  plate: 'ABC1D23',
  brand: 'Volvo',
  model: 'FH 540',
  year: 2022,
  color: 'Branco',
  speedLimitKmh: 90,
  deviceId: 'dev-1',
  ownerId: 'cust-1',
  historyRetentionDays: null,
  groupId: null,
  custom1: '',
  custom2: '',
  createdAt: '2026-09-01T12:00:00Z',
  updatedAt: '2026-09-01T12:00:00Z',
};

const MOCK_CUSTOMER: CustomerSummary = {
  id: 'cust-1',
  name: 'Transportadora Silva',
  email: 'silva@transporte.test',
  phone: '34988887777',
  document: '12345678000199',
  active: true,
  createdAt: '2026-08-01T12:00:00Z',
  activeSubscriptions: 1,
  freeSubscriptions: 0,
  vehicleCount: 1,
  sharedVehicles: 0,
  openInvoices: 0,
  overdueInvoices: 0,
  openAmountCents: 0,
  suspended: false, organizationId: null, organizationName: '',
};

// Os rastreadores recusados por falta de cadastro, e o que foi salvo.
let unknownAttempts: unknown[] = [];
const saved: { op: string; id?: string; input: Record<string, unknown> }[] = [];

vi.mock('@/api/resources', () => ({
  devicesApi: {
    list: async () => [MOCK_DEVICE, MOCK_PENDING_DEVICE],
    create: async (input: Record<string, unknown>) => {
      saved.push({ op: 'create', input });
      return MOCK_DEVICE;
    },
    update: async (id: string, input: Record<string, unknown>) => {
      saved.push({ op: 'update', id, input });
      return MOCK_DEVICE;
    },
    unknown: async () => unknownAttempts,
    provisioning: async () => ({ device: MOCK_DEVICE, commands: [], warning: '' }),
  },
  vehiclesApi: {
    list: async () => [MOCK_VEHICLE],
    create: async () => MOCK_VEHICLE,
  },
  // O estoque: um chip livre e o que já está no MOCK_DEVICE.
  chipsApi: {
    list: async () => [
      { id: 'chip-livre', iccid: '89553202100093795348', phoneNumber: '(34) 99999-0001', carrier: 'Algar', apn: 'ss.algar.br',
        apnUser: 'algar', apnPasswordSet: true, notes: '', device: null, createdAt: '', updatedAt: '' },
      { id: 'chip-em-uso', iccid: '89553202100093795330', phoneNumber: '', carrier: '', apn: '', apnUser: '', apnPasswordSet: false,
        notes: '', device: { id: MOCK_DEVICE.id, imei: MOCK_DEVICE.imei }, createdAt: '', updatedAt: '' },
    ],
  },
  customersApi: {
    list: async () => [MOCK_CUSTOMER],
  },
  diagnosticsApi: {
    protocols: async () => [],
  },
  smsSetupApi: {
    usage: async () => ({ currentMonth: 0, limit: 100 }),
    get: async () => ({
      enabled: true,
      from: 'SMS Market',
      plan: {
        phone: '+5534999990011',
        steps: [
          { kind: 'APN', label: 'APN do chip', text: 'APN,zap.vivo.com.br,vivo,***#' },
        ],
        problems: [],
        unlock: { kind: 'UNLOCK', label: 'Destravar', text: 'CMDLOCK#' },
        query: { kind: 'PARAM', label: 'Pedir config', text: 'PARAM#' },
      },
      session: null,
    }),
    start: async () => ({ id: 'sess-1', status: 'SENDING' }),
    startBulk: async () => ({ total: 1, started: 1, failed: 0, results: [] }),
    cancel: async () => ({ id: 'sess-1', status: 'CANCELED' }),
  },
  deviceInputFrom: () => ({ imei: '' }),
}));

vi.mock('@/stores/AuthContext', () => ({
  useAuth: () => ({ canOperate: true, canManage: true }),
}));

vi.mock('@/components/ui/Toast', () => ({
  useToast: () => ({ notify: () => {} }),
}));

import { DevicesPage } from './DevicesPage';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));

/**
 * Espera o texto aparecer na página: o que os modais carregam (a configuração
 * por SMS, por exemplo) pode levar mais de um ciclo, ainda mais com a suíte
 * inteira rodando junto. Desiste em 3 s, e aí a conferência falha.
 */
async function waitForText(text: string) {
  const deadline = Date.now() + 3000;
  while (!document.body.textContent?.includes(text) && Date.now() < deadline) await flush();
  expect(document.body.textContent).toContain(text);
}

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={client}>
        <BrowserRouter>
          <DevicesPage />
        </BrowserRouter>
      </QueryClientProvider>,
    ),
  );
  await flush();
  return host;
}

afterEach(() => {
  document.body.innerHTML = '';
  unknownAttempts = [];
  saved.length = 0;
  vi.restoreAllMocks();
});

const typeInto = (input: HTMLInputElement, value: string) => {
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
  setter.call(input, value);
  input.dispatchEvent(new Event('input', { bubbles: true }));
};
const buttonNamed = (label: string) =>
  Array.from(document.querySelectorAll('button')).find((b) => b.textContent?.trim() === label) as HTMLButtonElement | undefined;

describe('DevicesPage', () => {
  it('exibe colunas de Placa, Cliente e Número na listagem de rastreadores', async () => {
    const host = await render();

    const headers = Array.from(host.querySelectorAll('th')).map((th) => th.textContent?.trim());
    expect(headers).toContain('IMEI');
    expect(headers).toContain('Veículo');
    expect(headers).toContain('Placa');
    expect(headers).toContain('Cliente');
    expect(headers).toContain('Número');
    expect(headers).toContain('Protocolo');
    expect(headers).toContain('Situação');

    // Confere os dados do rastreador na primeira linha
    const row = host.querySelector('tbody tr');
    expect(row).not.toBeNull();
    expect(row?.textContent).toContain('869247061230011');
    expect(row?.textContent).toContain('Caminhão 01');
    expect(row?.textContent).toContain('ABC1D23');
    expect(row?.textContent).toContain('Transportadora Silva');
    expect(row?.textContent).toContain('(34) 9-9999-0011');
  });

  it('exibe botão "Importar em massa" e abre o modal ao clicar', async () => {
    const host = await render();

    const bulkButton = Array.from(host.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Importar em massa',
    );
    expect(bulkButton).toBeDefined();

    act(() => {
      bulkButton?.click();
    });
    await flush();

    // Modal de importação em massa aberto
    await waitForText('Adicionar rastreadores em massa');
    await waitForText('Baixar modelo CSV');
  });

  it('exibe botão "Ativar" para pendentes e "Reativar" para já ativados', async () => {
    const host = await render();

    const rows = Array.from(host.querySelectorAll('tbody tr'));
    expect(rows).toHaveLength(2);

    // Linha 1 (MOCK_DEVICE) está ONLINE/ativo -> botão "Reativar"
    const row1ReactivateBtn = Array.from(rows[0].querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Reativar',
    );
    expect(row1ReactivateBtn).toBeDefined();

    // Linha 2 (MOCK_PENDING_DEVICE) está OFFLINE/sem lastSeenAt -> botão "Ativar"
    const row2ActivateBtn = Array.from(rows[1].querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Ativar',
    );
    expect(row2ActivateBtn).toBeDefined();

    // Clicar em "Ativar" abre o modal do rastreador pendente
    act(() => {
      row2ActivateBtn?.click();
    });
    await flush();

    await waitForText('Ativar rastreador 869247061230022');
    await waitForText('Configurar por SMS');
  });

  it('permite filtrar por "Pendentes de ativação" e "Já ativados"', async () => {
    const host = await render();

    expect(host.textContent).toContain('Todos (2)');
    expect(host.textContent).toContain('Pendentes de ativação (1)');
    expect(host.textContent).toContain('Já ativados (1)');

    const pendingFilterBtn = Array.from(host.querySelectorAll('button')).find((b) =>
      b.textContent?.includes('Pendentes de ativação'),
    );
    expect(pendingFilterBtn).toBeDefined();

    // Filtra por pendentes
    act(() => {
      pendingFilterBtn?.click();
    });
    await flush();

    const rowsPending = Array.from(host.querySelectorAll('tbody tr'));
    expect(rowsPending).toHaveLength(1);
    expect(rowsPending[0].textContent).toContain('869247061230022');
    expect(rowsPending[0].textContent).not.toContain('869247061230011');

    // Filtra por já ativados
    const activatedFilterBtn = Array.from(host.querySelectorAll('button')).find((b) =>
      b.textContent?.includes('Já ativados'),
    );
    expect(activatedFilterBtn).toBeDefined();

    act(() => {
      activatedFilterBtn?.click();
    });
    await flush();

    const rowsActivated = Array.from(host.querySelectorAll('tbody tr'));
    expect(rowsActivated).toHaveLength(1);
    expect(rowsActivated[0].textContent).toContain('869247061230011');
    expect(rowsActivated[0].textContent).not.toContain('869247061230022');
  });

  it('exibe botão "Ativação em massa" no header e abre o modal de ativação em massa', async () => {
    const host = await render();

    const bulkActivateBtn = Array.from(host.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Ativação em massa',
    );
    expect(bulkActivateBtn).toBeDefined();

    act(() => {
      bulkActivateBtn?.click();
    });
    await flush();

    await waitForText('Ativação de rastreadores em massa');
    expect(document.body.textContent).toContain('869247061230011');
  });

  it('permite selecionar rastreadores por checkbox e exibe barra de ações em lote', async () => {
    const host = await render();

    const rowCheckbox = host.querySelector('tbody tr input[type="checkbox"]') as HTMLInputElement;
    expect(rowCheckbox).not.toBeNull();

    act(() => {
      rowCheckbox.click();
    });
    await flush();

    expect(host.textContent).toContain('1 rastreador(es) selecionado(s)');
    expect(host.textContent).toContain('Ativar selecionados');
  });

  it('IMEI fora do padrão (o caso de 16 dígitos): o aviso aparece e o salvar pede a conferência', async () => {
    await render();
    act(() => buttonNamed('Novo rastreador')!.click());
    await flush();
    const imei = Array.from(document.querySelectorAll('label'))
      .find((l) => l.textContent === 'IMEI')!
      .parentElement!.querySelector('input') as HTMLInputElement;
    act(() => typeInto(imei, '8658930088121639'));
    await flush();
    expect(document.body.textContent).toContain('Este IMEI tem 16 dígitos');
    expect(buttonNamed('Salvar')!.disabled).toBe(true);
    const confirm = Array.from(document.querySelectorAll('label'))
      .find((l) => l.textContent?.includes('Conferi na etiqueta'))!
      .querySelector('input') as HTMLInputElement;
    act(() => confirm.click());
    await flush();
    expect(buttonNamed('Salvar')!.disabled).toBe(false);
    act(() => buttonNamed('Salvar')!.click());
    await flush();
    expect(saved[0]).toEqual({ op: 'create', input: expect.objectContaining({ imei: '8658930088121639', imeiConfirmed: true }) });

    // Corrigido para o IMEI certo, o aviso some.
    act(() => buttonNamed('Novo rastreador')!.click());
    await flush();
    const again = Array.from(document.querySelectorAll('label'))
      .find((l) => l.textContent === 'IMEI')!
      .parentElement!.querySelector('input') as HTMLInputElement;
    act(() => typeInto(again, '865893088121639'));
    await flush();
    expect(document.body.textContent).not.toContain('Este IMEI');
    expect(buttonNamed('Salvar')!.disabled).toBe(false);
  });

  it('o chip do estoque: escolhido, os dados vêm dele; o que está em outro rastreador não aparece', async () => {
    await render();
    act(() => buttonNamed('Novo rastreador')!.click());
    await flush();
    const chip = Array.from(document.querySelectorAll('label'))
      .find((l) => l.textContent === 'Chip')!
      .parentElement!.querySelector('select') as HTMLSelectElement;
    const options = Array.from(chip.options).map((o) => o.textContent);
    expect(options).toEqual(['Digitar os dados do chip', '8955 3202 1000 9379 5348 · (34) 9-9999-0001 · Algar']);
    expect(document.body.textContent).toContain('Linha do chip');
    act(() => {
      Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')!.set!.call(chip, 'chip-livre');
      chip.dispatchEvent(new Event('change', { bubbles: true }));
    });
    await flush();
    // Os campos digitados saem; fica o resumo do chip.
    expect(document.body.textContent).not.toContain('Linha do chip');
    expect(document.body.textContent).toContain('Linha (34) 9-9999-0001 · ICCID 8955 3202 1000 9379 5348 · APN ss.algar.br (algar, com senha)');
    const imei = Array.from(document.querySelectorAll('label'))
      .find((l) => l.textContent === 'IMEI')!
      .parentElement!.querySelector('input') as HTMLInputElement;
    act(() => typeInto(imei, '869247061230055'));
    await flush();
    act(() => buttonNamed('Salvar')!.click());
    await flush();
    expect(saved[0]).toEqual({ op: 'create', input: expect.objectContaining({ chipId: 'chip-livre', iccid: '89553202100093795348' }) });
  });

  it('o rastreador recusado sem cadastro: corrige o cadastro parecido ou abre um novo', async () => {
    unknownAttempts = [
      {
        imei: '869247061230019', protocol: 'gt06', remoteAddr: '187.32.97.125:58772', firstSeenAt: '2026-10-13T23:43:00Z',
        lastSeenAt: new Date().toISOString(), attempts: 25, suggestion: { deviceId: 'dev-2', imei: '869247061230022' },
      },
      {
        imei: '359710049095095', protocol: 'gt06', remoteAddr: '', firstSeenAt: '2026-10-13T23:43:00Z',
        lastSeenAt: new Date().toISOString(), attempts: 3, suggestion: null,
      },
    ];
    vi.spyOn(window, 'confirm').mockReturnValue(true);
    await render();
    await flush();
    expect(document.body.textContent).toContain('Rastreadores tentando conectar sem cadastro');
    expect(document.body.textContent).toContain('provável erro de digitação no cadastro');
    act(() => buttonNamed('Corrigir para este IMEI')!.click());
    await flush();
    expect(saved[0]).toEqual({ op: 'update', id: 'dev-2', input: expect.objectContaining({ imei: '869247061230019', imeiConfirmed: true }) });

    // Sem cadastro parecido: abre o cadastro com o IMEI.
    act(() => buttonNamed('Cadastrar')!.click());
    await flush();
    const imei = Array.from(document.querySelectorAll('label'))
      .find((l) => l.textContent === 'IMEI')!
      .parentElement!.querySelector('input') as HTMLInputElement;
    expect(imei.value).toBe('359710049095095');
  });
});
