// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ToastProvider } from '@/components/ui/Toast';
import type { TheftMode, TheftView, VehicleView } from '@/types';

const calls: unknown[] = [];
let view: TheftView;
const activeMode: TheftMode = {
  id: 'm1', vehicleId: 'v1', activatedByName: 'Caio Lima', activatedAt: '2026-10-06T22:10:00Z',
  expiresAt: '2026-10-09T22:10:00Z', boostStatus: 'SENT', boostCommandStatus: 'ACKNOWLEDGED', boostSmsAt: null,
  endedAt: null, outcome: '', restoreStatus: '',
};
const off = (over: Partial<TheftView> = {}): TheftView => ({
  mode: null, publicUrl: '', canActivate: true, canEnd: true, parkedSeconds: 30, durationHours: 72, ...over,
});
const on = (over: Partial<TheftView> = {}): TheftView => ({
  ...off(), mode: activeMode, publicUrl: 'https://farborastreadores.com.br/localizar/abc123', ...over,
});

vi.mock('@/api/resources', () => ({
  theftApi: {
    get: async () => view,
    activate: async (id: string) => {
      calls.push(['activate', id]);
      view = on();
      return view;
    },
    end: async (id: string, outcome: string, token: string) => {
      calls.push(['end', id, outcome, token]);
      view = off();
      return view;
    },
  },
}));
// A confirmação (biometria ou senha) tem os próprios testes.
vi.mock('@/components/auth/IdentityCheck', () => ({
  IdentityCheck: ({ action, onGrant }: { action: string; onGrant: (t: string) => void }) => (
    <button type="button" onClick={() => onGrant('comprovante')}>
      Confirmar: {action}
    </button>
  ),
}));
vi.mock('@/components/ui/Address', () => ({ Address: () => <span>Rua Augusta, 500 - São Paulo</span> }));

import { TheftPanel, shareText, trackerLine } from './TheftPanel';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');
const button = (label: string | RegExp) =>
  Array.from(document.querySelectorAll('button')).find((b) =>
    typeof label === 'string' ? b.textContent === label : label.test(b.textContent ?? ''),
  ) as HTMLButtonElement;

const vehicle = {
  id: 'v1', name: 'Moto do trabalho', plate: 'ROU1B23', brand: 'Honda', model: 'CG 160', color: 'Vermelha',
  device: { id: 'd1' }, lastPosition: { latitude: -23.55, longitude: -46.65 },
} as unknown as VehicleView;

async function render(props: { prompt?: boolean; reportUrl?: string } = {}) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <MemoryRouter>
          <ToastProvider>
            <TheftPanel vehicle={vehicle} {...props} />
          </ToastProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  );
  await flush();
}

afterEach(() => {
  document.body.innerHTML = '';
  calls.length = 0;
});

describe('modo roubo', () => {
  it('as frases do rastreador e do link', () => {
    expect(trackerLine(activeMode, 30)).toBe('O rastreador confirmou: posição a cada 30 s, mesmo parado.');
    expect(trackerLine({ ...activeMode, boostCommandStatus: 'SENT' }, 30)).toMatch(/^Comando enviado ao rastreador/);
    expect(trackerLine({ ...activeMode, boostStatus: 'PENDING', boostSmsAt: '2026-10-06T22:10:00Z' }, 30)).toBe(
      'O rastreador está fora do ar: o intervalo curto (posição a cada 30 s, mesmo parado) vai assim que ele se conectar; também mandamos por SMS.',
    );
    expect(trackerLine({ ...activeMode, boostStatus: 'SKIPPED' }, 30)).toMatch(/não muda o intervalo/);
    expect(shareText(vehicle, 'https://x/localizar/abc')).toBe(
      'Veículo roubado: Moto do trabalho (Honda CG 160 Vermelha), placa ROU1B23. Localização ao vivo: https://x/localizar/abc',
    );
  });

  it('liga com uma confirmação e vira o passo a passo', async () => {
    view = off();
    await render({ reportUrl: '/relatorio-roubo/v1' });
    expect(text()).toContain('a cada 30 s, mesmo parado');
    await act(async () => button('Meu veículo foi roubado').click());
    expect(text()).toContain('Ativar o modo roubo de Moto do trabalho?');
    expect(text()).toContain('Desliga sozinho em 72 h');
    await act(async () => button('Ativar o modo roubo').click());
    await flush();
    expect(calls).toEqual([['activate', 'v1']]);
    expect(text()).toContain('Modo roubo ativo');
    expect(text()).toContain('por Caio Lima');
    expect(text()).toContain('O rastreador confirmou');
    expect(text()).toContain('Rua Augusta, 500');
    expect(document.querySelector('a[href="tel:190"]')).toBeTruthy();
    const whatsapp = document.querySelector('a[href^="https://wa.me/"]') as HTMLAnchorElement;
    expect(decodeURIComponent(whatsapp.href)).toContain('Localização ao vivo: https://farborastreadores.com.br/localizar/abc123');
    expect((document.querySelector('a[href="/relatorio-roubo/v1"]') as HTMLAnchorElement).textContent).toBe('Gerar o relatório');
    expect(text()).toContain('Não tente recuperar o veículo sozinho');
    expect(text()).toContain('Use o Desligar motor em Comandos');
  });

  it('o dono desliga com a biometria ou a senha', async () => {
    view = on();
    await render();
    await act(async () => button('Recuperei o veículo').click());
    await act(async () => button(/Confirmar: Para marcar o veículo como recuperado/).click());
    await flush();
    expect(calls).toEqual([['end', 'v1', 'RECOVERED', 'comprovante']]);
    expect(text()).toContain('Meu veículo foi roubado');
  });

  it('quem pode bloquear vê o passo a passo, mas não desliga', async () => {
    view = on({ canEnd: false });
    await render();
    expect(text()).toContain('Modo roubo ativo');
    expect(button('Recuperei o veículo')).toBeUndefined();
    expect(text()).toContain('só o dono desliga antes');
    expect(text()).toContain('Use o Bloquear motor em Comandos');
  });

  it('quem só acompanha não liga', async () => {
    view = off({ canActivate: false });
    await render();
    expect(text()).not.toContain('Modo roubo');
  });

  it('pelo alerta, já pergunta', async () => {
    view = off();
    Element.prototype.scrollIntoView = () => undefined;
    await render({ prompt: true });
    expect(text()).toContain('Ativar o modo roubo de Moto do trabalho?');
  });
});
