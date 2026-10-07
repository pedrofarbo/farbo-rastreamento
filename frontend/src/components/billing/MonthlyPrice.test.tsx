// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { MonthlyPrice } from './MonthlyPrice';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');

const sub = {
  priceCents: 6990, promoPriceCents: null, promoUntil: null, nextDueDate: '2027-07-10',
  nextPriceCents: 7333, nextPriceFrom: '2027-08-01',
};

async function render(staff = false) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () => createRoot(host).render(<MonthlyPrice sub={sub} staff={staff} />));
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
