// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { Device, Installer, InstallerLink } from '@/types';

const URL_ABERTO = 'https://farbo.test/instalador/avulsa/abc';
const device = (over: Partial<Device>): Device =>
  ({ id: 'd1', imei: '869247061230055', model: 'TK910', manufacturer: 'Coban', notes: '', ...over }) as Device;
const link = (over: Partial<InstallerLink> = {}): InstallerLink => ({
  id: 'l1', deviceId: 'd1', imei: '869247061230055', deviceModel: 'Coban TK910', vehicleName: '', plate: '', installerId: 'i1',
  installerName: 'Zé Instalador', notes: 'Moto do João', status: 'open', checkedAt: null, checkReady: false, completedAt: null,
  installerNote: '', closedAt: null, createdAt: '2026-10-28T12:00:00Z', url: URL_ABERTO, ...over,
});
const installers = [
  { id: 'i1', name: 'Zé Instalador', city: 'Uberlândia', whatsapp: '5534999990000' },
  { id: 'i2', name: 'Auto Elétrica Sul', city: '', whatsapp: '5534988880000' },
] as Installer[];

const data = vi.hoisted(() => ({ links: [] as object[], devices: [] as unknown[], calls: [] as unknown[][] }));
vi.mock('@/api/resources', () => ({
  devicesApi: { list: async () => data.devices },
  installerLinksApi: {
    list: async () => data.links,
    create: async (input: Record<string, unknown>) => {
      data.calls.push(['create', input]);
      return { ...data.links[0], ...input, id: 'novo', imei: '861234567890127', status: 'open' };
    },
    close: async (id: string) => {
      data.calls.push(['close', id]);
      return { ...data.links[0], status: 'closed' };
    },
  },
}));
vi.mock('@/components/ui/Toast', () => ({ useToast: () => ({ notify: () => {} }) }));
// A câmera fica no teste do LabelScanner: aqui, uma leitura pronta.
vi.mock('@/components/scan/LabelScanner', () => ({
  LabelScanner: ({ open, onRead }: { open: boolean; onRead: (code: string, source: string) => void }) =>
    open ? (
      <button type="button" onClick={() => onRead('861234567890127', 'barcode')}>
        leitura simulada
      </button>
    ) : null,
}));

import { InstallerLinkModal, InstallerLinksCard } from './InstallerLinks';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');
const button = (label: string) => Array.from(document.querySelectorAll('button')).find((b) => b.textContent?.trim() === label)!;
const control = <T extends HTMLElement>(label: string) => {
  const l = Array.from(document.querySelectorAll('label')).find((x) => x.textContent?.startsWith(label))!;
  return document.getElementById(l.htmlFor) as T;
};
const setValue = async (el: HTMLInputElement | HTMLSelectElement, value: string) =>
  act(async () => {
    const proto = el instanceof HTMLSelectElement ? HTMLSelectElement.prototype : HTMLInputElement.prototype;
    Object.getOwnPropertyDescriptor(proto, 'value')!.set!.call(el, value);
    el.dispatchEvent(new Event(el instanceof HTMLSelectElement ? 'change' : 'input', { bubbles: true }));
  });
const options = () =>
  Array.from(control<HTMLSelectElement>('Rastreador').options).map((o) => `${o.textContent}${o.disabled ? ' [x]' : ''}`);

async function render(node: ReactNode) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{node}</QueryClientProvider>,
    ),
  );
  await vi.waitFor(() => expect(text()).not.toContain('Carregando'));
}

afterEach(() => {
  document.body.innerHTML = '';
  data.calls = [];
});

