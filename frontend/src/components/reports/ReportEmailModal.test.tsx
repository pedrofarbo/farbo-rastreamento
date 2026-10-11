// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { ReportScheduleInput } from '@/api/resources';
import type { ReportSchedules } from '@/types';

const calls: unknown[] = [];
let view: ReportSchedules;
vi.mock('@/api/resources', () => ({
  reportSchedulesApi: {
    list: async () => view,
    create: async (input: ReportScheduleInput) => {
      calls.push(['create', input]);
      return { id: 'n1', lastSentAt: null, createdAt: '', groupId: null, ...input };
    },
    remove: async (id: string) => {
      calls.push(['remove', id]);
    },
  },
}));
vi.mock('@/components/ui/Toast', () => ({ useToast: () => ({ notify: () => undefined }) }));

import { ReportEmailModal, invalidEmail, parseEmails } from './ReportEmailModal';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');
const button = (label: string) =>
  Array.from(document.querySelectorAll('button')).find((b) => b.textContent === label) as HTMLButtonElement;
function type(el: HTMLTextAreaElement | HTMLSelectElement, value: string) {
  const proto = el instanceof HTMLSelectElement ? HTMLSelectElement.prototype : HTMLTextAreaElement.prototype;
  Object.getOwnPropertyDescriptor(proto, 'value')?.set?.call(el, value);
  el.dispatchEvent(new Event(el instanceof HTMLSelectElement ? 'change' : 'input', { bubbles: true }));
}

const groups = [
  { id: 'g1', name: 'Norte', vehicles: 4 },
  { id: 'g2', name: 'Sul', vehicles: 2 },
];

async function render(restricted = false) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient()}>
        <ReportEmailModal open onClose={() => undefined} groups={groups} restricted={restricted} />
      </QueryClientProvider>,
    ),
  );
  await flush();
}

afterEach(() => {
  document.body.innerHTML = '';
  calls.length = 0;
});

describe('relatório por e-mail', () => {
  it('lê os e-mails a mais como a pessoa digita', () => {
    expect(parseEmails(' Socio@Frota.test, ops@frota.test;\nsocio@frota.test ')).toEqual(['socio@frota.test', 'ops@frota.test']);
    expect(invalidEmail(['ok@frota.test', 'sem-arroba'])).toBe('sem-arroba');
  });

  it('mostra os agendados, agenda um com e-mails a mais e para outro', async () => {
    view = {
      schedules: [
        { id: 's1', frequency: 'monthly', groupId: 'g2', extraRecipients: ['dir@frota.test'], lastSentAt: null, createdAt: '' },
      ],
      maxSchedules: 5,
      maxExtraRecipients: 5,
      canAddRecipients: true,
    };
    await render();
    expect(text()).toContain('Todo mês · Sul');
    expect(text()).toContain('também para dir@frota.test');

    const weekly = document.querySelectorAll('input[name="report-frequency"]')[0] as HTMLInputElement;
    await act(async () => weekly.click());
    const extras = document.querySelector('textarea') as HTMLTextAreaElement;
    await act(async () => type(extras, 'gerente@frota, x'));
    await act(async () => button('Agendar').click());
    expect(text()).toContain('E-mail inválido: gerente@frota');
    await act(async () => type(extras, 'Gerente@Frota.test, fin@frota.test'));
    await act(async () => button('Agendar').click());
    await flush();
    expect(calls).toEqual([
      ['create', { frequency: 'weekly', groupId: null, extraRecipients: ['gerente@frota.test', 'fin@frota.test'] }],
    ]);

    calls.length = 0;
    await act(async () => button('Parar').click());
    await flush();
    expect(calls).toEqual([['remove', 's1']]);
  });

  it('quem está restrito escolhe um dos seus grupos; o Visualizador manda só para si', async () => {
    view = { schedules: [], maxSchedules: 5, maxExtraRecipients: 5, canAddRecipients: false };
    await render(true);
    const select = document.querySelector('select') as HTMLSelectElement;
    expect(Array.from(select.options).map((o) => o.textContent)).toEqual(['Grupo Norte', 'Grupo Sul']);
    expect(document.querySelector('textarea')).toBeNull();
    await act(async () => type(select, 'g2'));
    await act(async () => button('Agendar').click());
    await flush();
    expect(calls).toEqual([['create', { frequency: 'weekly', groupId: 'g2', extraRecipients: [] }]]);
  });
});
