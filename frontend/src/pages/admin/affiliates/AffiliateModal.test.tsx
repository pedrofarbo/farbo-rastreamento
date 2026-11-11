// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

const calls = vi.hoisted(() => [] as unknown[][]);
vi.mock('@/api/resources', () => ({
  affiliatesApi: {
    create: async (input: Record<string, unknown>) => {
      calls.push(['create', input]);
      return { id: 'a1', code: 'lia', reportToken: 'x', ...input };
    },
    update: async () => ({}),
    renewReportLink: async () => ({}),
  },
  leadsApi: { downloadQr: async () => {} },
}));
vi.mock('@/components/ui/Toast', () => ({ useToast: () => ({ notify: () => {} }) }));

import { AffiliateModal } from './AffiliateModal';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const text = () => document.body.textContent ?? '';
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

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient()}>
        <AffiliateModal affiliate={null} open defaultCents={600} onClose={() => {}} onSaved={() => {}} />
      </QueryClientProvider>,
    ),
  );
}

afterEach(() => {
  document.body.innerHTML = '';
  calls.length = 0;
});

describe('o cadastro do afiliado', () => {
  it('comissão em percentual da mensalidade', async () => {
    await render();
    await setValue(control<HTMLInputElement>('Nome'), 'Lia Percentual');
    await setValue(control<HTMLSelectElement>('Comissão'), 'percent');
    expect(text()).toContain('a parcela do rastreador fica fora');
    await setValue(control<HTMLInputElement>('Percentual da mensalidade'), '150');
    await act(async () => button('Criar afiliado').click());
    expect(text()).toContain('Percentual inválido');
    expect(calls).toHaveLength(0);

    await setValue(control<HTMLInputElement>('Percentual da mensalidade'), '12,5');
    await act(async () => button('Criar afiliado').click());
    await vi.waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0][1]).toMatchObject({ name: 'Lia Percentual', commissionType: 'percent', commissionBps: 1250, commissionCents: null });
  });

  it('a mensalidade pelo link, por tipo de veículo', async () => {
    await render();
    await setValue(control<HTMLInputElement>('Nome'), 'Lia Tabela');
    await setValue(control<HTMLSelectElement>('Mensalidade pelo link'), 'kind');
    await setValue(control<HTMLInputElement>('Moto (R$/mês)'), '39,90');
    await setValue(control<HTMLInputElement>('Carro (R$/mês)'), '49,90');
    await act(async () => button('Criar afiliado').click());
    expect(text()).toContain('Preencha a mensalidade da moto, do carro e do caminhão');
    expect(calls).toHaveLength(0);

    await setValue(control<HTMLInputElement>('Caminhão (R$/mês)'), '59,90');
    await act(async () => button('Criar afiliado').click());
    await vi.waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0][1]).toMatchObject({ planPrices: { motorcycleCents: 3990, carCents: 4990, truckCents: 5990 } });
  });

  it('a instalação e a desinstalação pelo representante, que recebe direto', async () => {
    await render();
    await setValue(control<HTMLInputElement>('Nome'), 'Rui Representante');
    await setValue(control<HTMLSelectElement>('Instalação e desinstalação pelo link'), 'direct');
    expect(text()).toContain('não entram no Pix nem nas faturas');
    await setValue(control<HTMLInputElement>('Instalação (R$)'), '120,00');
    await act(async () => button('Criar afiliado').click());
    expect(text()).toContain('Preencha o preço da instalação e o da desinstalação');
    expect(calls).toHaveLength(0);

    await setValue(control<HTMLInputElement>('Desinstalação do comodato (R$)'), '0');
    await setValue(control<HTMLInputElement>('Garantia de troca no comodato'), 'cinco');
    await act(async () => button('Criar afiliado').click());
    expect(text()).toContain('Garantia de troca inválida');
    expect(calls).toHaveLength(0);

    await setValue(control<HTMLInputElement>('Garantia de troca no comodato'), '5,00');
    await act(async () => button('Criar afiliado').click());
    await vi.waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0][1]).toMatchObject({
      installPrices: { mode: 'direct', installCents: 12000, uninstallCents: 0, guaranteeCents: 500 },
    });
  });

  it('a senha da página do afiliado', async () => {
    await render();
    await setValue(control<HTMLInputElement>('Nome'), 'Rui Representante');
    await act(async () => (Array.from(document.querySelectorAll('label')).find((l) => l.textContent?.startsWith('Pedir senha'))!.querySelector('input') as HTMLInputElement).click());
    await act(async () => button('Criar afiliado').click());
    expect(text()).toContain('Defina a senha da página');
    await setValue(control<HTMLInputElement>('Senha da página'), 'curta');
    await act(async () => button('Criar afiliado').click());
    expect(text()).toContain('pelo menos 8 caracteres');
    expect(calls).toHaveLength(0);

    await setValue(control<HTMLInputElement>('Senha da página'), 'senha-do-rui-1');
    await act(async () => button('Criar afiliado').click());
    await vi.waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0][1]).toMatchObject({ reportPassword: 'senha-do-rui-1' });
  });

  it('o representante cuida das instalações (com a senha) e o rastreador pelo link', async () => {
    await render();
    await setValue(control<HTMLInputElement>('Nome'), 'Rui Representante');
    await setValue(control<HTMLSelectElement>('Rastreador pelo link'), 'loan');
    const manage = Array.from(document.querySelectorAll('label')).find((l) => l.textContent?.startsWith('O representante cuida'))!;
    await act(async () => (manage.querySelector('input') as HTMLInputElement).click());
    // Marcar a gestão já marca a senha da página.
    const passwordBox = Array.from(document.querySelectorAll('label')).find((l) => l.textContent?.startsWith('Pedir senha'))!;
    expect((passwordBox.querySelector('input') as HTMLInputElement).checked).toBe(true);
    await act(async () => (passwordBox.querySelector('input') as HTMLInputElement).click());
    await act(async () => button('Criar afiliado').click());
    expect(text()).toContain('Para o representante cuidar das instalações, marque a senha da página');
    expect(calls).toHaveLength(0);

    await act(async () => (passwordBox.querySelector('input') as HTMLInputElement).click());
    await setValue(control<HTMLInputElement>('Senha da página'), 'senha-do-rui-1');
    await act(async () => button('Criar afiliado').click());
    await vi.waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0][1]).toMatchObject({ equipmentModes: 'loan', manageInstalls: true, reportPassword: 'senha-do-rui-1', handDelivery: false });
  });

  it('a entrega em mãos pelo representante', async () => {
    await render();
    await setValue(control<HTMLInputElement>('Nome'), 'Rui Representante');
    const hand = Array.from(document.querySelectorAll('label')).find((l) => l.textContent?.startsWith('Entrega em mãos'))!;
    await act(async () => (hand.querySelector('input') as HTMLInputElement).click());
    await act(async () => button('Criar afiliado').click());
    await vi.waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0][1]).toMatchObject({ handDelivery: true });
  });

  it('o representante oficial na landing: pede a cidade e sugere o WhatsApp do cadastro', async () => {
    await render();
    await setValue(control<HTMLInputElement>('Nome'), 'Rui Representante');
    await setValue(control<HTMLInputElement>('WhatsApp'), '19999990000');
    const listed = Array.from(document.querySelectorAll('label')).find((l) => l.textContent?.startsWith('Mostrar na landing'))!;
    await act(async () => (listed.querySelector('input') as HTMLInputElement).click());
    expect(control<HTMLInputElement>('WhatsApp público').value).toBe('(19) 9-9999-0000');
    await act(async () => button('Criar afiliado').click());
    expect(text()).toContain('Para aparecer na landing, diga a cidade');
    expect(calls).toHaveLength(0);

    await setValue(control<HTMLInputElement>('Cidade (na landing)'), 'Campinas/SP');
    await setValue(control<HTMLInputElement>('Região que atende'), 'Campinas e Valinhos');
    await act(async () => button('Criar afiliado').click());
    await vi.waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0][1]).toMatchObject({
      listed: true, listingCity: 'Campinas/SP', listingArea: 'Campinas e Valinhos', listingWhatsapp: '(19) 9-9999-0000',
    });
  });

  it('o semestral e o anual pelo link: desligados por padrão; ligados, com os valores dele; não no só comodato', async () => {
    await render();
    await setValue(control<HTMLInputElement>('Nome'), 'Rafa Representante');
    const toggle = Array.from(document.querySelectorAll('label')).find((l) => l.textContent?.startsWith('Oferecer o semestral e o anual pelo link dele'))!
      .querySelector('input') as HTMLInputElement;
    expect(toggle.checked).toBe(false);
    expect(control<HTMLInputElement>('Semestral (R$, a cada 6 meses)').value).toBe('200,00');
    expect(control<HTMLInputElement>('Anual (R$, a cada 12 meses)').value).toBe('400,00');
    await act(async () => toggle.click());
    await setValue(control<HTMLInputElement>('Anual (R$, a cada 12 meses)'), '');
    await act(async () => button('Criar afiliado').click());
    expect(text()).toContain('Para ligar o semestral e o anual, informe os dois valores.');
    await setValue(control<HTMLInputElement>('Anual (R$, a cada 12 meses)'), '450,00');
    await setValue(control<HTMLSelectElement>('Rastreador pelo link'), 'loan');
    await act(async () => button('Criar afiliado').click());
    expect(text()).toContain('o link só de comodato não os oferece');
    expect(calls).toHaveLength(0);
    await setValue(control<HTMLSelectElement>('Rastreador pelo link'), '');
    await act(async () => button('Criar afiliado').click());
    await vi.waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0][1]).toMatchObject({ recurrence: { enabled: true, semiannualCents: 20000, annualCents: 45000 } });
  });

  it('valor fixo continua como antes', async () => {
    await render();
    await setValue(control<HTMLInputElement>('Nome'), 'João');
    await setValue(control<HTMLInputElement>('Valor por veículo'), '8,00');
    await act(async () => button('Criar afiliado').click());
    await vi.waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0][1]).toMatchObject({
      commissionType: 'fixed', commissionCents: 800, commissionBps: null, planPrices: null, installPrices: null,
    });
    // Sem mexer na senha, ela não vai.
    expect(calls[0][1]).not.toHaveProperty('reportPassword');
  });
});
