// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

// Função simples em vez de vi.fn: o espião do Vitest acompanha a promessa
// rejeitada e a acusa como não tratada, mesmo o componente tratando.
const sent: unknown[] = [];
let respond: () => Promise<unknown> = async () => ({ status: 'ok' });
vi.mock('@/api/resources', () => ({
  publicApi: {
    createLead: (input: unknown) => {
      sent.push(input);
      return respond();
    },
  },
}));

import { LeadModal } from './LeadModal';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

function render(plan?: string) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const root = createRoot(host);
  act(() => root.render(<LeadModal isOpen onClose={() => {}} defaultPlan={plan} />));
  return host;
}

/** Preenche como o usuário: o setter nativo, para o React ver a mudança. */
function type(el: HTMLInputElement | HTMLTextAreaElement, value: string) {
  const proto = el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
  Object.getOwnPropertyDescriptor(proto, 'value')!.set!.call(el, value);
  act(() => {
    el.dispatchEvent(new Event('input', { bubbles: true }));
  });
}

const byId = <T extends HTMLElement>(host: HTMLElement, id: string) => host.querySelector(`#${id}`) as T;
const submitButton = (host: HTMLElement) => host.querySelector('button[type="submit"]') as HTMLButtonElement;

beforeEach(() => {
  sent.length = 0;
  respond = async () => ({ status: 'ok' });
});
afterEach(() => {
  document.body.innerHTML = '';
});

describe('LeadModal (pré-cadastro)', () => {
  it('só envia com nome, e-mail e o aceite; manda os dados e mostra a confirmação', async () => {
    const host = render('Preço Especial Insanos MC - R$ 39,90');

    expect(byId<HTMLSelectElement>(host, 'lead-plan').value).toBe('Preço Especial Insanos MC - R$ 39,90');
    // No plano do Insanos, a promoção mostra a mensalidade deles.
    expect(host.textContent).toContain('R$ 27,90/mês');
    expect(submitButton(host).disabled).toBe(true);

    type(byId(host, 'lead-name'), 'Ana Souza');
    type(byId(host, 'lead-email'), 'ana@exemplo.com.br');
    expect(submitButton(host).disabled).toBe(true); // falta o WhatsApp
    type(byId(host, 'lead-phone'), '11988887777');
    expect(byId<HTMLInputElement>(host, 'lead-phone').value).toBe('(11) 98888-7777');
    type(byId(host, 'lead-city'), 'Campinas');
    expect(submitButton(host).disabled).toBe(true); // falta o aceite

    act(() => byId<HTMLInputElement>(host, 'lead-consent').click());
    expect(submitButton(host).disabled).toBe(false);

    await act(async () => {
      submitButton(host).click();
    });
    expect(sent).toHaveLength(1);
    expect(sent[0]).toEqual(
      expect.objectContaining({
        name: 'Ana Souza',
        email: 'ana@exemplo.com.br',
        phone: '(11) 98888-7777',
        city: 'Campinas',
        plan: 'Preço Especial Insanos MC - R$ 39,90',
        vehicleType: 'moto',
        vehicleCount: 1,
        consent: true,
        joinLaunch: true,
        website: '',
      }),
    );
    expect(host.textContent).toContain('também entrou na lista de pré-lançamento');
    expect(host.textContent).toContain('Recebemos seu interesse!');
    expect(host.textContent).toContain('ana@exemplo.com.br');
  });

  it('mostra o erro do servidor e continua no formulário', async () => {
    respond = () => Promise.reject(new Error('informe um e-mail válido'));
    const host = render();
    type(byId(host, 'lead-name'), 'Ana');
    type(byId(host, 'lead-email'), 'ana@x');
    type(byId(host, 'lead-phone'), '(11) 98888-7777');
    act(() => byId<HTMLInputElement>(host, 'lead-consent').click());
    await act(async () => {
      submitButton(host).click();
    });
    expect(host.querySelector('[role="alert"]')?.textContent).toBe('informe um e-mail válido');
    expect(byId(host, 'lead-name')).not.toBeNull();
  });

  it('a lista de pré-lançamento vem marcada; desmarcada, não entra', async () => {
    const host = render();
    const launch = byId<HTMLInputElement>(host, 'lead-launch');
    expect(launch.checked).toBe(true);
    expect(host.textContent).toContain('R$ 34,90/mês');

    type(byId(host, 'lead-name'), 'Carla');
    type(byId(host, 'lead-email'), 'carla@exemplo.com.br');
    type(byId(host, 'lead-phone'), '19977776666');
    act(() => launch.click());
    act(() => byId<HTMLInputElement>(host, 'lead-consent').click());
    await act(async () => {
      submitButton(host).click();
    });
    expect(sent[0]).toEqual(expect.objectContaining({ joinLaunch: false }));
    expect(host.textContent).not.toContain('também entrou na lista');
  });

    it('quantidade pelo − e +, e a mensagem só aparece quando pedida', async () => {
    const host = render();
    const minus = host.querySelector('button[aria-label="Um veículo a menos"]') as HTMLButtonElement;
    const plus = host.querySelector('button[aria-label="Um veículo a mais"]') as HTMLButtonElement;
    expect(minus.disabled).toBe(true); // não passa de 1 para baixo
    act(() => plus.click());
    act(() => plus.click());
    expect(byId<HTMLInputElement>(host, 'lead-count').value).toBe('3');
    act(() => minus.click());

    expect(byId(host, 'lead-message')).toBeNull();
    act(() => (Array.from(host.querySelectorAll('button')).find((b) => b.textContent?.includes('mensagem')) as HTMLButtonElement).click());
    type(byId(host, 'lead-message'), 'Tenho duas motos');

    type(byId(host, 'lead-name'), 'Davi');
    type(byId(host, 'lead-email'), 'davi@exemplo.com.br');
    type(byId(host, 'lead-phone'), '21966665555');
    act(() => byId<HTMLInputElement>(host, 'lead-consent').click());
    await act(async () => {
      submitButton(host).click();
    });
    expect(sent[0]).toEqual(expect.objectContaining({ vehicleCount: 2, message: 'Tenho duas motos' }));
  });

    it('a isca fica fora da tela e fora do Tab', () => {
    const host = render();
    const bait = byId<HTMLInputElement>(host, 'lead-website');
    expect(bait.tabIndex).toBe(-1);
    expect(bait.closest('[aria-hidden="true"]')).not.toBeNull();
  });
});