describe('a instalação avulsa na tela de Prestadores', () => {
  it('lista os links: o teste do sinal, a conclusão e o encerramento', async () => {
    data.links = [
      link({ checkedAt: new Date().toISOString(), checkReady: false }),
      link({ id: 'l2', imei: '861234567890127', status: 'done', completedAt: '2026-10-28T15:00:00Z', installerNote: 'Debaixo do banco', url: undefined }),
      link({ id: 'l3', imei: '869247061230111', status: 'closed', closedAt: '2026-10-27T10:00:00Z', installerId: null, url: undefined }),
    ];
    await render(<InstallerLinksCard installers={installers} />);
    expect(text()).toContain('1 link(s) aberto(s)');
    expect(text()).toContain('Sem sinal no teste');
    expect(text()).toContain('testado agora');
    expect(text()).toContain('Concluída');
    expect(text()).toContain('“Debaixo do banco”');
    expect(text()).toContain('Encerrado');
    // Só o aberto tem as ações; o WhatsApp vai ao número do prestador, com o link.
    expect(Array.from(document.querySelectorAll('button')).filter((b) => b.textContent === 'Encerrar')).toHaveLength(1);
    const wa = document.querySelector('a[href^="https://wa.me/"]')!.getAttribute('href')!;
    expect(wa.startsWith('https://wa.me/5534999990000?text=')).toBe(true);
    expect(decodeURIComponent(wa)).toContain(`IMEI 869247061230055 (Coban TK910)`);
    expect(decodeURIComponent(wa)).toContain(URL_ABERTO);
    await act(async () => button('Encerrar').click());
    expect(data.calls).toContainEqual(['close', 'l1']);
  });

  it('gera o link: busca o rastreador (ou lê a etiqueta), o prestador já escolhido e o link para mandar', async () => {
    data.links = [link()];
    data.devices = [
      device({}),
      device({ id: 'd2', imei: '861234567890127', model: 'J16', manufacturer: '' }),
      device({ id: 'd3', imei: '869247061230111', model: 'J16', manufacturer: '' }),
    ];
    await render(<InstallerLinkModal installers={installers} installerId="i2" onClose={() => {}} />);
    expect(control<HTMLSelectElement>('Instalador').value).toBe('i2');
    // O que já tem link aberto fica de fora.
    expect(options()).toContain('869247061230055 · Coban TK910 (link aberto) [x]');
    expect(button('Gerar o link').disabled).toBe(true);

    await setValue(control<HTMLInputElement>('Buscar o rastreador'), 'j16');
    expect(options()).toEqual(['Escolha o rastreador', '861234567890127 · J16', '869247061230111 · J16']);
    await setValue(control<HTMLInputElement>('Buscar o rastreador'), '0111');
    expect(options()).toEqual(['Escolha o rastreador', '869247061230111 · J16']);

    // A etiqueta lida pela câmera já escolhe o rastreador.
    await act(async () => (document.querySelector('button[aria-label="Ler o IMEI pela câmera"]') as HTMLButtonElement).click());
    await act(async () => button('leitura simulada').click());
    expect(control<HTMLInputElement>('Buscar o rastreador').value).toBe('861234567890127');
    expect(control<HTMLSelectElement>('Rastreador').value).toBe('d2');

    await act(async () => button('Gerar o link').click());
    await vi.waitFor(() => expect(text()).toContain('Mande o link ao instalador'));
    expect(data.calls).toContainEqual([
      'create',
      { deviceId: 'd2', installerId: 'i2', installerName: 'Auto Elétrica Sul', notes: '' },
    ]);
    expect(text()).toContain(URL_ABERTO);
    expect(document.querySelector('a[href^="https://wa.me/"]')!.getAttribute('href')).toMatch(/^https:\/\/wa\.me\/5534988880000\?text=/);
  });

  it('instalador de fora da lista: precisa do nome', async () => {
    data.links = [];
    data.devices = [device({})];
    await render(<InstallerLinkModal installers={installers} onClose={() => {}} />);
    await setValue(control<HTMLSelectElement>('Rastreador'), 'd1');
    expect(button('Gerar o link').disabled).toBe(true);
    await setValue(control<HTMLInputElement>('Nome do instalador'), 'Carlos');
    expect(button('Gerar o link').disabled).toBe(false);
  });
});
