// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { PartnerRecipient } from '@/types';

const calls = vi.hoisted(() => [] as unknown[][]);
vi.mock('@/api/resources', () => ({
  publicApi: {
    partnerRecipient: async (token: string, input: unknown) => {
      calls.push(['recipient', token, input]);
      return { enabled: true, status: 'registration' };
    },
    partnerKycLink: async (token: string) => {
      calls.push(['kyc', token]);
      return { url: 'https://kyc.pagar.me/abc', base64: 'iVBORw0KGgo', expirationDate: null };
    },
  },
}));
vi.mock('@/services/zipcode', () => ({
  lookupZipCode: async () => ({
    status: 'found',
    address: { street: 'Av. Rio Branco', district: 'Centro', city: 'Rio de Janeiro', state: 'RJ' },
  }),
}));

import { RecipientSection } from './RecipientSection';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const text = () => document.body.textContent ?? '';
const button = (label: string) => Array.from(document.querySelectorAll('button')).find((b) => b.textContent?.trim() === label)!;
const field = (label: string) => {
  const l = Array.from(document.querySelectorAll('label')).find((x) => x.firstChild?.textContent === label)!;
  return l.querySelector('input') as HTMLInputElement;
};
const type = async (input: HTMLInputElement, value: string, blur = false) =>
  act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value);
    input.dispatchEvent(new Event('input', { bubbles: true }));
    if (blur) input.dispatchEvent(new FocusEvent('focusout', { bubbles: true }));
  });

let changed: PartnerRecipient[] = [];
async function render(recipient: PartnerRecipient) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(<RecipientSection token="tok" recipient={recipient} onChange={(r) => changed.push(r)} />),
  );
}

afterEach(() => {
  document.body.innerHTML = '';
  calls.length = 0;
  changed = [];
});

describe('o recebedor do split na página do afiliado', () => {
  it('o cadastro de pessoa física vai para a Pagar.me (o CEP adianta o endereço)', async () => {
    await render({ enabled: true, status: '' });
    expect(text()).toContain('a Farbo não guarda nenhum deles');
    await type(field('CPF'), '529.982.247-25');
    await type(field('Nome completo'), 'Lia da Silva');
    await type(field('Nascimento'), '12101995');
    expect(field('Nascimento').value).toBe('12/10/1995');
    await type(field('Renda mensal (R$)'), '5.000,00');
    await type(field('Profissão'), 'Influenciadora');
    await type(field('E-mail'), 'lia@afiliada.test');
    await type(field('Celular com DDD'), '(21) 99464-7568');
    await type(field('CEP'), '20040-020', true);
    await act(async () => new Promise((r) => setTimeout(r, 0)));
    expect(field('Rua').value).toBe('Av. Rio Branco');
    await type(field('Número'), '100');
    await type(field('Código do banco'), '260');
    await type(field('Agência (sem dígito)'), '0001');
    await type(field('Conta (sem dígito)'), '1234567');
    await type(field('Dígito da conta'), '8');
    await act(async () => button('Enviar o cadastro para a Pagar.me').click());
    expect(calls[0]).toEqual([
      'recipient',
      'tok',
      expect.objectContaining({
        kind: 'individual', document: '529.982.247-25', name: 'Lia da Silva', birthdate: '12/10/1995',
        monthlyIncomeCents: 500000, annualRevenueCents: 0, city: 'Rio de Janeiro', state: 'RJ', bank: '260',
        accountType: 'checking',
      }),
    ]);
    expect(changed).toEqual([{ enabled: true, status: 'registration' }]);
  });

  it('empresa: pede razão social, nome fantasia e faturamento', async () => {
    await render({ enabled: true, status: '' });
    await act(async () => button('Empresa (CNPJ)').click());
    expect(field('CNPJ')).toBeTruthy();
    expect(field('Razão social')).toBeTruthy();
    await act(async () => button('Enviar o cadastro para a Pagar.me').click());
    expect(text()).toContain('Informe o faturamento anual');
    expect(calls).toHaveLength(0);
  });

  it('cadastro enviado: a prova de vida pelo QR Code e o link', async () => {
    await render({ enabled: true, status: 'registration' });
    await act(async () => button('Fazer a prova de vida').click());
    expect(calls).toEqual([['kyc', 'tok']]);
    expect(document.querySelector('img')?.getAttribute('src')).toBe('data:image/png;base64,iVBORw0KGgo');
    expect(document.querySelector('a')?.getAttribute('href')).toBe('https://kyc.pagar.me/abc');
    expect(text()).toContain('O link vale 20 minutos');
  });

  it('ativo e recusado', async () => {
    await render({ enabled: true, status: 'active' });
    expect(text()).toContain('Cadastro ativo');
    document.body.innerHTML = '';
    await render({ enabled: true, status: 'refused' });
    expect(text()).toContain('A Pagar.me recusou o cadastro');
  });
});
