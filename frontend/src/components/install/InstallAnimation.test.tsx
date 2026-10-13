// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { InstallAnimation, STEPS } from './InstallAnimation';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const text = () => document.body.textContent ?? '';
const button = (label: string) => document.querySelector(`button[aria-label="${label}"]`) as HTMLButtonElement;
const title = () => document.querySelector('h3')?.textContent;

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () => createRoot(host).render(<InstallAnimation />));
}

afterEach(() => {
  document.body.innerHTML = '';
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('a instalação animada', () => {
  it('toca sozinha, etapa por etapa, e volta ao começo', async () => {
    vi.useFakeTimers();
    await render();
    expect(text()).toContain('Etapa 1 de 8');
    expect(title()).toBe('Desligue o veículo');
    expect(document.querySelector('svg')?.getAttribute('aria-label')).toBe('Etapa 1: Desligue o veículo');
    await act(async () => vi.advanceTimersByTime(STEPS[0].ms));
    expect(title()).toBe('Escolha o local');
    for (let i = 1; i < STEPS.length; i++) {
      await act(async () => vi.advanceTimersByTime(STEPS[i].ms));
    }
    expect(title()).toBe('Desligue o veículo');
  });

  it('pausa (a etapa fica pronta, parada), avança, volta e vai direto a uma etapa', async () => {
    vi.useFakeTimers();
    await render();
    await act(async () => button('Pausar a animação').click());
    await act(async () => vi.advanceTimersByTime(60_000));
    expect(title()).toBe('Desligue o veículo');
    expect(document.querySelector('[aria-live]')?.getAttribute('aria-live')).toBe('polite');
    await act(async () => button('Etapa anterior').click());
    expect(title()).toBe('Teste pelo app');
    await act(async () => button('Próxima etapa').click());
    expect(title()).toBe('Desligue o veículo');
    await act(async () => button('Etapa 7: O relê: verdes no fio da bomba').click());
    expect(title()).toBe('O relê: verdes no fio da bomba');
    expect(text()).toContain('dois verdes');
    expect(document.querySelector('a')?.getAttribute('href')).toBe('#rele');
    expect(button('Etapa 7: O relê: verdes no fio da bomba').getAttribute('aria-current')).toBe('step');
  });

  it('a cena monta o que já foi feito e mostra o que é da etapa', async () => {
    await render();
    await act(async () => button('Pausar a animação').click());
    const scene = () => document.querySelector('svg')?.textContent ?? '';
    // Antes do relê: sem relê; no teste: o app com o bloqueio.
    await act(async () => button('Etapa 3: Preto no terra').click());
    expect(scene()).toContain('J16');
    expect(scene()).not.toContain('RELÊ');
    await act(async () => button('Etapa 6: O relê: amarelo e branco').click());
    expect(scene()).toContain('RELÊ');
    await act(async () => button('Etapa 7: O relê: verdes no fio da bomba').click());
    expect(scene()).toContain('✂');
    await act(async () => button('Etapa 8: Teste pelo app').click());
    expect(scene()).toContain('online');
    expect(scene()).toContain('Desligar motor');
    expect(scene()).toContain('Liberar motor');
    expect(scene()).toContain('ligada');
  });

  it('com "reduzir movimento", começa parada', async () => {
    vi.stubGlobal('matchMedia', (query: string) => ({ matches: query.includes('reduce'), addEventListener: () => undefined, removeEventListener: () => undefined }));
    await render();
    expect(button('Tocar a animação')).not.toBeNull();
  });
});
