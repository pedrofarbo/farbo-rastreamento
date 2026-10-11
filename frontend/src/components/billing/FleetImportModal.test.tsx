// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { FleetImportRequest } from '@/api/resources';
import type { FleetImportResult, FleetSettings } from '@/types';

const calls: FleetImportRequest[] = [];
// A conferência do servidor recusa a placa da 2ª linha na primeira vez.
let refuse = true;
vi.mock('@/api/resources', () => ({
  catalogApi: {
    get: async () => ({
      planName: 'Plano Mensal', planPriceCents: 6990, defaultDueDay: 10, equipmentName: 'Rastreador J16',
      equipmentPriceCents: 15000, setupDueDays: 3, equipmentMaxInstallments: 10, launchPromo: null,
    }),
  },
  customersApi: {
    importFleet: async (_customerId: string, input: FleetImportRequest) => {
      calls.push(input);
      if (refuse) {
        const { ApiError } = await import('@/api/client');
        throw new ApiError(422, '1 problemas na planilha', {
          code: 'IMPORT_PROBLEMS',
          problems: [{ line: 2, message: 'a placa XYZ9A99 já está cadastrada' }],
        });
      }
      return {
        vehicles: input.rows.length, installed: 1, newGroups: ['Norte'], equipmentCents: input.equipmentCents * input.rows.length,
        setupInvoice: null, dryRun: Boolean(input.dryRun),
      } satisfies FleetImportResult;
    },
  },
}));

import { FleetImportModal } from './FleetImportModal';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');
const button = (label: string | RegExp) =>
  Array.from(document.querySelectorAll('button')).find((b) =>
    typeof label === 'string' ? b.textContent === label : label.test(b.textContent ?? ''),
  ) as HTMLButtonElement;

const fleet: FleetSettings = {
  groups: [],
  labels: { label1: 'Nº do contrato', label2: '' },
  maxGroups: 100,
  maxCustomLength: 100,
  maxLabelLength: 40,
};

async function upload(content: string) {
  const input = document.querySelector('input[type="file"]') as HTMLInputElement;
  const file = new File([content], 'frota.csv', { type: 'text/csv' });
  Object.defineProperty(input, 'files', { value: [file], configurable: true });
  await act(async () => input.dispatchEvent(new Event('change', { bubbles: true })));
  await flush();
  await flush();
}

afterEach(() => {
  document.body.innerHTML = '';
  calls.length = 0;
  refuse = true;
});

describe('a frota por planilha', () => {
  it('confere, mostra os problemas pela linha da planilha e importa com o plano da conta', async () => {
    const done: FleetImportResult[] = [];
    const host = document.createElement('div');
    document.body.appendChild(host);
    await act(async () =>
      createRoot(host).render(
        <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
          <FleetImportModal
            open
            fleet={fleet}
            admin={{ customerId: 'c1', devices: [], deliveryAddress: null, currentPlan: null, accountPlan: null, accountDueDay: 15 }}
            onClose={() => undefined}
            onDone={(result) => done.push(result)}
          />
        </QueryClientProvider>,
      ),
    );
    await flush();
    expect(text()).toContain('Nº do contrato');

    // Um erro que a própria tela pega: nem vai ao servidor.
    await upload('Nome;Placa\nVan 01;ABC1D23\n;SEM1N00\n');
    expect(text()).toContain('Linha 3');
    expect(text()).toContain('Falta o nome do veículo.');
    expect(calls).toHaveLength(0);

    // O servidor recusa a placa do 2º veículo, que está na linha 4 da planilha.
    const csv = 'Nome;Placa;Grupo;IMEI do rastreador\nVan 01;ABC1D23;Zona Sul;869247061900001\n\nVan 02;XYZ9A99;Norte;\n';
    await upload(csv);
    expect(calls).toHaveLength(1);
    expect(calls[0].dryRun).toBe(true);
    expect(text()).toContain('Linha 4');
    expect(text()).toContain('já está cadastrada');
    expect(button('Continuar').disabled).toBe(true);

    // Planilha corrigida: o resumo, depois a cobrança e a importação.
    refuse = false;
    await act(async () => button('Escolher outra planilha').click());
    await upload(csv);
    expect(text()).toContain('Grupos novos');
    expect(text()).toContain('Norte');
    await act(async () => button('Continuar').click());
    expect(text()).toContain('todo dia 15');
    expect(text()).toContain('2 × R$');
    await act(async () => button('Importar 2 veículos').click());
    await flush();

    const final = calls[calls.length - 1];
    expect(final.dryRun).toBe(false);
    expect(final.equipmentCents).toBe(15000);
    expect(final.plan.dueDay).toBe(15);
    expect(final.rows).toEqual([
      { vehicle: { name: 'Van 01', plate: 'ABC1D23' }, group: 'Zona Sul', imei: '869247061900001' },
      { vehicle: { name: 'Van 02', plate: 'XYZ9A99' }, group: 'Norte', imei: '' },
    ]);
    expect(done).toHaveLength(1);
  });
});
