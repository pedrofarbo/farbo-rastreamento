// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it } from 'vitest';

import { InstallationSection } from './InstallationSection';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

afterEach(() => {
  document.body.innerHTML = '';
});

describe('Instalação profissional', () => {
  it('mostra a animação em SVG, acessível, com as três etapas, no lugar das fotos', () => {
    const host = document.createElement('div');
    document.body.appendChild(host);
    act(() => createRoot(host).render(<InstallationSection onOpenInstallers={() => {}} />));

    const svg = host.querySelector('#instalacao svg[role="img"]');
    expect(svg).not.toBeNull();
    expect(svg?.querySelector('title')?.textContent).toBe('Instalação profissional do rastreador');
    const steps = Array.from(host.querySelectorAll('figcaption li')).map((li) => li.textContent);
    expect(steps).toEqual(['1Fixação no veículo', '2Ligação elétrica', '3Rastreador online']);
    expect(host.querySelectorAll('img')).toHaveLength(0);
  });
});
