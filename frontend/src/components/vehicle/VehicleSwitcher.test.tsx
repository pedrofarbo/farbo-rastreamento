// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it } from 'vitest';

import type { VehicleView } from '@/types';

import { VehicleSwitcher } from './VehicleSwitcher';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const v = (id: string, name: string, plate = '', status = 'ONLINE') =>
  ({ id, name, plate, brand: '', model: '', custom1: '', custom2: '', device: { imei: `86924706123${id}`, status } }) as unknown as VehicleView;
const FLEET = [v('1', 'Caminhão 01', 'ABC1D23'), v('2', 'Van 02', 'XYZ9K88'), v('3', 'Moto 03', '', 'OFFLINE')];

let picked: string[] = [];
async function render(vehicles = FLEET, currentId = '1', search = true) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <VehicleSwitcher vehicles={vehicles} currentId={currentId} onSelect={(id) => picked.push(id)} search={search} />,
    ),
  );
  return host;
}
const button = (label: string) => document.querySelector(`button[aria-label^="${label}"]`) as HTMLButtonElement;
function type(input: HTMLInputElement, value: string) {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set?.call(input, value);
  input.dispatchEvent(new Event('input', { bubbles: true }));
}

afterEach(() => {
  document.body.innerHTML = '';
  picked = [];
});

describe('o seletor de veículo', () => {
  it('o anterior e o próximo dão a volta na lista', async () => {
    await render(FLEET, '1');
    expect(button('Trocar de veículo').textContent).toContain('1 de 3');
    await act(async () => button('Próximo veículo').click());
    await act(async () => button('Veículo anterior').click());
    expect(picked).toEqual(['2', '3']);
  });

  it('a busca acha pelo nome e pela placa; Enter escolhe o primeiro; Esc fecha', async () => {
    await render(FLEET, '1');
    await act(async () => button('Trocar de veículo').click());
    const input = document.querySelector('input[type="search"]') as HTMLInputElement;
    expect(document.querySelectorAll('[role="option"]')).toHaveLength(3);
    expect(document.querySelector('[aria-selected="true"]')?.textContent).toContain('Caminhão 01');
    await act(async () => type(input, 'xyz9'));
    expect(Array.from(document.querySelectorAll('[role="option"]')).map((o) => o.textContent)).toEqual(['Van 02XYZ9K88']);
    await act(async () => input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })));
    expect(picked).toEqual(['2']);
    expect(document.querySelector('[role="dialog"]')).toBeNull();

    await act(async () => button('Trocar de veículo').click());
    await act(async () => type(document.querySelector('input[type="search"]') as HTMLInputElement, 'nada'));
    expect(document.body.textContent).toContain('Nenhum veículo com essa busca.');
    await act(async () => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' })));
    expect(document.querySelector('[role="dialog"]')).toBeNull();
  });

  it('sem a busca (o mapa, com a lista ao lado), só as setas; com um veículo só, some', async () => {
    await render(FLEET, '2', false);
    expect(button('Trocar de veículo')).toBeNull();
    await act(async () => button('Próximo veículo').click());
    expect(picked).toEqual(['3']);
    document.body.innerHTML = '';
    const host = await render([FLEET[0]], '1');
    expect(host.innerHTML).toBe('');
  });
});
