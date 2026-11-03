// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { InstallPrices } from '@/types';

const calls = vi.hoisted(() => [] as unknown[][]);
vi.mock('@/api/resources', () => ({
  publicApi: {
    partnerInstallPrices: async (token: string, input: { installCents: number; uninstallCents: number }) => {
      calls.push([token, input]);
      return { mode: 'direct', ...input, guaranteeCents: null };
    },
  },
}));

import { InstallPricesSection } from './InstallPricesSection';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');
const input = (label: string) =>
  Array.from(document.querySelectorAll('label')).find((l) => l.textContent?.startsWith(label))!.querySelector('input') as HTMLInputElement;
const type = (el: HTMLInputElement, value: string) =>
  act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(el, value);
    el.dispatchEvent(new Event('input', { bubbles: true }));
  });

afterEach(() => {
  document.body.innerHTML = '';
  calls.length = 0;
});

describe('os valores de instalação do representante', () => {
  it('ele define os valores, que não passam pela Farbo', async () => {
    const changed: InstallPrices[] = [];
    const host = document.createElement('div');
    document.body.appendChild(host);
    await act(async () =>
      createRoot(host).render(
        <InstallPricesSection
          token="tok"
          prices={{ mode: 'direct', installCents: 12000, uninstallCents: 8000 }}
          onChange={(p) => changed.push(p)}
        />,
      ),
    );
    expect(text()).toContain('esses valores não passam pela Farbo');
    await type(input('Instalação (R$)'), 'cento e vinte');
    await act(async () => (host.querySelector('button[type="submit"]') as HTMLButtonElement).click());
    expect(text()).toContain('Confira os valores');
    expect(calls).toHaveLength(0);

    await type(input('Instalação (R$)'), '180,00');
    await type(input('Desinstalação do comodato (R$)'), '0');
    await act(async () => (host.querySelector('button[type="submit"]') as HTMLButtonElement).click());
    expect(calls).toEqual([['tok', { installCents: 18000, uninstallCents: 0 }]]);
    expect(changed[0]).toMatchObject({ installCents: 18000, uninstallCents: 0 });
    expect(text()).toContain('Valores salvos');
  });
});
