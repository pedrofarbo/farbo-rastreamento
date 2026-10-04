// @vitest-environment jsdom
import { act } from 'react';
import type { ReactNode } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it } from 'vitest';

import { Footer } from '@/components/landing/Footer';
import { COMPANY, PRIVACY_PATH, TERMS_PATH } from '@/config/legal';

import { PrivacyPage } from './PrivacyPage';
import { TermsPage } from './TermsPage';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
window.scrollTo = () => {};

function render(node: ReactNode) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  act(() => createRoot(host).render(<MemoryRouter>{node}</MemoryRouter>));
  return host;
}

afterEach(() => {
  document.body.innerHTML = '';
});

/** Cada item do sumário leva a uma seção que existe, e vice-versa. */
function expectTocMatchesSections(host: HTMLElement) {
  const toc = Array.from(host.querySelectorAll('nav[aria-label="Sumário"] a')).map((a) => a.getAttribute('href'));
  const sections = Array.from(host.querySelectorAll('article section[id]')).map((s) => `#${s.id}`);
  expect(toc.length).toBeGreaterThan(10);
  expect(toc).toEqual(sections);
}

describe('Política de Privacidade', () => {
  it('título, data, sumário e o que o sistema realmente faz', () => {
    const host = render(<PrivacyPage />);
    expect(host.querySelector('h1')?.textContent).toBe('Política de Privacidade');
    expect(document.title).toBe(`Política de Privacidade — ${COMPANY.name}`);
    expectTocMatchesSections(host);
    const text = host.textContent ?? '';
    for (const fact of [
      'Não vendemos nem alugamos',
      'não usa cookies',
      '7, 14 ou 30 dias',
      'O IP não é guardado',
      'a sua biometria nunca sai do seu aparelho',
      'AbacatePay',
      'Melhor Envios',
      'OpenStreetMap',
      'Marco Civil da Internet',
      'Respondemos em até 15 dias',
      'ANPD',
    ]) {
      expect(text).toContain(fact);
    }
    expect(host.querySelector(`a[href="mailto:${COMPANY.email}"]`)).not.toBeNull();
  });
});

describe('Termos de Uso', () => {
  it('título, sumário e os combinados principais', () => {
    const host = render(<TermsPage />);
    expect(host.querySelector('h1')?.textContent).toBe('Termos de Uso');
    expectTocMatchesSections(host);
    const text = host.textContent ?? '';
    for (const fact of [
      'não é seguro',
      'artigo 49 do Código de Defesa do Consumidor',
      'garantia legal de 90 dias',
      'acione a polícia primeiro (190)',
      'parado ou em velocidade muito baixa',
      'Elas nunca desbloqueiam',
      'Lei nº 14.132/2021',
      'sem multa',
      'foro do domicílio do Cliente',
    ]) {
      expect(text).toContain(fact);
    }
    expect(host.querySelector(`a[href="${PRIVACY_PATH}"]`)).not.toBeNull();
  });
});

describe('links para os documentos', () => {
  it('o rodapé leva aos termos e à privacidade, e a navegação volta à landing', () => {
    const host = render(<Footer />);
    expect(host.querySelector(`a[href="${TERMS_PATH}"]`)?.textContent).toBe('Termos de Uso');
    expect(host.querySelector(`a[href="${PRIVACY_PATH}"]`)?.textContent).toBe('Política de Privacidade');
    expect(host.querySelector('a[href="/#planos"]')).not.toBeNull();
  });
});
