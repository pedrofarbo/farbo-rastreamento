// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ToastProvider } from '@/components/ui/Toast';
import type { SmsSession, SmsSetupView } from '@/types';

let view: SmsSetupView;
const started: unknown[] = [];
let startResult: () => Promise<SmsSession>;
vi.mock('@/api/resources', () => ({
  smsSetupApi: {
    get: async () => view,
    start: (deviceId: string, input: unknown) => {
      started.push([deviceId, input]);
      return startResult();
    },
    cancel: async () => ({ ...view.session, status: 'CANCELED' }),
  },
}));

import { SmsSetupPanel, phoneLabel, stepStatus } from './SmsSetupPanel';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const button = (label: string) => Array.from(document.querySelectorAll('button')).find((b) => b.textContent === label) as HTMLButtonElement;

const PLAN: SmsSetupView['plan'] = {
  phone: '+5511999998888',
  steps: [
    { kind: 'APN', label: 'APN do chip', text: 'APN,zap.vivo.com.br,vivo,***#' },
    { kind: 'SERVER', label: 'Servidor da Farbo', text: 'SERVER,1,farborastreadores.com.br,5000,0#' },
    { kind: 'GMT', label: 'Fuso horário (UTC)', text: 'GMT,W,0,0#' },
    { kind: 'TIMER', label: 'Posição a cada 30s em movimento, 3600s parado', text: 'TIMER,30,3600#' },
  ],
  problems: [],
  unlock: { kind: 'UNLOCK', label: 'Destravar o canal de comandos', text: 'CMDLOCK,123456,0#' },
  query: { kind: 'PARAM', label: 'Pedir a configuração de volta', text: 'PARAM#' },
};

const session = (over: Partial<SmsSession>): SmsSession => ({
  id: 's1', deviceId: 'd1', fulfillmentId: 'f1', phone: '+5511999998888', status: 'SENDING', error: '', note: '',
  steps: PLAN.steps.map((s, i) => ({ ...s, status: i === 0 ? 'delivered' : i === 1 ? 'sent' : 'pending', error: '', sentAt: i < 2 ? '2026-10-06T12:00:00Z' : null })),
  replies: [], waitingSince: null, connectedAt: null, finishedAt: null, createdAt: '2026-10-06T12:00:00Z', ...over,
});

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <MemoryRouter>
          <ToastProvider>
            <SmsSetupPanel deviceId="d1" fulfillmentId="f1" />
          </ToastProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  );
  await flush();
  return host;
}

afterEach(() => {
  document.body.innerHTML = '';
  started.length = 0;
});

describe('SmsSetupPanel', () => {
  it('mostra os comandos e manda com os opcionais escolhidos', async () => {
    view = { enabled: true, from: '+15005550006', inboundUrl: 'https://x/api/twilio/inbound', plan: PLAN, session: null };
    startResult = async () => {
      // Como a API: a leitura seguinte já traz a configuração.
      view = { ...view, session: session({}) };
      return view.session!;
    };
    const host = await render();
    expect(host.textContent).toContain('Chip (11) 99999-8888');
    expect(host.textContent).toContain('APN,zap.vivo.com.br,vivo,***#');
    expect(host.textContent).toContain('SERVER,1,farborastreadores.com.br,5000,0#');
    expect(host.textContent).not.toContain('CMDLOCK');
    const boxes = Array.from(host.querySelectorAll('input[type=checkbox]')) as HTMLInputElement[];
    await act(async () => boxes[0].click());
    expect(host.textContent).toContain('CMDLOCK,123456,0#');
    await act(async () => button('Configurar por SMS').click());
    await flush();
    expect(started).toEqual([['d1', { fulfillmentId: 'f1', unlock: true, query: false }]]);
    // Andando: o progresso e o botão de interromper.
    expect(host.textContent).toContain('Mandando os comandos');
    expect(host.textContent).toContain('Entregue');
    expect(button('Interromper')).toBeTruthy();
    expect(button('Configurar por SMS')).toBeUndefined();
  });

  it('o que falta no cadastro e o Twilio desligado travam o envio', async () => {
    view = {
      enabled: false, from: '', inboundUrl: '', session: null,
      plan: { ...PLAN, phone: '', steps: [], problems: ['Cadastre o número do chip (com DDD) no rastreador.'] },
    };
    const host = await render();
    expect(host.textContent).toContain('O envio de SMS não está configurado');
    expect(host.textContent).toContain('Cadastre o número do chip');
    expect(button('Configurar por SMS').disabled).toBe(true);
  });

  it('o fim: conectou, ou o motivo de não dar certo (e mandar de novo)', async () => {
    view = { enabled: true, from: '+1', inboundUrl: '', plan: PLAN, session: session({
      status: 'DONE', note: 'O rastreador conectou e o pedido passou para "Configurado".',
      replies: [{ body: 'SERVER OK', at: '2026-10-06T12:01:00Z' }],
    }) };
    let host = await render();
    expect(host.textContent).toContain('Rastreador conectado');
    expect(host.textContent).toContain('passou para "Configurado"');
    expect(host.textContent).toContain('SERVER OK');
    document.body.innerHTML = '';
    view = { ...view, session: session({ status: 'TIMEOUT', error: 'O rastreador não conectou em 20 minutos.' }) };
    host = await render();
    expect(host.textContent).toContain('O rastreador não conectou em 20 minutos.');
    expect(button('Mandar de novo')).toBeTruthy();
  });

  it('formatos', () => {
    expect(phoneLabel('+5511999998888')).toBe('(11) 99999-8888');
    expect(phoneLabel('+551133334444')).toBe('(11) 3333-4444');
    expect(phoneLabel('+14158675310')).toBe('+14158675310');
    expect(stepStatus({ ...PLAN.steps[0], status: 'undelivered', error: '', sentAt: null }).label).toBe('Não chegou');
    expect(stepStatus({ ...PLAN.steps[0], status: 'queued', error: '', sentAt: null }).label).toBe('Enviando');
  });
});
