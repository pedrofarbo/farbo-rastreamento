// @vitest-environment jsdom
import { act, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { SearchSelectField } from './SearchSelectField';
import type { SearchOption } from './SearchSelectField';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const OPTIONS: SearchOption[] = [
  { value: 'd1', label: '869247061230011 · TK910', search: ['89553202100093795330'], codes: ['869247061230011'] },
  { value: 'd2', label: '861234567890127 · J16', search: ['(34) 99999-0001'], codes: ['861234567890127'] },
  { value: 'd3', label: '869247061230033 · J16', codes: ['869247061230033'], disabled: true },
];

function Harness({ initial = '', autoPick, onPick }: { initial?: string; autoPick?: boolean; onPick: (v: string) => void }) {
  const [value, setValue] = useState(initial);
  return (
    <SearchSelectField
      label="Rastreador"
      value={value}
      onChange={(v) => {
        setValue(v);
        onPick(v);
      }}
      options={OPTIONS}
      emptyOption="aguardando instalação"
      searchLabel="Buscar rastreador"
      searchPlaceholder="Buscar por IMEI"
      autoPick={autoPick}
    />
  );
}

function render(node: React.ReactNode) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  act(() => createRoot(host).render(node));
  return host;
}

const search = () => document.querySelector('input[type="search"]') as HTMLInputElement;
const select = () => document.querySelector('select') as HTMLSelectElement;
const options = () => Array.from(select().options).map((o) => o.textContent);
const type = (value: string) =>
  act(() => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(search(), value);
    search().dispatchEvent(new Event('input', { bubbles: true }));
  });

afterEach(() => {
  document.body.innerHTML = '';
});

describe('SearchSelectField', () => {
  it('a busca filtra as opções, pelo texto e pelo que não aparece (o chip, a linha)', () => {
    render(<Harness onPick={() => {}} />);
    expect(select().labels?.[0]?.textContent).toBe('Rastreador');
    expect(search().getAttribute('aria-label')).toBe('Buscar rastreador');
    expect(options()).toHaveLength(4);

    type('j16');
    expect(options()).toEqual(['aguardando instalação', '861234567890127 · J16', '869247061230033 · J16']);
    expect(document.body.textContent).toContain('2 encontrados');

    type('8955 3202');
    expect(options()).toEqual(['aguardando instalação', '869247061230011 · TK910']);
    type('(34) 99999');
    expect(options()).toEqual(['aguardando instalação', '861234567890127 · J16']);
    type('yamaha');
    expect(options()).toEqual(['aguardando instalação']);
    expect(document.body.textContent).toContain('Nenhum encontrado com “yamaha”');
  });

  it('a escolhida fica na lista, mesmo fora da busca', () => {
    render(<Harness initial="d1" onPick={() => {}} />);
    type('j16');
    expect(options()).toContain('869247061230011 · TK910');
    expect(select().value).toBe('d1');
  });

  it('o IMEI inteiro já escolhe; o desabilitado não; com autoPick desligado, só filtra', () => {
    const picked = vi.fn();
    render(<Harness onPick={picked} />);
    type('8612 3456 7890 127');
    expect(picked).toHaveBeenCalledWith('d2');
    expect(select().value).toBe('d2');
    type('869247061230033');
    expect(picked).toHaveBeenCalledTimes(1);

    document.body.innerHTML = '';
    const off = vi.fn();
    render(<Harness autoPick={false} onPick={off} />);
    type('861234567890127');
    expect(off).not.toHaveBeenCalled();
    expect(options()).toEqual(['aguardando instalação', '861234567890127 · J16']);
  });
});
