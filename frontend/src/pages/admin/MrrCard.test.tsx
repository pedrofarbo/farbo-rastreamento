// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import type { UseQueryResult } from '@tanstack/react-query';
import { afterEach, describe, expect, it } from 'vitest';

import type { MRR } from '@/types';

import { ForecastTile, MrrCard, signedMoney } from './MrrCard';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const text = (el: Element) => (el.textContent ?? '').replace(/ /g, ' ');

afterEach(() => {
  document.body.innerHTML = '';
});

describe('MRR', () => {
  it('o sinal do valor', () => {
    expect(signedMoney(6500).replace(/ /g, ' ')).toBe('+R$ 65,00');
    expect(signedMoney(-3990).replace(/ /g, ' ')).toBe('−R$ 39,90');
    expect(signedMoney(0)).toBe('—');
  });

  it('mês a mês: as mensalidades, as novas, as encerradas, o resto, a variação, as parcelas e o total', async () => {
    const data: MRR = {
      currentCents: 17470,
      subscriptions: 4,
      installmentsCents: 2200,
      totalCents: 19670,
      forecast: null,
      months: [
        { month: '2026-10', mrrCents: 17470, subscriptions: 4, newCents: 6990, newCount: 2, churnCents: 3990, churnCount: 1, changeCents: 6500, otherCents: 3500,
          installmentsCents: 2200, installmentsPaidCents: 1200, totalCents: 19670 },
        { month: '2026-09', mrrCents: 10970, subscriptions: 3, newCents: 3990, newCount: 1, churnCents: 0, churnCount: 0, changeCents: 3990, otherCents: 0,
          installmentsCents: 0, installmentsPaidCents: 0, totalCents: 10970 },
      ],
    };
    const host = document.createElement('div');
    document.body.appendChild(host);
    await act(async () => createRoot(host).render(<MrrCard mrr={{ data, isLoading: false } as UseQueryResult<MRR>} />));
    const rows = Array.from(host.querySelectorAll('tbody tr')).map(text);
    expect(rows[0]).toContain('out/2026');
    expect(rows[0]).toContain('até hoje');
    expect(rows[0]).toContain('R$ 174,70');
    expect(rows[0]).toContain('4 assinaturas');
    expect(rows[0]).toContain('+R$ 69,90');
    expect(rows[0]).toContain('2 novas');
    expect(rows[0]).toContain('1 encerrada');
    expect(rows[0]).toContain('−R$ 39,90');
    expect(rows[0]).toContain('+R$ 35,00');
    expect(rows[0]).toContain('+R$ 65,00');
    // As parcelas do rastreador (quanto já foi pago) e o total.
    expect(rows[0]).toContain('R$ 22,00');
    expect(rows[0]).toContain('R$ 12,00 pagas');
    expect(rows[0]).toContain('R$ 196,70');
    expect(rows[1]).not.toContain('pagas');
    expect(rows[1]).toContain('set/2026');
    expect(rows[1]).not.toContain('até hoje');
  });

  it('a previsão do mês que vem: o total e a divisão', async () => {
    const host = document.createElement('div');
    document.body.appendChild(host);
    await act(async () =>
      createRoot(host).render(
        <ForecastTile
          forecast={{ month: '2026-11', monthlyCents: 13950, monthlies: 5, installmentsCents: 15600, totalCents: 29550 }}
        />,
      ),
    );
    expect(text(host)).toContain('Previsão nov/2026');
    expect(text(host)).toContain('R$ 295,50');
    expect(text(host)).toContain('R$ 139,50 em 5 mensalidades + R$ 156,00 parcelas');
  });
});
