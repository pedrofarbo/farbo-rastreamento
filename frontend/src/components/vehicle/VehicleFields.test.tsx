// @vitest-environment jsdom
import { act, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it } from 'vitest';

import type { VehicleInput } from '@/api/resources';

import { EMPTY_VEHICLE, VehicleFields } from './VehicleFields';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let current: VehicleInput = EMPTY_VEHICLE;
function Harness({ initial }: { initial: VehicleInput }) {
  const [value, setValue] = useState(initial);
  current = value;
  return <VehicleFields value={value} onChange={setValue} />;
}

async function render(initial: VehicleInput) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () => createRoot(host).render(<Harness initial={initial} />));
  return host;
}

afterEach(() => {
  document.body.innerHTML = '';
});

describe('o tipo do veículo no cadastro', () => {
  it('começa em carro e troca para moto', async () => {
    const host = await render(EMPTY_VEHICLE);
    const option = (label: string) =>
      Array.from(host.querySelectorAll('[role=radio]')).find((b) => b.textContent?.includes(label)) as HTMLButtonElement;
    expect(option('Carro').getAttribute('aria-checked')).toBe('true');
    expect(option('Moto').getAttribute('aria-checked')).toBe('false');
    await act(async () => option('Moto').click());
    expect(current.kind).toBe('MOTORCYCLE');
    expect(option('Moto').getAttribute('aria-checked')).toBe('true');
  });

  it('veículo sem tipo (dados antigos) aparece como carro', async () => {
    const host = await render({ name: 'Antigo' });
    const checked = host.querySelector('[role=radio][aria-checked=true]');
    expect(checked?.textContent).toContain('Carro');
  });
});
