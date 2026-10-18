// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { MonthlyPrice, isFree } from './MonthlyPrice';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');

const sub = {
  priceCents: 6990, promoPriceCents: null, promoUntil: null, nextDueDate: '2027-07-10',
  nextPriceCents: 7333, nextPriceFrom: '2027-08-01',
};

async function render(staff = false, value: Parameters<typeof MonthlyPrice>[0]['sub'] = sub) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () => createRoot(host).render(<MonthlyPrice sub={value} staff={staff} />));
  return host;
}

afterEach(() => {
  document.body.innerHTML = '';
  vi.useRealTimers();
});

describe('a mensalidade com o reajuste agendado', () => {
  it('o cliente só vê o valor novo a partir de um mês antes; a equipe, sempre', async () => {
    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime(new Date(2027, 5, 30, 12));
    await render();
    expect(text()).toBe('R$ 69,90/mês');
    document.body.innerHTML = '';
    await render(true);
    expect(text()).toContain('reajuste anual pelo IPCA: R$ 73,33 a partir de 01/08/2027');

    document.body.innerHTML = '';
    vi.setSystemTime(new Date(2027, 6, 1, 9));
    await render();
    expect(text()).toContain('R$ 73,33 a partir de 01/08/2027');
  });
});

describe('o valor da mensalidade', () => {
  it('sai num elemento só (no celular, a linha não separa o valor do /mês)', async () => {
    const host = await render(false, { ...sub, nextPriceCents: undefined, nextPriceFrom: undefined });
    expect(host.children).toHaveLength(1);
    expect(text()).toBe('R$ 69,90/mês');
    document.body.innerHTML = '';
    const promo = await render(false, { ...sub, promoPriceCents: 3490, promoUntil: '2027-08-10' });
    expect(promo.children).toHaveLength(1);
    expect(text()).toContain('R$ 34,90/mês (pré-lançamento');
  });

  it('a grátis é "Grátis", e não R$ 0,00/mês', async () => {
    const free = { priceCents: 0, promoPriceCents: null, promoUntil: null, nextDueDate: '2026-11-10' };
    expect(isFree(free)).toBe(true);
    expect(isFree(sub)).toBe(false);
    expect(isFree({ ...free, promoPriceCents: 3490, promoUntil: '2027-01-10' })).toBe(false);
    await render(false, free);
    expect(text()).toBe('Grátis');
  });
});
